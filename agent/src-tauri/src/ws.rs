use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;

use base64::{engine::general_purpose::STANDARD as BASE64, Engine as _};
use futures_util::{SinkExt, StreamExt};
use rand::rngs::OsRng;
use rand::RngCore;
use serde_json::{json, Value};
use tokio::sync::mpsc;
use tokio::time::{sleep, Duration};
use tokio_tungstenite::{
    connect_async_with_config,
    tungstenite::{protocol::WebSocketConfig, Message},
};
use xsalsa20poly1305::{
    aead::{Aead, KeyInit},
    Key, Nonce, XSalsa20Poly1305,
};

use tauri::Manager;

use crate::room::RoomConfig;

pub async fn run_ws(
    config: RoomConfig,
    mut rx: mpsc::Receiver<String>,
    cancel: Arc<AtomicBool>,
    connected: Arc<AtomicBool>,
    last_remote: Arc<std::sync::Mutex<String>>,
    app: tauri::AppHandle,
) {
    let ws_url = config
        .server_url
        .trim_end_matches('/')
        .replace("https://", "wss://")
        .replace("http://", "ws://");

    let key_bytes = match BASE64.decode(&config.encryption_key) {
        Ok(b) if b.len() == 32 => b,
        Ok(b) => {
            eprintln!("beam-agent: encryption key must be 32 bytes, got {}", b.len());
            return;
        }
        Err(e) => {
            eprintln!("beam-agent: invalid base64 encryption key: {}", e);
            return;
        }
    };

    let mut retry_delay_secs: u64 = 1;

    loop {
        if cancel.load(Ordering::SeqCst) {
            break;
        }

        // Build WS URL. Use simple string concat — path_segments_mut().push()
        // appends after a trailing empty segment causing a double-slash.
        let url = format!("{}/ws/{}", ws_url.trim_end_matches('/'), config.room_code);

        // Limit incoming WS frames to 10 MB — prevents a rogue server from
        // allocating unbounded memory by sending a giant single message.
        let ws_cfg = WebSocketConfig {
            max_frame_size: Some(10 * 1024 * 1024),
            max_message_size: Some(10 * 1024 * 1024),
            ..Default::default()
        };

        #[cfg(debug_assertions)]
        eprintln!("connecting to: {}", url);
        let ws_stream = match connect_async_with_config(&url, Some(ws_cfg), false).await {
            Ok((stream, _)) => stream,
            Err(e) => {
                #[cfg(debug_assertions)]
                eprintln!("ws connect failed: {}, retry in {}s", e, retry_delay_secs);
                sleep(Duration::from_secs(retry_delay_secs)).await;
                retry_delay_secs = (retry_delay_secs * 2).min(30);
                continue;
            }
        };

        #[cfg(debug_assertions)]
        eprintln!("connected!");
        connected.store(true, Ordering::SeqCst);
        crate::tray::set_status(&app, true);
        retry_delay_secs = 1;

        let (mut write, mut read) = ws_stream.split();

        // Send join message so peers see "Desktop Agent" as the label.
        // Include passphrase if the room requires one.
        let mut join_payload = json!({
            "room_code": config.room_code,
            "device_label": "Desktop Agent"
        });
        if let Some(ref p) = config.passphrase {
            if !p.is_empty() {
                join_payload["passphrase"] = json!(p);
            }
        }
        let join_msg = json!({
            "type": "join",
            "payload": join_payload,
            "ts": std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap_or_default()
                .as_millis() as u64
        });
        if write.send(Message::Text(join_msg.to_string())).await.is_err() {
            connected.store(false, Ordering::SeqCst);
            crate::tray::set_status(&app, false);
            sleep(Duration::from_secs(retry_delay_secs)).await;
            retry_delay_secs = (retry_delay_secs * 2).min(30);
            continue;
        }

        let key = Key::from_slice(&key_bytes);
        let cipher = XSalsa20Poly1305::new(key);

        // Single loop: select! on incoming WS messages and outgoing clipboard sends
        loop {
            if cancel.load(Ordering::SeqCst) {
                connected.store(false, Ordering::SeqCst);
                crate::tray::set_status(&app, false);
                return;
            }

            tokio::select! {
                msg = read.next() => {
                    match msg {
                        Some(Ok(Message::Text(text))) => {
                            if let Ok(val) = serde_json::from_str::<Value>(&text) {
                                if val["type"] == "error"
                                    && val["payload"]["code"] == "auth_required"
                                {
                                    // Room requires a passphrase we don't have — stop retrying
                                    // and surface the settings window so the user can provide it.
                                    connected.store(false, Ordering::SeqCst);
                                    crate::tray::set_status(&app, false);
                                    if let Some(window) = app.get_window("settings") {
                                        let _ = window.show();
                                        let _ = window.set_focus();
                                    }
                                    let _ = app.emit_all("auth-required", ());
                                    return; // stop the retry loop entirely
                                }
                                if val["type"] == "item" {
                                    let payload = &val["payload"];
                                    if payload["kind"] == "text" {
                                        if let (Some(enc_b64), Some(nonce_b64)) = (
                                            payload["encrypted_data"].as_str(),
                                            payload["nonce"].as_str(),
                                        ) {
                                            if let (Ok(enc_bytes), Ok(nonce_bytes)) = (
                                                BASE64.decode(enc_b64),
                                                BASE64.decode(nonce_b64),
                                            ) {
                                                if nonce_bytes.len() == 24 {
                                                    let nonce = Nonce::from_slice(&nonce_bytes);
                                                    if let Ok(plain) = cipher.decrypt(nonce, enc_bytes.as_slice()) {
                                                        if let Ok(plain_text) = String::from_utf8(plain) {
                                                            // Set last_remote BEFORE writing clipboard so the
                                                            // clipboard watcher sees it and won't re-send this text.
                                                            *last_remote.lock().unwrap() = plain_text.clone();
                                                            let _ = crate::clipboard::write_clipboard(&plain_text);
                                                            #[cfg(debug_assertions)]
                                                            eprintln!("remote clipboard: {} chars", plain_text.len());
                                                        }
                                                    }
                                                }
                                            }
                                        }
                                    }
                                }
                            }
                        }
                        Some(Ok(Message::Close(_))) | Some(Err(_)) | None => break,
                        _ => {}
                    }
                }

                text = rx.recv() => {
                    match text {
                        Some(text) => {
                            let mut nonce_bytes = [0u8; 24];
                            OsRng.fill_bytes(&mut nonce_bytes);
                            let nonce = Nonce::from_slice(&nonce_bytes);
                            let mut id_bytes = [0u8; 8];
                            OsRng.fill_bytes(&mut id_bytes);
                            let item_id = format!("{:016x}", u64::from_le_bytes(id_bytes));
                            match cipher.encrypt(nonce, text.as_bytes()) {
                                Ok(ciphertext) => {
                                    let msg = json!({
                                        "type": "item",
                                        "payload": {
                                            "item_id": item_id,
                                            "kind": "text",
                                            "encrypted_data": BASE64.encode(&ciphertext),
                                            "nonce": BASE64.encode(&nonce_bytes),
                                            "device_label": "Desktop Agent"
                                        }
                                    });
                                    if write.send(Message::Text(msg.to_string())).await.is_err() {
                                        break;
                                    }
                                }
                                Err(e) => {
                                    eprintln!("beam-agent: encrypt error: {}", e);
                                }
                            }
                        }
                        None => return, // channel closed — agent is shutting down
                    }
                }
            }
        }

        connected.store(false, Ordering::SeqCst);
        crate::tray::set_status(&app, false);

        #[cfg(debug_assertions)]
        eprintln!("disconnected, retry in {}s", retry_delay_secs);
        sleep(Duration::from_secs(retry_delay_secs)).await;
        retry_delay_secs = (retry_delay_secs * 2).min(30);
    }
}
