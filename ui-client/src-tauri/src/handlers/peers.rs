use serde::{Deserialize, Serialize};

use super::settings::read_user_config;

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Contact {
    pub id: [u8; 32],
    pub addr: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub relay: Option<String>,
}

#[tauri::command]
pub fn list_closest(target: Option<String>, k: Option<u32>) -> Result<Vec<Contact>, String> {
    let cfg = read_user_config()?;
    let port = if cfg.http_port != 0 {
        cfg.http_port
    } else {
        8012
    };
    let mut url = format!("http://0.0.0.0:{}/closest", port);
    let mut sep = '?';
    if let Some(t) = target.as_ref().filter(|s| !s.is_empty()) {
        url.push(sep);
        url.push_str(&format!("target={}", urlencoding::encode(t)));
        sep = '&';
    }
    if let Some(k) = k {
        url.push(sep);
        url.push_str(&format!("k={}", k));
    }

    let client = reqwest::blocking::Client::new();
    let resp = client
        .get(&url)
        .header("accept", "application/json")
        .send()
        .map_err(|e| format!("request failed: {e}"))?;
    if !resp.status().is_success() {
        let status = resp.status();
        let body = resp.text().unwrap_or_default();
        return Err(format!("server error {}: {}", status, body));
    }
    let contacts: Vec<Contact> = resp.json().map_err(|e| format!("decode failed: {e}"))?;
    Ok(contacts)
}
