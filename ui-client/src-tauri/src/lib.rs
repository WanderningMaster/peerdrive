mod handlers;
use handlers::daemon::{read_startup_flags, write_startup_flags};
use handlers::daemon::{
    service_restart, service_start, service_status, service_stop, start_journal_stream,
    stop_journal_stream, LogStreamState,
};
use handlers::peers::list_closest;
use handlers::store::{store_size, gc_store};
use handlers::pins::{add_and_pin_file, list_pins, pin_by_cid};
use handlers::settings::{read_user_config, write_user_config};

// Learn more about Tauri commands at https://tauri.app/develop/calling-rust/
#[tauri::command]
fn greet(name: &str) -> String {
    format!("Hello, {}! You've been greeted from Rust!", name)
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    std::env::set_var("WEBKIT_DISABLE_DMABUF_RENDERER", "1");

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
            add_and_pin_file,
            pin_by_cid,
            store_size,
            gc_store
        ])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
