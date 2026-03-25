use arboard::Clipboard;
use std::thread;
use std::time::Duration;
use tauri::{AppHandle, Manager};


use crate::AppState;

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

                let _ = app.emit_all("clipboard-change", text);
            }
        }
    }
}

/// Write text to the system clipboard.
pub fn write_clipboard(text: &str) -> Result<(), String> {
    let mut clipboard = Clipboard::new().map_err(|e| e.to_string())?;
    clipboard.set_text(text).map_err(|e| e.to_string())
}
