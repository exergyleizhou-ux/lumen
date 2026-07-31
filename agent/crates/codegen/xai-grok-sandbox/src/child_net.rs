//! Per-child network isolation.
//!
//! Linux installs a seccomp filter in `pre_exec`. macOS cannot apply an
//! equivalent filter there, so [`child_command`] wraps the target in
//! `/usr/bin/sandbox-exec` with a network-denying Seatbelt profile.

use std::ffi::OsStr;
use std::process::Command;

#[cfg(target_os = "macos")]
const SANDBOX_EXEC: &str = "/usr/bin/sandbox-exec";

#[cfg(target_os = "macos")]
const NETWORK_DENY_PROFILE: &str = "(version 1)(allow default)(deny network*)";

/// Build a child command with the platform's network restriction applied.
///
/// On macOS, restricted commands are executed through `sandbox-exec`. On
/// Linux the command remains unchanged because the seccomp filter is installed
/// separately in `pre_exec`; other platforms currently leave it unchanged.
pub fn child_command(program: impl AsRef<OsStr>, restrict_network: bool) -> Command {
    #[cfg(target_os = "macos")]
    if restrict_network {
        let mut command = Command::new(SANDBOX_EXEC);
        command
            .arg("-p")
            .arg(NETWORK_DENY_PROFILE)
            .arg("--")
            .arg(program.as_ref());
        return command;
    }

    let _ = restrict_network;
    Command::new(program)
}

/// Install seccomp BPF filter blocking network syscalls.
///
/// # Safety
///
/// Must be called in a `pre_exec` context (after `fork`, before `exec`).
#[cfg(target_os = "linux")]
pub unsafe fn install_child_network_filter() -> std::io::Result<()> {
    use libc::{
        BPF_ABS, BPF_JEQ, BPF_JMP, BPF_K, BPF_LD, BPF_RET, BPF_W, PR_SET_NO_NEW_PRIVS,
        PR_SET_SECCOMP, SECCOMP_MODE_FILTER, SYS_accept, SYS_accept4, SYS_bind, SYS_connect,
        SYS_listen, SYS_sendmsg, SYS_sendto, prctl, sock_filter, sock_fprog,
    };

    const SECCOMP_RET_ALLOW: u32 = 0x7fff_0000;
    const SECCOMP_RET_ERRNO: u32 = 0x0005_0000;
    const EPERM_VAL: u32 = 1; // libc::EPERM

    macro_rules! bpf_stmt {
        ($code:expr, $k:expr) => {
            sock_filter {
                code: $code as u16,
                jt: 0,
                jf: 0,
                k: $k as u32,
            }
        };
    }

    macro_rules! bpf_jump {
        ($code:expr, $k:expr, $jt:expr, $jf:expr) => {
            sock_filter {
                code: $code as u16,
                jt: $jt,
                jf: $jf,
                k: $k as u32,
            }
        };
    }

    const NR_OFFSET: u32 = 0; // seccomp_data.nr offset

    let blocked_syscalls: &[i64] = &[
        SYS_connect,
        SYS_bind,
        SYS_sendto,
        SYS_sendmsg,
        SYS_listen,
        SYS_accept,
        SYS_accept4,
    ];

    let mut filter: Vec<sock_filter> = Vec::new();
    let total_checks = blocked_syscalls.len();

    // 1. Load syscall number
    filter.push(bpf_stmt!(BPF_LD | BPF_W | BPF_ABS, NR_OFFSET));

    // 2. Check each blocked syscall
    for (i, &syscall) in blocked_syscalls.iter().enumerate() {
        let remaining = total_checks - i - 1;
        filter.push(bpf_jump!(
            BPF_JMP | BPF_JEQ | BPF_K,
            syscall,
            remaining as u8 + 1, // match: jump to ERRNO
            0                    // no match: check next
        ));
    }

    // 3. Default: ALLOW
    filter.push(bpf_stmt!(BPF_RET | BPF_K, SECCOMP_RET_ALLOW));

    // 4. Blocked: ERRNO(EPERM)
    filter.push(bpf_stmt!(BPF_RET | BPF_K, SECCOMP_RET_ERRNO | EPERM_VAL));

    let prog = sock_fprog {
        len: filter.len() as u16,
        filter: filter.as_mut_ptr(),
    };

    // Must set PR_SET_NO_NEW_PRIVS before applying seccomp filter
    // SAFETY: prctl with PR_SET_NO_NEW_PRIVS is safe in pre_exec context.
    if unsafe { prctl(PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0) } != 0 {
        return Err(std::io::Error::last_os_error());
    }

    // SAFETY: prog is a valid sock_fprog pointing to our filter array.
    if unsafe {
        prctl(
            PR_SET_SECCOMP,
            SECCOMP_MODE_FILTER as libc::c_ulong,
            &prog as *const _ as libc::c_ulong,
            0,
            0,
        )
    } != 0
    {
        return Err(std::io::Error::last_os_error());
    }

    Ok(())
}

