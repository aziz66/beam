use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RoomConfig {
    pub server_url: String,
    pub room_code: String,
    pub encryption_key: String,
    pub auto_sync: bool,
    /// Optional passphrase for passphrase-protected pinned rooms.
    /// Omitted from serialization when None so existing keyring entries
    /// that predate this field deserialize without error.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub passphrase: Option<String>,
}

impl Default for RoomConfig {
    fn default() -> Self {
        Self {
            server_url: "http://localhost:8080".to_string(),
            room_code: String::new(),
            encryption_key: String::new(),
            auto_sync: true,
        }
    }
}

/// Store room credentials in the OS keyring.
pub fn save_credentials(config: &RoomConfig) -> Result<(), String> {
    let entry = keyring::Entry::new("beam-agent", "room-config").map_err(|e| e.to_string())?;
    let json = serde_json::to_string(config).map_err(|e| e.to_string())?;
    entry.set_password(&json).map_err(|e| e.to_string())
}

/// Load room credentials from the OS keyring.
pub fn load_credentials() -> Result<RoomConfig, String> {
    let entry = keyring::Entry::new("beam-agent", "room-config").map_err(|e| e.to_string())?;
    let json = entry.get_password().map_err(|e| e.to_string())?;
    serde_json::from_str(&json).map_err(|e| e.to_string())
}
