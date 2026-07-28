//! Windows system sleep/wake via `PowerRegisterSuspendResumeNotification`
//! with a `DEVICE_NOTIFY_CALLBACK` recipient — no hidden window or message
//! loop required (Windows 8+).
//!
//! Dark wake detection uses `PowerSettingRegisterNotification` for
//! `GUID_MONITOR_POWER_ON` and `GUID_CONSOLE_DISPLAY_STATE` to receive
//! precise display-on/off events rather than polling monitor count.
//!
//! NOTE: this module only compiles when targeting Windows.

use std::os::raw::c_void;
use std::sync::atomic::{AtomicBool, Ordering};

use windows_sys::Win32::Foundation::HANDLE;
use windows_sys::Win32::System::Power::{
    DEVICE_NOTIFY_SUBSCRIBE_PARAMETERS, HPOWERNOTIFY, PowerRegisterSuspendResumeNotification,
    PowerSettingRegisterNotification, PowerSettingUnregisterNotification,
    PowerUnregisterSuspendResumeNotification,
};
use windows_sys::Win32::UI::WindowsAndMessaging::{
    DEVICE_NOTIFY_CALLBACK, GetSystemMetrics, SM_REMOTESESSION,
};

use super::{PowerCallback, PowerEvent};

// Power-broadcast event types (WM_POWERBROADCAST `wParam`).
const PBT_APMSUSPEND: u32 = 0x0004;
const PBT_APMRESUMESUSPEND: u32 = 0x0007;
const PBT_APMRESUMEAUTOMATIC: u32 = 0x0012;

const ERROR_SUCCESS: u32 = 0;

// PBT_POWERSETTINGCHANGE: delivered when a power setting changes (e.g. display on/off).
const PBT_POWERSETTINGCHANGE: u32 = 0x8013;

// GUID_MONITOR_POWER_ON: {02731015-4510-4526-99E6-E5A17EBD1AEA}
const GUID_MONITOR_POWER_ON: windows_sys::core::GUID = windows_sys::core::GUID {
    data1: 0x02731015,
    data2: 0x4510,
    data3: 0x4526,
    data4: [0x99, 0xE6, 0xE5, 0xA1, 0x7E, 0xBD, 0x1A, 0xEA],
};

// GUID_CONSOLE_DISPLAY_STATE: {6FE69556-704A-47A0-8F24-C28D936FDA47}
const GUID_CONSOLE_DISPLAY_STATE: windows_sys::core::GUID = windows_sys::core::GUID {
    data1: 0x6FE69556,
    data2: 0x704A,
    data3: 0x47A0,
    data4: [0x8F, 0x24, 0xC2, 0x8D, 0x93, 0x6F, 0xDA, 0x47],
};

/// Tracks whether the display is currently on. Updated by the power-setting
/// callback on `PBT_POWERSETTINGCHANGE` events. Initialized to `true` (assume
/// display is on until we learn otherwise from a notification).
static DISPLAY_ON: AtomicBool = AtomicBool::new(true);

/// `POWERBROADCAST_SETTING` layout for reading the setting value in the
/// power callback. Not in windows-sys 0.59 feature set, so defined locally.
#[repr(C)]
struct PowerBroadcastSetting {
    power_setting: windows_sys::core::GUID,
    data_length: u32,
    data: [u8; 1],
}

/// Compare two GUIDs for equality. `windows_sys::core::GUID` does not
/// implement `PartialEq`, so compare fields manually.
fn guid_eq(a: &windows_sys::core::GUID, b: &windows_sys::core::GUID) -> bool {
    a.data1 == b.data1 && a.data2 == b.data2 && a.data3 == b.data3 && a.data4 == b.data4
}

/// Heap-pinned so its address stays stable for the registration lifetime; the
/// raw pointer is handed to the OS as the callback context.
struct Context {
    callback: PowerCallback,
}

pub(crate) struct Listener {
    // Registration handle from `PowerRegisterSuspendResumeNotification`
    // (a `*mut c_void`; cast to `HPOWERNOTIFY` for unregister).
    handle: *mut c_void,
    // Display notification handles (GUID_MONITOR_POWER_ON, GUID_CONSOLE_DISPLAY_STATE).
    display_handles: [*mut c_void; 2],
    // Kept alive (and freed in `Drop`) because the OS holds a raw pointer to it.
    ctx: *mut Context,
}

// The OS invokes the callback on an arbitrary thread; the handle is only used
// to unregister. `PowerCallback` is `Send + Sync`.
unsafe impl Send for Listener {}
unsafe impl Sync for Listener {}