/// macOS must use [`child_command`] so `sandbox-exec` can wrap the real target.
/// Failing here prevents a future `pre_exec` caller from silently running with
/// unrestricted network access.
///
/// # Safety
///
/// This function must not be used on macOS; use [`child_command`] instead.
#[cfg(target_os = "macos")]
pub unsafe fn install_child_network_filter() -> std::io::Result<()> {
    Err(std::io::Error::new(
        std::io::ErrorKind::Unsupported,
        "macOS child network isolation requires child_command/sandbox-exec",
    ))
}

/// No process-level child network filter is available on this platform.
///
/// On Windows, use [`restrict_child_network_wfp`] to install per-process WFP
/// filters after the child has been spawned (pass the child's PID). This must
/// be called from the parent, not from a `pre_exec` hook (which does not exist
/// on Windows).
///
/// # Safety
///
/// No-op outside Linux and macOS.
#[cfg(not(any(target_os = "linux", target_os = "macos")))]
pub unsafe fn install_child_network_filter() -> std::io::Result<()> {
    Ok(())
}

// ---------------------------------------------------------------------------
// Windows: per-process network restriction via Windows Filtering Platform
// ---------------------------------------------------------------------------

#[cfg(windows)]
mod wfp_restrict {
    use std::io;
    use std::sync::atomic::{AtomicBool, Ordering};

    use windows_sys::Win32::Foundation::HANDLE;
    use windows_sys::Win32::NetworkManagement::WindowsFilteringPlatform::{
        FwpmEngineClose0, FwpmEngineOpen0, FwpmFilterAdd0, FwpmFilterDeleteById0,
        FwpmSubLayerAdd0, FWPM_CONDITION_ALE_APP_ID, FWPM_FILTER0, FWPM_FILTER_CONDITION0,
        FWPM_SESSION0, FWPM_SUBLAYER0,
    };

    fn wfp_err(msg: impl Into<String>) -> io::Error {
        io::Error::other(msg.into())
    }

    /// WFP sublayer GUID for Lumen network sandbox.
    /// {C5A1F7E3-9B4D-4A2E-8F1C-D6E3B5A7C9F0}
    const LUMEN_SUBLAYER_KEY: windows_sys::core::GUID = windows_sys::core::GUID {
        data1: 0xC5A1F7E3,
        data2: 0x9B4D,
        data3: 0x4A2E,
        data4: [0x8F, 0x1C, 0xD6, 0xE3, 0xB5, 0xA7, 0xC9, 0xF0],
    };

    const FWPM_SESSION_FLAG_DYNAMIC: u32 = 0x00000001;

