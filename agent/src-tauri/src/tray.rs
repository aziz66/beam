use tauri::{
    AppHandle, CustomMenuItem, SystemTray, SystemTrayEvent, SystemTrayMenu, SystemTrayMenuItem,
};

pub fn create_tray() -> SystemTray {
    let menu = SystemTrayMenu::new()
        .add_item(CustomMenuItem::new("status", "Disconnected").disabled())
        .add_native_item(SystemTrayMenuItem::Separator)
        .add_item(CustomMenuItem::new("copy_link", "Copy Room Link"))
        .add_item(CustomMenuItem::new("open_browser", "Open in Browser"))
        .add_native_item(SystemTrayMenuItem::Separator)
        .add_item(CustomMenuItem::new("auto_sync", "Auto-sync Clipboard ✓"))
        .add_item(CustomMenuItem::new("settings", "Settings"))
        .add_native_item(SystemTrayMenuItem::Separator)
        .add_item(CustomMenuItem::new("quit", "Quit"));

    SystemTray::new().with_menu(menu)
}

pub fn handle_tray_event(app: &AppHandle, event: SystemTrayEvent) {
    match event {
        SystemTrayEvent::MenuItemClick { id, .. } => match id.as_str() {
            "quit" => {
                std::process::exit(0);
            }
            "settings" => {
                // Open settings window
                if let Some(window) = app.get_window("settings") {
                    let _ = window.show();
                    let _ = window.set_focus();
                }
            }
            "copy_link" => {
                let _ = app.emit_all("copy-room-link", ());
            }
            "open_browser" => {
                let _ = app.emit_all("open-in-browser", ());
            }
            "auto_sync" => {
                let _ = app.emit_all("toggle-auto-sync", ());
            }
            _ => {}
        },
        _ => {}
    }
}
