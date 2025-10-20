use serde::Serialize;
use std::fs;
use std::io::{BufRead, BufReader};
use std::process::{Child, Command, Stdio};
use std::sync::{Arc, Mutex};
use tauri::{AppHandle, Emitter, State};

fn unit_name(name: &str) -> String {
    if name.ends_with(".service") {
        name.to_string()
    } else {
        format!("{name}.service")
    }
}

fn run_systemctl(args: &[&str]) -> Result<String, String> {
    let output = Command::new("systemctl")
        .env("SYSTEMD_COLORS", "0")
        .arg("--user")
        .arg("--no-pager")
        .args(args)
        .output()
        .map_err(|e| format!("failed to run systemctl: {e}"))?;
    if output.status.success() {
        Ok(String::from_utf8_lossy(&output.stdout).trim().to_string())
    } else {
        Err(String::from_utf8_lossy(&output.stderr).trim().to_string())
    }
}

#[tauri::command]
pub fn service_status(name: &str) -> Result<String, String> {
    let unit = unit_name(name);
    let output = Command::new("systemctl")
        .arg("--user")
        .arg("is-active")
        .arg(&unit)
        .output()
        .map_err(|e| format!("failed to run systemctl: {e}"))?;

    let stdout = String::from_utf8_lossy(&output.stdout).trim().to_string();
    let stderr = String::from_utf8_lossy(&output.stderr).trim().to_string();

    // systemctl is-active returns non-zero exit status for states other than "active"
    // but still prints the state string (e.g., "inactive", "failed") to stdout.
    if !stdout.is_empty() {
        return Ok(stdout);
    }

    // If the unit is missing, stderr usually contains a not-found message.
    if stderr.contains("could not be found") || stderr.contains("not-found") {
        return Ok("not-found".into());
    }

    // Fallback: surface stderr or a generic unknown state
    if !stderr.is_empty() {
        Err(stderr)
    } else {
        Ok("unknown".into())
    }
}

#[tauri::command]
pub fn service_start(name: &str) -> Result<(), String> {
    let unit = unit_name(name);
    // --no-block returns immediately and avoids UI hangs while unit activates
    run_systemctl(&["start", "--no-block", &unit]).map(|_| ())
}

#[tauri::command]
pub fn service_stop(name: &str) -> Result<(), String> {
    let unit = unit_name(name);
    run_systemctl(&["stop", "--no-block", &unit]).map(|_| ())
}

#[tauri::command]
pub fn service_restart(name: &str) -> Result<(), String> {
    let unit = unit_name(name);
    run_systemctl(&["restart", "--no-block", &unit]).map(|_| ())
}

#[derive(Default)]
pub struct LogStreamState(pub Arc<Mutex<Option<Child>>>);

#[derive(Clone, Serialize)]
struct LogLinePayload {
    service: String,
    line: String,
}

#[tauri::command]
pub fn start_journal_stream(
    app: AppHandle,
    state: State<LogStreamState>,
    name: &str,
) -> Result<(), String> {
    let unit = unit_name(name);

    // Stop any existing stream
    if let Some(mut existing) = state.0.lock().unwrap().take() {
        let _ = existing.kill();
        let _ = existing.wait();
    }

    let mut child = Command::new("journalctl")
        .arg("--user")
        .arg("-u")
        .arg(&unit)
        .arg("-f") // follow
        .arg("-n")
        .arg("200") // last 200 lines first
        .arg("-o")
        .arg("short-iso")
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .map_err(|e| format!("failed to spawn journalctl: {e}"))?;

    let stdout = child.stdout.take().ok_or("no stdout from journalctl")?;
    let app_clone = app.clone();
    let unit_clone = unit.clone();

    // Reader thread to emit log lines as events
    std::thread::spawn(move || {
        let reader = BufReader::new(stdout);
        for line in reader.lines() {
            match line {
                Ok(l) => {
                    let _ = app_clone.emit(
                        "daemon://logs",
                        LogLinePayload {
                            service: unit_clone.clone(),
                            line: l,
                        },
                    );
                }
                Err(_) => break,
            }
        }
    });

    *state.0.lock().unwrap() = Some(child);
    Ok(())
}

#[tauri::command]
pub fn stop_journal_stream(state: State<LogStreamState>) -> Result<(), String> {
    if let Some(mut child) = state.0.lock().unwrap().take() {
        let _ = child.kill();
        let _ = child.wait();
    }
    Ok(())
}

#[tauri::command]
pub fn read_startup_flags(name: &str) -> Result<String, String> {
    let unit = unit_name(name);
    let cfg = dirs::config_dir().ok_or("cannot resolve user config dir")?;
    let path = cfg.join("systemd/user").join(format!("{}", unit));

    if !path.exists() {
        return Ok(String::new());
    }
    let data = fs::read_to_string(&path).map_err(|e| format!("failed to read unit file: {e}"))?;

    let mut exec_line: Option<String> = None;
    for line in data.lines() {
        let l = line.trim();
        if l.starts_with("ExecStart=") {
            let value = l.trim_start_matches("ExecStart=").trim();
            exec_line = Some(value.to_string())
        }
    }
    let init_cmd = "/usr/local/bin/peerdrive init";
    if let Some(exec_line) = exec_line {
        if let Some((_, flags)) = exec_line.split_once(init_cmd) {
            return Ok(flags.to_string());
        }
    }

    return Ok(String::new());
}

#[tauri::command]
pub fn write_startup_flags(name: &str, flags: &str) -> Result<(), String> {
    let unit = unit_name(name);
    let cfg = dirs::config_dir().ok_or("cannot resolve user config dir")?;
    let path = cfg.join("systemd/user").join(format!("{}", unit));

    if !path.exists() {
        return Ok(());
    }
    let data = fs::read_to_string(&path).map_err(|e| format!("failed to read unit file: {e}"))?;

    let mut new_lines = Vec::new();
    let init_cmd = "/usr/local/bin/peerdrive init";
    for line in data.lines() {
        if let Some(_) = line.trim().strip_prefix("ExecStart=") {
            let new_exec = format!("ExecStart={} {}", init_cmd, flags.trim());
            new_lines.push(new_exec);
        } else {
            new_lines.push(line.to_string());
        }
    }
    fs::write(&path, new_lines.join("\n"))
        .map_err(|e| format!("failed to write unit file: {e}"))?;
    Ok(())
}
