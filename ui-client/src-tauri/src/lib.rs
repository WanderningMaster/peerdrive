mod handlers;
use handlers::settings::{read_user_config, write_user_config};
use handlers::daemon::{
    service_restart, service_start, service_status, service_stop, start_journal_stream, stop_journal_stream,
    LogStreamState,
};
use handlers::daemon::{read_startup_flags, write_startup_flags};
use handlers::peers::list_closest;
use handlers::pins::{list_pins, add_and_pin_file, pin_by_cid};

// Learn more about Tauri commands at https://tauri.app/develop/calling-rust/
#[tauri::command]
fn greet(name: &str) -> String {
    format!("Hello, {}! You've been greeted from Rust!", name)
}


#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_opener::init())
        .plugin(tauri_plugin_dialog::init())
        .manage(LogStreamState::default())
        .invoke_handler(tauri::generate_handler![
            greet,
            read_user_config,
            write_user_config,
            service_status,
            service_start,
            service_stop,
            service_restart,
            start_journal_stream,
            stop_journal_stream,
            read_startup_flags,
            write_startup_flags,
            list_closest,
            list_pins,
            add_and_pin_file
            , pin_by_cid
        ])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