impl Listener {
    pub(crate) fn start(callback: PowerCallback) -> Option<Self> {
        let ctx = Box::into_raw(Box::new(Context { callback }));

        let mut params = DEVICE_NOTIFY_SUBSCRIBE_PARAMETERS {
            Callback: Some(power_callback),
            Context: ctx as *mut c_void,
        };

        // Register suspend/resume notifications.
        let mut handle: *mut c_void = std::ptr::null_mut();
        let status = unsafe {
            PowerRegisterSuspendResumeNotification(
                DEVICE_NOTIFY_CALLBACK,
                &mut params as *mut DEVICE_NOTIFY_SUBSCRIBE_PARAMETERS as HANDLE,
                &mut handle,
            )
        };

        if status != ERROR_SUCCESS || handle.is_null() {
            unsafe { drop(Box::from_raw(ctx)) };
            return None;
        }

        // Register display power notifications (GUID_MONITOR_POWER_ON).
        let mut display_handle0: *mut c_void = std::ptr::null_mut();
        unsafe {
            PowerSettingRegisterNotification(
                &GUID_MONITOR_POWER_ON,
                DEVICE_NOTIFY_CALLBACK,
                &mut params as *mut DEVICE_NOTIFY_SUBSCRIBE_PARAMETERS as HANDLE,
                &mut display_handle0,
            );
        };

        // Register console display state notifications (GUID_CONSOLE_DISPLAY_STATE).
        let mut display_handle1: *mut c_void = std::ptr::null_mut();
        unsafe {
            PowerSettingRegisterNotification(
                &GUID_CONSOLE_DISPLAY_STATE,
                DEVICE_NOTIFY_CALLBACK,
                &mut params as *mut DEVICE_NOTIFY_SUBSCRIBE_PARAMETERS as HANDLE,
                &mut display_handle1,
            );
        };

        let display_handles = [display_handle0, display_handle1];

        Some(Self {
            handle,
            display_handles,
            ctx,
        })
    }
}

impl Drop for Listener {
    fn drop(&mut self) {
        unsafe {
            PowerUnregisterSuspendResumeNotification(self.handle as HPOWERNOTIFY);
            for &h in &self.display_handles {
                if !h.is_null() {
                    PowerSettingUnregisterNotification(h as HPOWERNOTIFY);
                }
            }
            drop(Box::from_raw(self.ctx));
        }
    }
}

unsafe extern "system" fn power_callback(
    context: *const c_void,
    event_type: u32,
    setting: *const c_void,
) -> u32 {
    // Safe: `context` is the live `Context` we registered with.
    let ctx = unsafe { &*(context as *const Context) };
    match event_type {
        PBT_APMSUSPEND => (ctx.callback)(PowerEvent::WillSleep),
        // A single resume can deliver both PBT_APMRESUMEAUTOMATIC and
        // PBT_APMRESUMESUSPEND, so `DidWake` may fire twice per wake. That is
        // fine and intentional: lowering the sleep gate is idempotent, so a
        // duplicate wake is harmless — do not try to "dedupe" this later.
        PBT_APMRESUMEAUTOMATIC | PBT_APMRESUMESUSPEND => (ctx.callback)(PowerEvent::DidWake),
        PBT_POWERSETTINGCHANGE => {
            if setting.is_null() {
                return ERROR_SUCCESS;
            }
            let pbs = unsafe { &*(setting as *const PowerBroadcastSetting) };
            if guid_eq(&pbs.power_setting, &GUID_MONITOR_POWER_ON)
                || guid_eq(&pbs.power_setting, &GUID_CONSOLE_DISPLAY_STATE)
            {
                if pbs.data_length >= 4 {
                    let value = unsafe { *(pbs.data.as_ptr() as *const u32) };
                    // 0 = off, 1 = on, 2 = dimmed. Treat dimmed as off for
                    // dark-wake detection purposes.
                    DISPLAY_ON.store(value == 1, Ordering::Relaxed);
                }
            }
        }
        _ => {}
    }
    ERROR_SUCCESS
}

pub(crate) fn current_power_state() -> crate::PowerState {
    // A remote (RDP) session means a user is actively connected — FullWake.
    if unsafe { GetSystemMetrics(SM_REMOTESESSION) } != 0 {
        return crate::PowerState::FullWake;
    }

    // Use the display-on atomic (updated by PowerSettingRegisterNotification
    // callbacks) instead of polling GetSystemMetrics(SM_CMONITORS). When the
    // display is off, the system is likely in a low-power background state
    // (Modern Standby / Connected Standby), treated as DarkWake.
    if DISPLAY_ON.load(Ordering::Relaxed) {
        crate::PowerState::FullWake
    } else {
        crate::PowerState::DarkWake
    }
}

/// Returns whether the display is currently on, as tracked by
/// `PowerSettingRegisterNotification` callbacks.
pub(crate) fn is_display_on() -> bool {
    DISPLAY_ON.load(Ordering::Relaxed)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn current_power_state_does_not_panic() {
        // Must always return a valid enum variant, never panic.
        let state = current_power_state();
        assert!(matches!(
            state,
            crate::PowerState::FullWake | crate::PowerState::DarkWake | crate::PowerState::Unknown
        ));
    }

    #[test]
    fn listener_start_and_drop_clean() {
        let _listener = Listener::start(Box::new(|_| {}));
        // Must not panic or leak on drop.
    }
}
