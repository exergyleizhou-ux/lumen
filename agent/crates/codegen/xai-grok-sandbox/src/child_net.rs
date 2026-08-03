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
/// KNOWN GAP (Windows): returning `Ok(())` means `restrict_network` is
/// silently a no-op — a sandboxed child on Windows has full network access.
/// Do not present network isolation as available on Windows in any UI or
/// readiness claim until a WFP/firewall-based filter exists. Tracked in
/// docs/go-era-branch-map.md (安全 section) and the release notes.
///
/// # Safety
///
/// No-op outside Linux and macOS.
#[cfg(not(any(target_os = "linux", target_os = "macos")))]
pub unsafe fn install_child_network_filter() -> std::io::Result<()> {
    tracing::warn!(
        "child network isolation is unavailable on this platform; the child runs with unrestricted network access"
    );
    Ok(())
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


#[cfg(target_os = "linux")]
mod ns_lockdown {
    use libc::sock_filter;

    pub(super) const SECCOMP_RET_ALLOW: u32 = 0x7fff_0000;
    pub(super) const SECCOMP_RET_ERRNO: u32 = 0x0005_0000;
    pub(super) const EPERM_VAL: u32 = 1;
    /// ENOSYS: libc treats clone3 as unavailable and falls back to legacy clone.
    pub(super) const ENOSYS_VAL: u32 = libc::ENOSYS as u32;
    #[cfg(target_arch = "x86_64")]
    pub(super) const X32_SYSCALL_BIT: u32 = 0x4000_0000;

    pub(super) const OFF_NR: u32 = 0;
    pub(super) const OFF_ARCH: u32 = 4;
    pub(super) const OFF_ARGS0_LO: u32 = 16; // LE low half of args[0]

    #[cfg(target_arch = "x86_64")]
    pub(super) const EXPECTED_ARCH: u32 = 0xc000_003e; // AUDIT_ARCH_X86_64
    #[cfg(target_arch = "aarch64")]
    pub(super) const EXPECTED_ARCH: u32 = 0xc000_00b7; // AUDIT_ARCH_AARCH64
    #[cfg(not(any(target_arch = "x86_64", target_arch = "aarch64")))]
    pub(super) const EXPECTED_ARCH: u32 = 0;

    pub(super) const CLONE_NAMESPACE_BITS: u32 = (libc::CLONE_NEWNS as u32)
        | (libc::CLONE_NEWCGROUP as u32)
        | (libc::CLONE_NEWUTS as u32)
        | (libc::CLONE_NEWIPC as u32)
        | (libc::CLONE_NEWUSER as u32)
        | (libc::CLONE_NEWPID as u32)
        | (libc::CLONE_NEWNET as u32)
        | (libc::CLONE_NEWTIME as u32);

    /// Linux `clone3` (arch-portable number; not always exported by libc).
    pub(super) const SYS_CLONE3: u32 = 435;

    fn stmt(code: u32, k: u32) -> sock_filter {
        sock_filter {
            code: code as u16,
            jt: 0,
            jf: 0,
            k,
        }
    }

    fn jump(code: u32, k: u32, jt: u8, jf: u8) -> sock_filter {
        sock_filter {
            code: code as u16,
            jt,
            jf,
            k,
        }
    }

    /// Classic BPF namespace lockdown.
    ///
    /// - `unshare` / `setns` / legacy `clone(CLONE_NEW*)` → EPERM
    /// - `clone3` → ENOSYS (flags live in a pointed-to struct classic BPF cannot
    ///   inspect; ENOSYS makes libc fall back to legacy clone for ordinary
    ///   spawn, while direct malicious clone3 cannot create namespaces)
    pub fn build_namespace_lockdown_filter() -> Vec<sock_filter> {
        use libc::{
            BPF_ABS, BPF_JEQ, BPF_JMP, BPF_JSET, BPF_K, BPF_LD, BPF_RET, BPF_W, SYS_clone,
            SYS_setns, SYS_unshare,
        };

        let mut f = Vec::with_capacity(22);
        f.push(stmt(BPF_LD | BPF_W | BPF_ABS, OFF_ARCH));
        f.push(jump(BPF_JMP | BPF_JEQ | BPF_K, EXPECTED_ARCH, 1, 0));
        f.push(stmt(BPF_RET | BPF_K, SECCOMP_RET_ERRNO | EPERM_VAL));
        f.push(stmt(BPF_LD | BPF_W | BPF_ABS, OFF_NR));
        #[cfg(target_arch = "x86_64")]
        {
            f.push(jump(BPF_JMP | BPF_JSET | BPF_K, X32_SYSCALL_BIT, 0, 1));
            f.push(stmt(BPF_RET | BPF_K, SECCOMP_RET_ERRNO | EPERM_VAL));
        }
        for sys in [SYS_unshare as u32, SYS_setns as u32] {
            f.push(jump(BPF_JMP | BPF_JEQ | BPF_K, sys, 0, 1));
            f.push(stmt(BPF_RET | BPF_K, SECCOMP_RET_ERRNO | EPERM_VAL));
        }
        f.push(jump(BPF_JMP | BPF_JEQ | BPF_K, SYS_CLONE3, 0, 1));
        f.push(stmt(BPF_RET | BPF_K, SECCOMP_RET_ERRNO | ENOSYS_VAL));
        f.push(jump(BPF_JMP | BPF_JEQ | BPF_K, SYS_clone as u32, 0, 3));
        f.push(stmt(BPF_LD | BPF_W | BPF_ABS, OFF_ARGS0_LO));
        f.push(jump(BPF_JMP | BPF_JSET | BPF_K, CLONE_NAMESPACE_BITS, 0, 1));
        f.push(stmt(BPF_RET | BPF_K, SECCOMP_RET_ERRNO | EPERM_VAL));
        f.push(stmt(BPF_RET | BPF_K, SECCOMP_RET_ALLOW));
        f
    }

    #[cfg(test)]
    pub fn filter_jeq_immediates(filter: &[sock_filter]) -> Vec<u32> {
        use libc::{BPF_JEQ, BPF_JMP, BPF_K};
        let jeq = (BPF_JMP | BPF_JEQ | BPF_K) as u16;
        filter
            .iter()
            .filter(|i| i.code == jeq)
            .map(|i| i.k)
            .collect()
    }

    pub fn install(filter: &mut [sock_filter]) -> std::io::Result<()> {
        use libc::{
            PR_SET_NO_NEW_PRIVS, SECCOMP_FILTER_FLAG_TSYNC, SECCOMP_SET_MODE_FILTER, SYS_seccomp,
            prctl, sock_fprog,
        };

        let prog = sock_fprog {
            len: filter.len() as u16,
            filter: filter.as_mut_ptr(),
        };

        // SAFETY: standard NO_NEW_PRIVS before seccomp.
        if unsafe { prctl(PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0) } != 0 {
            return Err(std::io::Error::last_os_error());
        }

        // SAFETY: prog valid for the duration of the syscall.
        // rc: 0 ok; >0 TSYNC failing TID; -1 errno.
        let rc = unsafe {
            libc::syscall(
                SYS_seccomp,
                SECCOMP_SET_MODE_FILTER as libc::c_long,
                SECCOMP_FILTER_FLAG_TSYNC as libc::c_long,
                &prog as *const sock_fprog as *const libc::c_void,
            )
        };
        if rc == 0 {
            return Ok(());
        }
        if rc > 0 {
            return Err(std::io::Error::other(format!(
                "seccomp TSYNC failed: thread {rc} could not install filter"
            )));
        }
        Err(std::io::Error::last_os_error())
    }
}


/// Deny nested namespace creation on all threads (TSYNC).
/// Ordinary process creation uses legacy clone after clone3 returns ENOSYS.
///
/// # Safety
/// Process-wide; call after bwrap re-exec / at apply.
#[cfg(target_os = "linux")]
pub unsafe fn install_namespace_lockdown_filter() -> std::io::Result<()> {
    let mut filter = ns_lockdown::build_namespace_lockdown_filter();
    ns_lockdown::install(&mut filter)
}


#[cfg(not(target_os = "linux"))]
pub unsafe fn install_namespace_lockdown_filter() -> std::io::Result<()> {
    Ok(())
}

#[cfg(all(test, target_os = "linux"))]
mod ns_lockdown_tests {
    use super::ns_lockdown::*;
    use libc::{SYS_clone, SYS_setns, SYS_unshare, sock_filter};

    /// Minimal classic-BPF interpreter over synthetic seccomp_data fields.
    fn eval(filter: &[sock_filter], arch: u32, nr: u32, arg0_lo: u32) -> u32 {
        use libc::{BPF_ABS, BPF_JEQ, BPF_JMP, BPF_JSET, BPF_K, BPF_LD, BPF_RET, BPF_W};
        let mut pc = 0usize;
        let mut a = 0u32;
        for _ in 0..filter.len().saturating_mul(2) {
            let insn = &filter[pc];
            let op = insn.code as u32;
            if op == (BPF_LD | BPF_W | BPF_ABS) {
                a = match insn.k {
                    OFF_NR => nr,
                    OFF_ARCH => arch,
                    OFF_ARGS0_LO => arg0_lo,
                    _ => 0,
                };
                pc += 1;
            } else if op == (BPF_JMP | BPF_JEQ | BPF_K) {
                pc = if a == insn.k {
                    pc + 1 + insn.jt as usize
                } else {
                    pc + 1 + insn.jf as usize
                };
            } else if op == (BPF_JMP | BPF_JSET | BPF_K) {
                pc = if a & insn.k != 0 {
                    pc + 1 + insn.jt as usize
                } else {
                    pc + 1 + insn.jf as usize
                };
            } else if op == (BPF_RET | BPF_K) {
                return insn.k;
            } else {
                panic!("unsupported opcode {:#x} at {pc}", insn.code);
            }
            if pc >= filter.len() {
                panic!("pc out of range");
            }
        }
        panic!("filter did not RET");
    }

    fn is_allow(r: u32) -> bool {
        r == SECCOMP_RET_ALLOW
    }
    fn is_eperm(r: u32) -> bool {
        r == (SECCOMP_RET_ERRNO | EPERM_VAL)
    }
    fn is_enosys(r: u32) -> bool {
        r == (SECCOMP_RET_ERRNO | ENOSYS_VAL)
    }

    #[test]
    fn namespace_filter_targets_unshare_setns_clone3_and_clone() {
        let f = build_namespace_lockdown_filter();
        let jeqs = filter_jeq_immediates(&f);
        assert!(jeqs.contains(&(SYS_unshare as u32)), "{jeqs:?}");
        assert!(jeqs.contains(&(SYS_setns as u32)), "{jeqs:?}");
        assert!(jeqs.contains(&SYS_CLONE3), "{jeqs:?}");
        assert!(jeqs.contains(&(SYS_clone as u32)), "{jeqs:?}");
        assert!(jeqs.contains(&EXPECTED_ARCH), "{jeqs:?}");
    }

    #[test]
    fn bpf_eval_ordinary_clone_allowed_namespace_clone_denied() {
        let f = build_namespace_lockdown_filter();
        // Ordinary clone/fork flags (no NEW*)
        assert!(is_allow(eval(
            &f,
            EXPECTED_ARCH,
            SYS_clone as u32,
            0x11 /* SIGCHLD | CLONE_VM-ish low bits without NEW* */
        )));
        assert!(is_eperm(eval(
            &f,
            EXPECTED_ARCH,
            SYS_clone as u32,
            libc::CLONE_NEWUSER as u32
        )));
        assert!(is_eperm(eval(
            &f,
            EXPECTED_ARCH,
            SYS_clone as u32,
            libc::CLONE_NEWNS as u32
        )));
    }

    #[test]
    fn bpf_eval_clone3_enosys_unshare_setns_eperm_read_allowed() {
        let f = build_namespace_lockdown_filter();
        assert!(is_enosys(eval(&f, EXPECTED_ARCH, SYS_CLONE3, 0)));
        assert!(is_eperm(eval(&f, EXPECTED_ARCH, SYS_unshare as u32, 0)));
        assert!(is_eperm(eval(&f, EXPECTED_ARCH, SYS_setns as u32, 0)));
        assert!(is_allow(eval(&f, EXPECTED_ARCH, 0, 0)));
    }

    #[test]
    fn bpf_eval_wrong_arch_and_x32_denied() {
        let f = build_namespace_lockdown_filter();
        assert!(is_eperm(eval(&f, 0xdead_beef, SYS_clone as u32, 0)));
        #[cfg(target_arch = "x86_64")]
        {
            // x32: nr has high bit set
            assert!(is_eperm(eval(
                &f,
                EXPECTED_ARCH,
                (SYS_unshare as u32) | X32_SYSCALL_BIT,
                0
            )));
        }
    }

    #[test]
    fn namespace_bits_cover_user_ns_and_mount_ns() {
        assert_ne!(CLONE_NAMESPACE_BITS & (libc::CLONE_NEWUSER as u32), 0);
        assert_ne!(CLONE_NAMESPACE_BITS & (libc::CLONE_NEWNS as u32), 0);
        assert_ne!(CLONE_NAMESPACE_BITS & (libc::CLONE_NEWNET as u32), 0);
    }

    #[test]
    fn filter_ends_with_allow() {
        let f = build_namespace_lockdown_filter();
        assert_eq!(f.last().unwrap().k, SECCOMP_RET_ALLOW);
    }
}
