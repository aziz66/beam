// Beam Desktop Agent — Tauri tray app for clipboard auto-sync

#![cfg_attr(
    all(not(debug_assertions), target_os = "windows"),
    windows_subsystem = "windows"
)]

mod clipboard;
mod room;
mod tray;
mod ws;

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use tauri::{AppHandle, Manager};
use tokio::sync::mpsc;
use url::Url;

pub struct AppState {
    pub ws_tx: std::sync::Mutex<Option<mpsc::Sender<String>>>,
    /// Per-connection cancel token. Replaced on each connect_room call so the
    /// old task's cancel is set to true independently of the new task's token.
    pub cancel: std::sync::Mutex<Arc<AtomicBool>>,
    pub config: std::sync::Mutex<Option<room::RoomConfig>>,
    pub connected: Arc<AtomicBool>,
    pub last_remote: Arc<std::sync::Mutex<String>>,
}

#[tauri::command]
async fn connect_room(config: room::RoomConfig, app: tauri::AppHandle) -> Result<String, String> {
    let state = app.state::<AppState>();

    // Signal old connection to stop using its own cancel token, then drop its
    // sender so the old task exits immediately via the closed rx channel.
    {
        let old_cancel = state.cancel.lock().unwrap().clone();
        old_cancel.store(true, Ordering::SeqCst);
    }
    *state.ws_tx.lock().unwrap() = None; // drops old tx → closes old rx
    state.connected.store(false, Ordering::SeqCst);

    *state.config.lock().unwrap() = Some(config.clone());

    // Persist credentials so the agent can auto-reconnect on next startup
    if let Err(e) = room::save_credentials(&config) {
        #[cfg(debug_assertions)]
        eprintln!("failed to save credentials: {}", e);
        let _ = e;
    }

    // Create a fresh cancel token for this connection (not shared with old task)
    let cancel = Arc::new(AtomicBool::new(false));
    *state.cancel.lock().unwrap() = cancel.clone();

    let (tx, rx) = mpsc::channel::<String>(64);
    *state.ws_tx.lock().unwrap() = Some(tx);

    let connected = state.connected.clone();
    let last_remote = state.last_remote.clone();

    std::thread::spawn(move || {
        let rt = tokio::runtime::Runtime::new().expect("tokio runtime");
        rt.block_on(ws::run_ws(config, rx, cancel, connected, last_remote, app));
    });

    Ok("spawned".to_string())
}

#[tauri::command]
async fn get_config(app: tauri::AppHandle) -> Result<Option<room::RoomConfig>, String> {
    let state = app.state::<AppState>();
    let config = state.config.lock().unwrap().clone();
    Ok(config)
}

#[tauri::command]
async fn get_status(app: tauri::AppHandle) -> Result<String, String> {
    let state = app.state::<AppState>();
    if state.connected.load(Ordering::SeqCst) {
        Ok("connected".to_string())
    } else {
        Ok("disconnected".to_string())
    }
}

