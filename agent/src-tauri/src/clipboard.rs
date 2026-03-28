use arboard::Clipboard;
use std::sync::atomic::Ordering;
use std::thread;
use std::time::Duration;
use tauri::{AppHandle, Manager};
use url::Url;

use crate::AppState;

/// Parse a Beam room URL from any domain.
/// Accepts: `<scheme>://<host>[:<port>]/r/<room-code>#<key>`
/// Returns `(server_url, room_code, encryption_key)` if valid, else `None`.
fn parse_beam_url(text: &str) -> Option<(String, String, String)> {
    let url = Url::parse(text.trim()).ok()?;
    let scheme = url.scheme();
    if scheme != "http" && scheme != "https" {
        return None;
    }
    let path = url.path();
    let room_code = path.strip_prefix("/r/")?;
    // room code must be non-empty, no extra path segments, and have ≥2 hyphens
    if room_code.is_empty() || room_code.contains('/') {
        return None;
    }
    if room_code.split('-').count() < 3 {
        return None;
    }
    let fragment = url.fragment()?;
    if fragment.is_empty() {
        return None;
    }
    let host = url.host_str()?;
    let server_url = match url.port() {
        Some(port) => format!("{}://{}:{}", scheme, host, port),
        None => format!("{}://{}", scheme, host),
    };
    Some((server_url, room_code.to_string(), fragment.to_string()))
}

/// Poll clipboard every 500ms; forward changes to WS and emit events.
pub fn watch_clipboard(app: AppHandle) {
    let mut clipboard = match Clipboard::new() {
        Ok(c) => c,
        Err(e) => {
            eprintln!("beam-agent: clipboard init failed: {}", e);
            return;
        }
    };

    let mut last_text = String::new();

    loop {
        thread::sleep(Duration::from_millis(500));

        if let Ok(text) = clipboard.get_text() {
            if text != last_text && !text.is_empty() {
                last_text = text.clone();

                // Forward to WS only if auto_sync is on and text wasn't just received from remote
                let state = app.state::<AppState>();
                let auto_sync = state.config.lock().unwrap()
                    .as_ref()
                    .map(|c| c.auto_sync)
                    .unwrap_or(false);
                if auto_sync {
                    let is_remote = {
                        let last = state.last_remote.lock().unwrap();
                        *last == text
                    };
                    if !is_remote {
                        let ws_tx = state.ws_tx.lock().unwrap();
                        if let Some(tx) = ws_tx.as_ref() {
                            // try_send: drop clipboard change if WS send buffer is full
                            let _ = tx.try_send(text.clone());
                        }
                    }
                }

                let _ = app.emit_all("clipboard-change", text.clone());

                // When not connected, detect Beam room URLs in clipboard and
                // notify the settings window to pre-fill the connection form.
                let connected = state.connected.load(Ordering::SeqCst);
                if !connected {
                    if let Some((server_url, room_code, key)) = parse_beam_url(&text) {
                        let _ = app.emit_all("beam-room-url", serde_json::json!({
                            "server_url": server_url,
                            "room_code": room_code,
                            "encryption_key": key
                        }));
                    }
                }
            }
        }
    }
}

/// Write text to the system clipboard.
pub fn write_clipboard(text: &str) -> Result<(), String> {
    let mut clipboard = Clipboard::new().map_err(|e| e.to_string())?;
    clipboard.set_text(text).map_err(|e| e.to_string())
}
