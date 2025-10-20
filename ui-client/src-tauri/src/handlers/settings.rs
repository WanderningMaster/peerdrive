use serde::{Deserialize, Serialize};
use std::fs;
use std::io::Write;
use std::path::PathBuf;

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct UserConfig {
    pub node_id: [u8; 32],
    pub tcp_port: u16,
    pub http_port: u16,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub relay: Option<String>,
    pub blockstore_path: String,
}

impl Default for UserConfig {
    fn default() -> Self {
        Self {
            node_id: [0u8; 32],
            tcp_port: 0,
            http_port: 0,
            relay: None,
            blockstore_path: "".to_string(),
        }
    }
}

fn config_path() -> Result<PathBuf, String> {
    let base = dirs::config_dir().ok_or("cannot resolve user config dir")?;
    Ok(base.join("peerdrive").join("config.json"))
}

#[tauri::command]
pub fn read_user_config() -> Result<UserConfig, String> {
    let path = config_path()?;
    if !path.exists() {
        return Ok(UserConfig::default());
    }
    let data = fs::read_to_string(&path).map_err(|e| format!("read failed: {e}"))?;
    let cfg: UserConfig =
        serde_json::from_str(&data).map_err(|e| format!("json parse failed: {e}"))?;
    Ok(cfg)
}

#[tauri::command]
pub fn write_user_config(cfg: UserConfig) -> Result<(), String> {
    let path = config_path()?;
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent).map_err(|e| format!("mkdir failed: {e}"))?;
    }
    let data =
        serde_json::to_string_pretty(&cfg).map_err(|e| format!("json encode failed: {e}"))?;
    let mut f = fs::File::create(&path).map_err(|e| format!("create failed: {e}"))?;
    f.write_all(data.as_bytes())
        .map_err(|e| format!("write failed: {e}"))?;
    Ok(())
}
