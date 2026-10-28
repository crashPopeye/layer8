//! The desktop shell. Deliberately thin: everything that matters (agents, git,
//! GitHub, Linear) lives in the Go daemon `layer8d`, bundled next to this binary.
//! The shell starts the daemon, shows the window, and installs signed updates.

use std::path::PathBuf;
use std::process::Command;
use std::sync::Mutex;

use serde::{Deserialize, Serialize};
#[cfg(target_os = "macos")]
use tauri::RunEvent;
use tauri::{Manager, Url, WindowEvent};
use tauri_plugin_dialog::DialogExt;
use tauri_plugin_updater::{Update, UpdaterExt};

#[derive(Clone, Serialize, Deserialize)]
struct DaemonInfo {
    url: String,
    token: String,
}

#[derive(Default)]
struct Shell {
    daemon: Mutex<Option<DaemonInfo>>,
    pending: Mutex<Option<Update>>,
}

#[derive(Serialize)]
struct UpdateStatus {
    checked: bool,
    available: bool,
    current: String,
    version: Option<String>,
    notes: Option<String>,
}

/// The bundled daemon binary: Contents/MacOS/layer8d next to our own executable.
fn daemon_binary() -> Result<PathBuf, String> {
    let exe = std::env::current_exe().map_err(|e| e.to_string())?;
    let dir = exe.parent().ok_or("no executable directory")?;
    Ok(dir.join("layer8d"))
}

/// Runs `layer8d <args>` and returns its stdout. It is a blocking call, so commands
/// call it from a blocking thread.
fn run_daemon_binary(args: &[&str]) -> Result<String, String> {
    let bin = daemon_binary()?;
    let out = Command::new(&bin)
        .args(args)
        .output()
        .map_err(|e| format!("could not run {}: {e}", bin.display()))?;
    if !out.status.success() {
        let err = String::from_utf8_lossy(&out.stderr).trim().to_string();
        return Err(if err.is_empty() { format!("layer8d {} failed", args.join(" ")) } else { err });
    }
    Ok(String::from_utf8_lossy(&out.stdout).to_string())
}

/// Starts (or upgrades) the daemon via `layer8d ensure`, once per app launch.
#[tauri::command]
async fn daemon_info(state: tauri::State<'_, Shell>) -> Result<DaemonInfo, String> {
    if let Some(info) = state.daemon.lock().unwrap().clone() {
        return Ok(info);
    }
    let out = tauri::async_runtime::spawn_blocking(|| run_daemon_binary(&["ensure"]))
        .await
        .map_err(|e| e.to_string())??;
    let info: DaemonInfo = serde_json::from_str(out.trim()).map_err(|e| format!("bad reply from layer8d ensure: {e}"))?;
    *state.daemon.lock().unwrap() = Some(info.clone());
    Ok(info)
}

#[tauri::command]
async fn pick_folder(app: tauri::AppHandle) -> Result<Option<String>, String> {
    let picked = tauri::async_runtime::spawn_blocking(move || app.dialog().file().set_title("Choose a folder").blocking_pick_folder())
        .await
        .map_err(|e| e.to_string())?;
    Ok(picked.and_then(|p| p.into_path().ok()).map(|p| p.to_string_lossy().to_string()))
}

#[tauri::command]
async fn install_cli() -> Result<String, String> {
    tauri::async_runtime::spawn_blocking(|| run_daemon_binary(&["install-cli"]))
        .await
        .map_err(|e| e.to_string())?
        .map(|s| s.trim().to_string())
}

/// Checks for an update through the daemon, which reads the private repository's
/// releases with the user's GitHub login. Tauri verifies the signature of anything
/// it downloads against the public key built into this app.
#[tauri::command]
async fn check_update(app: tauri::AppHandle, state: tauri::State<'_, Shell>) -> Result<UpdateStatus, String> {
    let info = daemon_info(state.clone()).await?;
    let endpoint = Url::parse(&format!("{}/api/update/manifest", info.url)).map_err(|e| e.to_string())?;
    let updater = app
        .updater_builder()
        .endpoints(vec![endpoint])
        .map_err(|e| e.to_string())?
        .header("Authorization", format!("Bearer {}", info.token))
        .map_err(|e| e.to_string())?
        .build()
        .map_err(|e| e.to_string())?;
    let current = app.package_info().version.to_string();
    let found = updater.check().await.map_err(|e| e.to_string())?;
    let status = UpdateStatus {
        checked: true,
        available: found.is_some(),
        current,
        version: found.as_ref().map(|u| u.version.clone()),
        notes: found.as_ref().and_then(|u| u.body.clone()),
    };
    *state.pending.lock().unwrap() = found;
    Ok(status)
}

/// Downloads, verifies and installs the update found by check_update, then restarts
/// the app. The daemon is not restarted here (that would kill working agents): it
/// notices its binary was replaced and switches to the new version once idle.
#[tauri::command]
async fn install_update(app: tauri::AppHandle, state: tauri::State<'_, Shell>) -> Result<(), String> {
    let update = state.pending.lock().unwrap().take().ok_or("no update to install; check again")?;
    update.download_and_install(|_, _| {}, || {}).await.map_err(|e| e.to_string())?;
    app.restart();
}

fn show_main(app: &tauri::AppHandle) {
    if let Some(w) = app.get_webview_window("main") {
        let _ = w.unminimize();
        let _ = w.show();
        let _ = w.set_focus();
    }
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    let app = tauri::Builder::default()
        // Must be registered first: a second launch just focuses the running app.
        .plugin(tauri_plugin_single_instance::init(|app, _args, _cwd| show_main(app)))
        .plugin(tauri_plugin_updater::Builder::new().build())
        .plugin(tauri_plugin_dialog::init())
        .manage(Shell::default())
        .invoke_handler(tauri::generate_handler![daemon_info, pick_folder, install_cli, check_update, install_update])
        .on_window_event(|window, event| {
            // Closing the window hides it; agents keep working in the daemon either way.
            if let WindowEvent::CloseRequested { api, .. } = event {
                api.prevent_close();
                let _ = window.hide();
            }
        })
        .build(tauri::generate_context!())
        .expect("error while building layer8");
    app.run(|app, event| {
        #[cfg(target_os = "macos")]
        if let RunEvent::Reopen { .. } = event {
            show_main(app);
        }
        let _ = (app, event);
    });
}