    const FWPM_LAYER_ALE_AUTH_CONNECT_V4: windows_sys::core::GUID = windows_sys::core::GUID {
        data1: 0x5A903A65, data2: 0xB706, data3: 0x44A8,
        data4: [0xA3, 0x32, 0x2C, 0x5D, 0x1A, 0xDE, 0xF4, 0xAE],
    };
    const FWPM_LAYER_ALE_AUTH_CONNECT_V6: windows_sys::core::GUID = windows_sys::core::GUID {
        data1: 0x8F3D5D6F, data2: 0x0C3B, data3: 0x4A90,
        data4: [0xB7, 0xEE, 0x23, 0xE6, 0x2D, 0x98, 0x8F, 0x45],
    };
    const FWPM_LAYER_ALE_AUTH_SEND_V4: windows_sys::core::GUID = windows_sys::core::GUID {
        data1: 0x6CA7165F, data2: 0x3407, data3: 0x49ED,
        data4: [0x8E, 0x4A, 0x59, 0xC4, 0x14, 0x77, 0x07, 0xE4],
    };
    const FWPM_LAYER_ALE_AUTH_SEND_V6: windows_sys::core::GUID = windows_sys::core::GUID {
        data1: 0x1FCD81D2, data2: 0x47F9, data3: 0x4A9E,
        data4: [0xB5, 0xED, 0x73, 0x13, 0x62, 0xF1, 0x7A, 0x49],
    };

    /// A handle to a network-restricted child process. When dropped, the WFP
    /// filters are removed and the engine session is closed.
    pub struct NetworkGuard {
        engine: HANDLE,
        filter_ids: Vec<u64>,
        closed: AtomicBool,
    }

    unsafe impl Send for NetworkGuard {}
    unsafe impl Sync for NetworkGuard {}

    impl NetworkGuard {
        /// Install WFP filters blocking all outbound TCP and UDP from `child_pid`.
        pub fn restrict(child_pid: u32) -> io::Result<Self> {
            let mut session: FWPM_SESSION0 = unsafe { std::mem::zeroed() };
            session.flags = FWPM_SESSION_FLAG_DYNAMIC;

            let mut engine: HANDLE = std::ptr::null_mut();
            let rc = unsafe {
                FwpmEngineOpen0(
                    std::ptr::null(), 0, std::ptr::null(),
                    &mut session, &mut engine,
                )
            };
            if rc != 0 || engine.is_null() {
                return Err(wfp_err(format!("FwpmEngineOpen0 failed: {rc:#x}")));
            }

            let mut sublayer: FWPM_SUBLAYER0 = unsafe { std::mem::zeroed() };
            sublayer.subLayerKey = LUMEN_SUBLAYER_KEY;
            let rc = unsafe { FwpmSubLayerAdd0(engine, &mut sublayer, std::ptr::null_mut()) };
            if rc != 0 && rc != 0x80320009 {
                unsafe { FwpmEngineClose0(engine) };
                return Err(wfp_err(format!("FwpmSubLayerAdd0 failed: {rc:#x}")));
            }

            let app_path: Vec<u16> = format!("\\device\\harddiskvolume*\\pid_{child_pid}\0")
                .encode_utf16().collect();
            let app_id_blob = windows_sys::Win32::NetworkManagement::WindowsFilteringPlatform::FWP_BYTE_BLOB {
                size: (app_path.len() * 2) as u32,
                data: app_path.as_ptr() as *mut u8,
            };

            let mut condition: FWPM_FILTER_CONDITION0 = unsafe { std::mem::zeroed() };
            condition.fieldKey = FWPM_CONDITION_ALE_APP_ID;
            condition.matchType = 0;
            condition.conditionValue.r#type = 0x10000005;
            condition.conditionValue.Anonymous.byteBlob = &app_id_blob as *const _ as *mut _;

            let layers: &[(windows_sys::core::GUID, &str)] = &[
                (FWPM_LAYER_ALE_AUTH_CONNECT_V4, "TCPv4"),
                (FWPM_LAYER_ALE_AUTH_CONNECT_V6, "TCPv6"),
                (FWPM_LAYER_ALE_AUTH_SEND_V4, "UDPv4"),
                (FWPM_LAYER_ALE_AUTH_SEND_V6, "UDPv6"),
            ];

            let mut filter_ids: Vec<u64> = Vec::with_capacity(layers.len());
            for (layer_key, _name) in layers {
                let mut filter: FWPM_FILTER0 = unsafe { std::mem::zeroed() };
                filter.layerKey = *layer_key;
                filter.subLayerKey = LUMEN_SUBLAYER_KEY;
                filter.action.r#type = 1;
                filter.numFilterConditions = 1;
                filter.filterCondition = &mut condition;
                filter.flags = 0;

                let mut filter_id: u64 = 0;
                let rc = unsafe {
                    FwpmFilterAdd0(engine, &mut filter, std::ptr::null_mut(), &mut filter_id)
                };
                if rc != 0 {
                    tracing::warn!(
                        pid = child_pid, error = rc,
                        "WFP filter add failed; child may have partial network access"
                    );
                } else {
                    filter_ids.push(filter_id);
                }
            }

            Ok(Self { engine, filter_ids, closed: AtomicBool::new(false) })
        }
    }

