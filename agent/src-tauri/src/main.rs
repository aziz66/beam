// Beam Desktop Agent — Tauri tray app for clipboard auto-sync

#![cfg_attr(
    all(not(debug_assertions), target_os = "windows"),
    windows_subsystem = "windows"
)]

mod clipboard;
mod room;
mod tray;
mod ws;

use std::io::{Read, Write};
use std::net::{TcpListener, TcpStream};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use tauri::{AppHandle, Manager};
use tokio::sync::mpsc;
use url::Url;

/// Local port used for single-instance forwarding (loopback only, no auth needed).
const SI_PORT: u16 = 33_820;

pub struct AppState {
    pub ws_tx: std::sync::Mutex<Option<mpsc::Sender<String>>>,
    pub cancel: std::sync::Mutex<Arc<AtomicBool>>,
    pub config: std::sync::Mutex<Option<room::RoomConfig>>,
    pub connected: Arc<AtomicBool>,
    pub last_remote: Arc<std::sync::Mutex<String>>,
}

#[tauri::command]
async fn connect_room(config: room::RoomConfig, app: tauri::AppHandle) -> Result<String, String> {
    let state = app.state::<AppState>();
    {
        let old_cancel = state.cancel.lock().unwrap().clone();
        old_cancel.store(true, Ordering::SeqCst);
    }
    *state.ws_tx.lock().unwrap() = None;
    state.connected.store(false, Ordering::SeqCst);
    *state.config.lock().unwrap() = Some(config.clone());
    if let Err(e) = room::save_credentials(&config) {
        #[cfg(debug_assertions)]
        eprintln!("failed to save credentials: {}", e);
        let _ = e;
    }
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
    Ok(app.state::<AppState>().config.lock().unwrap().clone())
}

#[tauri::command]
async fn get_status(app: tauri::AppHandle) -> Result<String, String> {
    let connected = app.state::<AppState>().connected.load(Ordering::SeqCst);
    Ok(if connected { "connected".to_string() } else { "disconnected".to_string() })
}

/// Parse a `beam://` or `beams://` deep-link URL and connect to the room.
fn handle_beam_url(app: &AppHandle, url: &str) {
    let parsed = match Url::parse(url.trim()) {
        Ok(u) => u,
        Err(e) => { eprintln!("beam: invalid deep-link URL '{}': {}", url, e); return; }
    };
    let http_scheme = if parsed.scheme() == "beams" { "https" } else { "http" };
    let host = match parsed.host_str() { Some(h) => h, None => return };
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

    let config = room::RoomConfig { server_url, room_code, encryption_key: key, auto_sync: true, passphrase: None };
    let state = app.state::<AppState>();
    {
        let old_cancel = state.cancel.lock().unwrap().clone();
        old_cancel.store(true, Ordering::SeqCst);
    }
    *state.ws_tx.lock().unwrap() = None;
    state.connected.store(false, Ordering::SeqCst);
    *state.config.lock().unwrap() = Some(config.clone());
    if let Err(e) = room::save_credentials(&config) { eprintln!("beam: save credentials: {}", e); }

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

    if let Some(window) = app.get_window("settings") {
        let _ = window.show();
        let _ = window.set_focus();
    }
}

/// Try to send the URL to an already-running instance on the loopback port.
/// Returns true if the running instance received it (this process should exit).
fn try_forward_to_running_instance(url: &str) -> bool {
    match TcpStream::connect_timeout(
        &format!("127.0.0.1:{}", SI_PORT).parse().unwrap(),
        std::time::Duration::from_millis(300),
    ) {
        Ok(mut stream) => {
            let _ = stream.write_all(url.as_bytes());
            true
        }
        Err(_) => false,
    }
}

/// Bind the single-instance listener. Incoming URLs are forwarded to `app` via handle_beam_url.
/// Must be called after the AppHandle is available (inside setup).
fn start_single_instance_listener(app: AppHandle) {
    std::thread::spawn(move || {
        let listener = match TcpListener::bind(format!("127.0.0.1:{}", SI_PORT)) {
            Ok(l) => l,
            Err(e) => { eprintln!("beam: single-instance listener bind failed: {}", e); return; }
        };
        for stream in listener.incoming() {
            if let Ok(mut s) = stream {
                let mut buf = String::new();
                let _ = s.read_to_string(&mut buf);
                let url = buf.trim().to_string();
                if !url.is_empty() {
                    handle_beam_url(&app, &url);
                }
            }
        }
    });
}

/// Register beam:// and beams:// URL schemes in HKCU — no admin required.
#[cfg(target_os = "windows")]
fn register_url_schemes() {
    use winreg::enums::{HKEY_CURRENT_USER, KEY_SET_VALUE};
    use winreg::RegKey;
    let exe = match std::env::current_exe() { Ok(p) => p, Err(_) => return };
    let cmd_value = format!("\"{}\" \"%1\"", exe.to_string_lossy());
    let hkcu = RegKey::predef(HKEY_CURRENT_USER);
    for scheme in &["beam", "beams"] {
        if let Ok((key, _)) = hkcu.create_subkey(format!("SOFTWARE\\Classes\\{}", scheme)) {
            let _ = key.set_value("", &format!("URL:{} Protocol", scheme));
            let _ = key.set_value("URL Protocol", &"");
        }
        if let Ok((cmd_key, _)) = hkcu.create_subkey_with_flags(
            format!("SOFTWARE\\Classes\\{}\\shell\\open\\command", scheme), KEY_SET_VALUE
        ) {
            let _ = cmd_key.set_value("", &cmd_value);
        }
    }
}

#[cfg(not(target_os = "windows"))]
fn register_url_schemes() {}

fn main() {
    register_url_schemes();

    let deep_link_url: Option<String> = std::env::args()
        .nth(1)
        .filter(|a| a.starts_with("beam://") || a.starts_with("beams://"));

    // If another instance is already running, forward the URL to it and exit.
    // This prevents a broken second instance from starting.
    if let Some(url) = &deep_link_url {
        if try_forward_to_running_instance(url) {
            return;
        }
    }

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
        .setup(move |app| {
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

            // Start single-instance listener so future beam:// clicks reach this instance.
            start_single_instance_listener(app.handle());

            if let Some(url) = deep_link_url {
                // Launched via deep link (no running instance found) — connect after a
                // short delay to let the window finish initialising.
                let handle = app.handle();
                std::thread::spawn(move || {
                    std::thread::sleep(std::time::Duration::from_millis(400));
                    handle_beam_url(&handle, &url);
                });
            } else {
                // Normal startup — auto-reconnect to last saved room.
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
            }

            let handle = app.handle();
            std::thread::spawn(move || clipboard::watch_clipboard(handle));

            Ok(())
        })
        .run(tauri::generate_context!())
        .expect("error running beam agent");
}
