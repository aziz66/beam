use tauri::{
    AppHandle, CustomMenuItem, Manager, SystemTray, SystemTrayEvent, SystemTrayMenu,
    SystemTrayMenuItem,
};

use crate::AppState;

pub fn create_tray() -> SystemTray {
    let menu = SystemTrayMenu::new()
        .add_item(CustomMenuItem::new("status", "Not connected").disabled())
        .add_native_item(SystemTrayMenuItem::Separator)
        .add_item(CustomMenuItem::new("copy_link", "Copy Room Link"))
        .add_item(CustomMenuItem::new("open_browser", "Open in Browser"))
        .add_native_item(SystemTrayMenuItem::Separator)
        .add_item(CustomMenuItem::new("settings", "Settings"))
        .add_native_item(SystemTrayMenuItem::Separator)
        .add_item(CustomMenuItem::new("quit", "Quit"));

    SystemTray::new().with_menu(menu)
}

pub fn handle_tray_event(app: &AppHandle, event: SystemTrayEvent) {
    if let SystemTrayEvent::MenuItemClick { id, .. } = event {
        match id.as_str() {
            "quit" => app.exit(0),

            "settings" => {
                if let Some(window) = app.get_window("settings") {
                    let _ = window.show();
                    let _ = window.set_focus();
                }
            }

            "copy_link" => {
                let state = app.state::<AppState>();
                let url = { state.config.lock().unwrap().as_ref().map(room_url) };
                if let Some(url) = url {
                    if let Err(e) = crate::clipboard::write_clipboard(&url) {
                        eprintln!("beam-agent: copy link failed: {}", e);
                    }
                } else {
                    // No room connected — open settings
                    if let Some(window) = app.get_window("settings") {
                        let _ = window.show();
                        let _ = window.set_focus();
                    }
                }
            }

            "open_browser" => {
                let state = app.state::<AppState>();
                let url = { state.config.lock().unwrap().as_ref().map(room_url) };
                if let Some(url) = url {
                    open_url(app, &url);
                } else {
                    // No room connected — open settings so user can connect first
                    if let Some(window) = app.get_window("settings") {
                        let _ = window.show();
                        let _ = window.set_focus();
                    }
                }
            }

            _ => {}
        }
    }
}

fn open_url(app: &AppHandle, url: &str) {
    // Only allow http(s) URLs to prevent protocol-handler abuse via user-supplied server_url.
    if !url.starts_with("http://") && !url.starts_with("https://") {
        #[cfg(debug_assertions)]
        eprintln!("open_url: rejected non-http(s) URL");
        return;
    }
    let _ = tauri::api::shell::open(&app.shell_scope(), url, None);
}

fn room_url(config: &crate::room::RoomConfig) -> String {
    format!(
        "{}/r/{}#{}",
        config.server_url.trim_end_matches('/'),
        config.room_code,
        config.encryption_key
    )
}

/// Update the tray status label. Called from ws.rs on connect/disconnect.
pub fn set_status(app: &AppHandle, connected: bool) {
    let label = if connected {
        "● Connected"
    } else {
        "Not connected"
    };
    let _ = app.tray_handle().get_item("status").set_title(label);
}