    impl Drop for NetworkGuard {
        fn drop(&mut self) {
            if self.closed.swap(true, Ordering::AcqRel) {
                return;
            }
            for &id in &self.filter_ids {
                unsafe { FwpmFilterDeleteById0(self.engine, id) };
            }
            unsafe { FwpmEngineClose0(self.engine) };
        }
    }
}

#[cfg(windows)]
pub use wfp_restrict::NetworkGuard;

/// Restrict network access for a child process identified by PID using WFP
/// (Windows Filtering Platform). Returns a guard that removes the filters on
/// drop.
///
/// This is the Windows equivalent of the Linux seccomp `install_child_network_filter`.
/// Unlike the Linux version (called in `pre_exec`), this must be called from the
/// **parent** process after the child has been spawned.
#[cfg(windows)]
pub fn restrict_child_network_wfp(child_pid: u32) -> std::io::Result<wfp_restrict::NetworkGuard> {
    wfp_restrict::NetworkGuard::restrict(child_pid)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn unrestricted_command_executes_target_directly() {
        let command = child_command("/bin/echo", false);
        assert_eq!(command.get_program(), "/bin/echo");
        assert_eq!(command.get_args().count(), 0);
    }

    #[cfg(target_os = "macos")]
    #[test]
    fn restricted_macos_command_uses_sandbox_exec() {
        let command = child_command("/bin/echo", true);
        let args: Vec<_> = command.get_args().collect();

        assert_eq!(command.get_program(), SANDBOX_EXEC);
        assert_eq!(
            args,
            ["-p", NETWORK_DENY_PROFILE, "--", "/bin/echo"]
                .map(OsStr::new)
                .as_slice()
        );
    }

    #[cfg(target_os = "macos")]
    #[test]
    fn sandbox_exec_blocks_child_network_connect() {
        let test_binary = std::env::current_exe().expect("current test binary");
        let mut command = child_command(test_binary, true);
        command
            .args([
                "--exact",
                "child_net::tests::network_connect_probe",
                "--nocapture",
            ])
            .env("XAI_GROK_SANDBOX_NETWORK_PROBE", "1");

        let output = command.output().expect("sandbox-exec should start");
        assert!(
            output.status.success(),
            "network connect must fail with EPERM under sandbox-exec: status={} stderr={}",
            output.status,
            String::from_utf8_lossy(&output.stderr)
        );
    }

    #[cfg(target_os = "macos")]
    #[test]
    fn network_connect_probe() {
        if std::env::var_os("XAI_GROK_SANDBOX_NETWORK_PROBE").is_none() {
            return;
        }

        let error =
            std::net::TcpStream::connect("127.0.0.1:9").expect_err("sandboxed connect must fail");
        assert_eq!(error.raw_os_error(), Some(libc::EPERM));
    }
}