/// Parse a `beam://` or `beams://` deep-link URL and connect to that room.
/// beam://  → http server   beams:// → https server
fn handle_beam_url(app: &AppHandle, url: &str) {
    let parsed = match Url::parse(url) {
        Ok(u) => u,
        Err(e) => { eprintln!("beam: invalid deep-link URL '{}': {}", url, e); return; }
    };

    let http_scheme = if parsed.scheme() == "beams" { "https" } else { "http" };
    let host = match parsed.host_str() {
        Some(h) => h,
        None => return,
    };
    let server_url = match parsed.port() {
        Some(port) => format!("{}://{}:{}", http_scheme, host, port),
        None => format!("{}://{}", http_scheme, host),
    };
    let room_code = parsed.path().trim_matches('/').to_string();
    if room_code.is_empty() { return; }
    let key = match parsed.fragment() {
        Some(f) if !f.is_empty() => f.to_string(),
        _ => { eprintln!("beam: deep-link missing encryption key fragment"); return; }
    };

    let config = room::RoomConfig {
        server_url,
        room_code,
        encryption_key: key,
        auto_sync: true,
        passphrase: None,
    };

    let state = app.state::<AppState>();

    // Stop any existing connection
    {
        let old_cancel = state.cancel.lock().unwrap().clone();
        old_cancel.store(true, Ordering::SeqCst);
    }
    *state.ws_tx.lock().unwrap() = None;
    state.connected.store(false, Ordering::SeqCst);

    *state.config.lock().unwrap() = Some(config.clone());
    if let Err(e) = room::save_credentials(&config) {
        eprintln!("beam: failed to save credentials: {}", e);
    }

    let cancel = Arc::new(AtomicBool::new(false));
    *state.cancel.lock().unwrap() = cancel.clone();
    let (tx, rx) = mpsc::channel::<String>(64);
    *state.ws_tx.lock().unwrap() = Some(tx);

    let connected = state.connected.clone();
    let last_remote = state.last_remote.clone();
    let app_clone = app.clone();

    std::thread::spawn(move || {
        let rt = tokio::runtime::Runtime::new().expect("tokio runtime");
        rt.block_on(ws::run_ws(config, rx, cancel, connected, last_remote, app_clone));
    });

    // Show settings window so user can see the connection being made
    if let Some(window) = app.get_window("settings") {
        let _ = window.show();
        let _ = window.set_focus();
    }
}

fn main() {
    // prepare() must be called before the Tauri builder — on Windows it registers
    // the beam:// and beams:// protocol handlers in HKCU and handles single-instance
    // forwarding (sends the URL to an already-running instance via a named pipe).
    tauri_plugin_deep_link::prepare("sh.beam.agent");

    tauri::Builder::default()
        .manage(AppState {
            ws_tx: std::sync::Mutex::new(None),
            cancel: std::sync::Mutex::new(Arc::new(AtomicBool::new(true))),
            config: std::sync::Mutex::new(None),
            connected: Arc::new(AtomicBool::new(false)),
            last_remote: Arc::new(std::sync::Mutex::new(String::new())),
        })
        .system_tray(tray::create_tray())
        .on_system_tray_event(tray::handle_tray_event)
        .invoke_handler(tauri::generate_handler![connect_room, get_status, get_config])
        .setup(|app| {
            tauri::WindowBuilder::new(
                app,
                "settings",
                tauri::WindowUrl::App("index.html".into()),
            )
            .title("Beam Agent Settings")
            .inner_size(420.0, 460.0)
            .resizable(false)
            .visible(false)
            .build()?;

            // Auto-reconnect to last room if credentials were saved
            if let Ok(config) = room::load_credentials() {
                let handle = app.handle();
                let state = handle.state::<AppState>();
                *state.config.lock().unwrap() = Some(config.clone());
                let cancel = Arc::new(AtomicBool::new(false));
                *state.cancel.lock().unwrap() = cancel.clone();
                let (tx, rx) = mpsc::channel::<String>(64);
                *state.ws_tx.lock().unwrap() = Some(tx);
                let connected = state.connected.clone();
                let last_remote = state.last_remote.clone();
                std::thread::spawn(move || {
                    let rt = tokio::runtime::Runtime::new().expect("tokio runtime");
                    rt.block_on(ws::run_ws(config, rx, cancel, connected, last_remote, handle));
                });
            }

            // Register beam:// and beams:// deep-link handlers
            {
                let handle = app.handle();
                tauri_plugin_deep_link::register("beam", move |url: String| {
                    handle_beam_url(&handle, &url);
                })?;
            }
            {
                let handle = app.handle();
                tauri_plugin_deep_link::register("beams", move |url: String| {
                    handle_beam_url(&handle, &url);
                })?;
            }

            let handle = app.handle();
            std::thread::spawn(move || {
                clipboard::watch_clipboard(handle);
            });

            Ok(())
        })
        .run(tauri::generate_context!())
        .expect("error running beam agent");
}
