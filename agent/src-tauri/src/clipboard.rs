use arboard::Clipboard;
use std::collections::hash_map::DefaultHasher;
use std::hash::{Hash, Hasher};
use std::thread;
use std::time::Duration;
use tauri::AppHandle;

/// Poll clipboard every 500ms and emit events on change.
pub fn watch_clipboard(app: AppHandle) {
    let mut clipboard = match Clipboard::new() {
        Ok(c) => c,
        Err(e) => {
            eprintln!("clipboard init failed: {}", e);
            return;
        }
    };

    let mut last_hash: u64 = 0;

    loop {
        thread::sleep(Duration::from_millis(500));

        if let Ok(text) = clipboard.get_text() {
            let hash = hash_string(&text);
            if hash != last_hash && !text.is_empty() {
                last_hash = hash;
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

fn hash_string(s: &str) -> u64 {
    let mut hasher = DefaultHasher::new();
    s.hash(&mut hasher);
    hasher.finish()
}
