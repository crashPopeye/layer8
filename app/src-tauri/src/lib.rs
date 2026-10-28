//! The desktop shell is deliberately thin: all state and agent work lives in
//! layer8d, so sessions keep running when the window is closed.

use std::net::TcpStream;
use std::path::PathBuf;
use std::process::{Command, Stdio};
use std::time::Duration;

#[derive(serde::Serialize)]
struct DaemonInfo {
    url: String,
    token: String,
}

fn layer8_home() -> PathBuf {
    if let Ok(h) = std::env::var("LAYER8_HOME") {
        return PathBuf::from(h);
    }
    let home = std::env::var("HOME").or_else(|_| std::env::var("USERPROFILE")).unwrap_or_default();
    PathBuf::from(home).join(".layer8")
}

fn daemon_port() -> u16 {
    if let Some(p) = std::env::var("LAYER8_PORT").ok().and_then(|p| p.parse().ok()) {
        return p;
    }
    std::fs::read_to_string(layer8_home().join("config.json"))
        .ok()
        .and_then(|s| serde_json::from_str::<serde_json::Value>(&s).ok())
        .and_then(|v| v.get("port")?.as_u64())
        .and_then(|p| u16::try_from(p).ok())
        .unwrap_or(7878)
}

fn daemon_up(port: u16) -> bool {
    TcpStream::connect_timeout(&([127, 0, 0, 1], port).into(), Duration::from_millis(250)).is_ok()
}

/// Starts `layer8d` from PATH (or LAYER8D_CMD) when nothing is listening yet.
/// The daemon is detached so closing the window never stops agents.
fn ensure_daemon(port: u16) {
    if daemon_up(port) {
        return;
    }
    let cmd = std::env::var("LAYER8D_CMD").unwrap_or_else(|_| "layer8d".into());
    let mut parts = cmd.split_whitespace();
    let Some(bin) = parts.next() else { return };
    let spawned = Command::new(bin)
        .args(parts)
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn();
    if let Err(err) = spawned {
        eprintln!("layer8: could not start daemon ({cmd}): {err}");
        return;
    }
    for _ in 0..40 {
        if daemon_up(port) {
            return;
        }
        std::thread::sleep(Duration::from_millis(100));
    }
}

#[tauri::command]
fn daemon_info() -> Result<DaemonInfo, String> {
    let port = daemon_port();
    ensure_daemon(port);
    let token = std::fs::read_to_string(layer8_home().join("token"))
        .map_err(|e| format!("cannot read daemon token: {e}. Is layer8d running?"))?;
    Ok(DaemonInfo { url: format!("http://127.0.0.1:{port}"), token: token.trim().to_string() })
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_opener::init())
        .invoke_handler(tauri::generate_handler![daemon_info])
        .run(tauri::generate_context!())
        .expect("error while running layer8");
}
