use serde::{Deserialize, Serialize};

use super::settings::read_user_config;

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct PinInfo {
    pub cid: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub name: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub mime: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub size: Option<u64>,
}

#[tauri::command]
pub fn list_pins() -> Result<Vec<PinInfo>, String> {
    let cfg = read_user_config()?;
    let port = cfg.http_port as u16;
    if port == 0 {
        return Err("HTTP port is not configured".into());
    }
    let url = format!("http://127.0.0.1:{}/pins", port);
    let resp = reqwest::blocking::get(url).map_err(|e| format!("request failed: {e}"))?;
    if !resp.status().is_success() {
        return Err(format!("bad status: {}", resp.status()));
    }
    let pins: Vec<PinInfo> = resp
        .json()
        .map_err(|e| format!("json decode failed: {e}"))?;
    Ok(pins)
}

#[derive(Debug, Clone, Deserialize)]
struct PutResp { cid: String }

#[tauri::command]
pub fn add_and_pin_file(path: String, compress: Option<bool>) -> Result<String, String> {
    let cfg = read_user_config()?;
    let port = cfg.http_port as u16;
    if port == 0 {
        return Err("HTTP port is not configured".into());
    }

    // 1) Add from path -> get CID
    let add_compress = compress.unwrap_or(false);
    let url_put = if add_compress {
        format!(
            "http://127.0.0.1:{}/dfs/put?in={}&compress=1",
            port,
            urlencoding::encode(path.as_str())
        )
    } else {
        format!(
            "http://127.0.0.1:{}/dfs/put?in={}",
            port,
            urlencoding::encode(path.as_str())
        )
    };
    let resp = reqwest::blocking::get(url_put).map_err(|e| format!("request failed: {e}"))?;
    if !resp.status().is_success() {
        return Err(format!("bad status: {}", resp.status()));
    }
    let put: PutResp = resp.json().map_err(|e| format!("json decode failed: {e}"))?;
    let cid = put.cid;

    // 2) Pin the CID unless distributed (compress) mode is used.
    // In distributed mode the service pins the manifest directly to avoid retaining all children.
    if !add_compress {
        let url_pin = format!(
            "http://127.0.0.1:{}/pin?cid={}",
            port,
            urlencoding::encode(cid.as_str())
        );
        let resp2 = reqwest::blocking::get(url_pin).map_err(|e| format!("request failed: {e}"))?;
        if !resp2.status().is_success() {
            return Err(format!("pin failed: {}", resp2.status()));
        }
    }

    Ok(cid)
}

/// Pin an existing CID via the local HTTP API.
#[tauri::command]
pub fn pin_by_cid(cid: String) -> Result<(), String> {
    if cid.trim().is_empty() {
        return Err("cid is required".into());
    }
    let cfg = read_user_config()?;
    let port = cfg.http_port as u16;
    if port == 0 {
        return Err("HTTP port is not configured".into());
    }
    let url_pin = format!(
        "http://127.0.0.1:{}/pin?cid={}",
        port,
        urlencoding::encode(cid.as_str())
    );
    let resp = reqwest::blocking::get(url_pin).map_err(|e| format!("request failed: {e}"))?;
    if !resp.status().is_success() {
        return Err(format!("pin failed: {}", resp.status()));
    }
    Ok(())
}
