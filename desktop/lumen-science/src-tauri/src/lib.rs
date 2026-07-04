//! Lumen Science Acceptance desktop — spawns Go GUI on loopback, quit stops proxy only.

use std::path::PathBuf;
use std::process::{Child, Command, Stdio};
use std::sync::Mutex;
use std::time::Duration;

use tauri::{Manager, RunEvent, State};

const GUI_PORT: u16 = 18990;

struct ProcState {
    gui: Option<Child>,
}

impl Default for ProcState {
    fn default() -> Self {
        Self { gui: None }
    }
}

fn lumen_bin() -> PathBuf {
    if let Ok(p) = std::env::var("LUMEN_BIN") {
        return PathBuf::from(p);
    }
    if let Ok(exe) = std::env::current_exe() {
        let mut dir = exe.parent().map(|p| p.to_path_buf());
        while let Some(d) = dir {
            for candidate in [d.join("lumen"), d.join("bin").join("lumen")] {
                if candidate.is_file() {
                    return candidate;
                }
            }
            dir = d.parent().map(|p| p.to_path_buf());
        }
    }
    PathBuf::from("lumen")
}

fn spawn_gui() -> Option<Child> {
    let bin = lumen_bin();
    Command::new(&bin)
        .args(["science", "gui", "--no-browser", "--port", &GUI_PORT.to_string()])
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn()
        .ok()
}

fn stop_proxy_only() {
    let _ = reqwest::blocking::Client::builder()
        .timeout(Duration::from_secs(3))
        .build()
        .and_then(|c| {
            c.post(format!("http://127.0.0.1:{GUI_PORT}/api/quit-proxy"))
                .header("Content-Type", "application/json")
                .body("{}")
                .send()
        });
}

fn wait_health() {
    let url = format!("http://127.0.0.1:{GUI_PORT}/api/health");
    for _ in 0..60 {
        if let Ok(resp) = reqwest::blocking::get(&url) {
            if resp.status().is_success() {
                return;
            }
        }
        std::thread::sleep(Duration::from_millis(250));
    }
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .manage(Mutex::new(ProcState::default()))
        .setup(|app| {
            let state: State<Mutex<ProcState>> = app.state();
            state.lock().unwrap().gui = spawn_gui();
            wait_health();
            if let Some(win) = app.get_webview_window("main") {
                let _ = win.eval(&format!(
                    "window.location.replace('http://127.0.0.1:{GUI_PORT}/');"
                ));
            }
            Ok(())
        })
        .build(tauri::generate_context!())
        .expect("error building tauri app")
        .run(|app, event| {
            if let RunEvent::Exit = event {
                stop_proxy_only();
                let state: State<Mutex<ProcState>> = app.state();
                if let Ok(mut st) = state.lock() {
                    if let Some(mut child) = st.gui.take() {
                        let _ = child.kill();
                    }
                };
            }
        });
}