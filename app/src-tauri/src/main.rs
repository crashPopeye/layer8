#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use serde::{Deserialize, Serialize};
use std::process::Command;
use std::sync::atomic::{AtomicUsize, Ordering};
use tauri::webview::NewWindowResponse;
use tauri::{Manager, RunEvent, WebviewUrl, WebviewWindowBuilder, WindowEvent};
use tauri_plugin_dialog::DialogExt;

#[derive(Serialize, Deserialize)]
struct DaemonInfo {
    url: String,
    token: String,
}

/// Runs the bundled `layer8d ensure` and returns the daemon's url and token.
#[tauri::command]
async fn daemon_info() -> Result<DaemonInfo, String> {
    let exe = std::env::current_exe().map_err(|e| e.to_string())?;
    let out = Command::new(exe.with_file_name("layer8d"))
        .arg("ensure")
        .output()
        .map_err(|e| format!("layer8d ensure: {e}"))?;
    if !out.status.success() {
        return Err(format!(
            "layer8d ensure: {}",
            String::from_utf8_lossy(&out.stderr).trim()
        ));
    }
    serde_json::from_slice(&out.stdout).map_err(|e| format!("layer8d ensure output: {e}"))
}

/// Opens the native folder picker and returns the chosen folder.
#[tauri::command]
async fn pick_folder(app: tauri::AppHandle) -> Option<String> {
    app.dialog()
        .file()
        .blocking_pick_folder()
        .map(|p| p.to_string())
}

/// Opens an https URL in the default browser.
#[tauri::command]
fn open_url(url: String) -> Result<(), String> {
    if !url.starts_with("https://") {
        return Err(format!("refusing to open {url}"));
    }
    Command::new("open")
        .arg(&url)
        .status()
        .map_err(|e| format!("open: {e}"))
        .map(|_| ())
}

/// Opens a `window.open` request, such as a popped-out dock panel, as a window sharing the opener's webview configuration.
fn popout(app: &tauri::AppHandle, features: tauri::webview::NewWindowFeatures) -> NewWindowResponse<tauri::Wry> {
    static COUNT: AtomicUsize = AtomicUsize::new(0);
    let label = format!("popout-{}", COUNT.fetch_add(1, Ordering::Relaxed));
    let blank = WebviewUrl::External("about:blank".parse().expect("about:blank is a URL"));
    match WebviewWindowBuilder::new(app, label, blank)
        .window_features(features)
        .title("layer8")
        .build()
    {
        Ok(window) => NewWindowResponse::Create { window },
        Err(_) => NewWindowResponse::Deny,
    }
}

fn main() {
    tauri::Builder::default()
        .plugin(tauri_plugin_dialog::init())
        .invoke_handler(tauri::generate_handler![daemon_info, pick_folder, open_url])
        .setup(|app| {
            let handle = app.handle().clone();
            let config = app.config().app.windows[0].clone();
            WebviewWindowBuilder::from_config(app, &config)?
                .on_new_window(move |_, features| popout(&handle, features))
                .build()?;
            Ok(())
        })
        .on_window_event(|window, event| {
            if let WindowEvent::CloseRequested { api, .. } = event {
                if window.label() != "main" {
                    return;
                }
                api.prevent_close();
                let _ = window.hide();
            }
        })
        .build(tauri::generate_context!())
        .expect("failed to build layer8")
        .run(|app, event| {
            if let RunEvent::Reopen { .. } = event {
                if let Some(window) = app.get_webview_window("main") {
                    let _ = window.show();
                    let _ = window.set_focus();
                }
            }
        });
}
