import { useEffect, useMemo, useState } from "react";
import "./App.css";
import FilesPage from "./Files";
import PeersPage from "./Peers";
import SettingsPage from "./Settings";
import DaemonPage from "./Daemon";
import { invoke } from "@tauri-apps/api/core";

type Tab = "files" | "peers" | "settings" | "daemon";

function App() {
  const [tab, setTab] = useState<Tab>("files");
  const [serviceStatus, setServiceStatus] = useState<string>("unknown");

  const SERVICE = "peerdrived";
  const isServiceActive = useMemo(() => serviceStatus.trim() === "active", [serviceStatus]);

  async function refreshServiceStatus() {
    try {
      const s = await invoke<string>("service_status", { name: SERVICE });
      setServiceStatus(s);
    } catch {
      // keep previous status on error
    }
  }

  useEffect(() => {
    void refreshServiceStatus();
    const t = setInterval(() => void refreshServiceStatus(), 3000);
    return () => clearInterval(t);
  }, []);

  return (
    <div className="app-root">
      <header className="app-header">
        <div className="brand">PeerDrive</div>
        <nav className="tabs">
          <button
            className={`tab ${tab === "files" ? "active" : ""}`}
            onClick={() => setTab("files")}
          >
            Files
          </button>
          <button
            className={`tab ${tab === "peers" ? "active" : ""}`}
            onClick={() => setTab("peers")}
          >
            Peers
          </button>
          <button
            className={`tab ${tab === "settings" ? "active" : ""}`}
            onClick={() => setTab("settings")}
          >
            Settings
          </button>
          <button
            className={`tab ${tab === "daemon" ? "active" : ""}`}
            onClick={() => setTab("daemon")}
          >
            Daemon
          </button>
        </nav>
      </header>

      <main className="app-content">
        {tab === "files" && (
          isServiceActive ? (
            <FilesPage />
          ) : (
              <div className="muted">Service is not active</div>
          )
        )}
        {tab === "peers" && (
          isServiceActive ? (
            <PeersPage />
          ) : (
              <div className="muted">Service is not active</div>
          )
        )}
        {tab === "settings" && <SettingsPage />}
        {tab === "daemon" && <DaemonPage />}
      </main>
    </div>
  );
}

export default App;
