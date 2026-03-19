// Beam Desktop Agent — Tauri tray app for clipboard auto-sync
// Connects to a Beam server room via WebSocket and syncs clipboard content

#![cfg_attr(
    all(not(debug_assertions), target_os = "windows"),
    windows_subsystem = "windows"
)]

mod clipboard;
mod room;
mod tray;

fn main() {
    tauri::Builder::default()
        .system_tray(tray::create_tray())
        .on_system_tray_event(tray::handle_tray_event)
        .setup(|app| {
            // Start clipboard watcher
            let handle = app.handle();
            std::thread::spawn(move || {
                clipboard::watch_clipboard(handle);
            });
            Ok(())
        })
        .run(tauri::generate_context!())
        .expect("error running beam agent");
}
