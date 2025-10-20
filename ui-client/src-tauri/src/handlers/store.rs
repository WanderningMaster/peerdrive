use serde::{Deserialize, Serialize};

use super::settings::read_user_config;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct StoreSize {
    pub blocks: u64,
    pub bytes: u64,
}

#[tauri::command]
pub fn store_size() -> Result<StoreSize, String> {
    let cfg = read_user_config()?;
    let port = cfg.http_port as u16;
    if port == 0 {
        return Err("HTTP port is not configured".into());
    }
    let url = format!("http://127.0.0.1:{}/store/size", port);
    let resp = reqwest::blocking::get(url).map_err(|e| format!("request failed: {e}"))?;
    if !resp.status().is_success() {
        return Err(format!("bad status: {}", resp.status()));
    }
    let m: StoreSize = resp
        .json()
        .map_err(|e| format!("json decode failed: {e}"))?;
    Ok(m)
}

#[derive(Debug, Clone, Deserialize)]
struct GcResp { freed: i32 }

#[tauri::command]
pub fn gc_store() -> Result<i32, String> {
    let cfg = read_user_config()?;
    let port = cfg.http_port as u16;
    if port == 0 {
        return Err("HTTP port is not configured".into());
    }
    let url = format!("http://127.0.0.1:{}/gc", port);
    let client = reqwest::blocking::Client::new();
    let resp = client
        .post(url)
        .header("Content-Type", "application/json")
        .body("{}")
        .send()
        .map_err(|e| format!("request failed: {e}"))?;
    if !resp.status().is_success() {
        return Err(format!("bad status: {}", resp.status()));
    }
    let m: GcResp = resp.json().map_err(|e| format!("json decode failed: {e}"))?;
    Ok(m.freed)
}
