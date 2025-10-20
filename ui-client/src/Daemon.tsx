import { useEffect, useMemo, useRef, useState } from "react";
import { invoke } from "@tauri-apps/api/core";
import { listen, UnlistenFn } from "@tauri-apps/api/event";

const SERVICE = "peerdrived";

export default function DaemonPage() {
  const [status, setStatus] = useState<string>("unknown");
  const [busy, setBusy] = useState(false);
  const [streaming, setStreaming] = useState(false);
  const [logs, setLogs] = useState<string[]>([]);
  const [flags, setFlags] = useState("");
  const [flagsSaving, setFlagsSaving] = useState(false);
  const [flagsLoading, setFlagsLoading] = useState(false);
  const unlistenRef = useRef<UnlistenFn | null>(null);
  const logsRef = useRef<HTMLPreElement | null>(null);
  const pollingRef = useRef(false);

  const isActive = useMemo(() => status.trim() === "active", [status]);
  const statusClass = useMemo(() => {
    const s = status.trim();
    if (s === "active") return "success";
    if (s === "failed") return "danger";
    if (s === "activating" || s === "deactivating" || s === "reloading") return "warning";
    if (s === "inactive") return "muted";
    return "info";
  }, [status]);

  useEffect(() => {
    checkStatus();
    loadFlags();
    const interval = setInterval(() => {
      checkStatus();
    }, 3000);
    return () => {
      stopLogs();
      clearInterval(interval);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    // auto scroll to bottom on new lines
    const el = logsRef.current;
    if (!el) return;
    el.scrollTo({ top: el.scrollHeight, behavior: "smooth" });
  }, [logs]);

  async function checkStatus() {
    try {
      const s = await invoke<string>("service_status", { name: SERVICE });
      setStatus(s);
    } catch (e: any) {
    }
  }

  async function loadFlags() {
    try {
      setFlagsLoading(true);
      const f = await invoke<string>("read_startup_flags", { name: SERVICE });
      console.log(f)
      setFlags(f || "");
    } catch (e: any) {
    } finally {
      setFlagsLoading(false);
    }
  }

  const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));
  async function pollStatusUntilSettled(timeoutMs = 10000, everyMs = 600) {
    if (pollingRef.current) return;
    pollingRef.current = true;
    const start = Date.now();
    try {
      while (Date.now() - start < timeoutMs) {
        const s = await invoke<string>("service_status", { name: SERVICE });
        setStatus(s);
        const st = s.trim();
        if (st === "active" || st === "inactive" || st === "failed") break;
        await sleep(everyMs);
      }
    } finally {
      pollingRef.current = false;
    }
  }

  async function toggleStartStop() {
    try {
      setBusy(true);
      if (isActive) {
        await invoke("service_stop", { name: SERVICE });
      } else {
        await invoke("service_start", { name: SERVICE });
      }
      await checkStatus();
      // follow activation until it settles
      void pollStatusUntilSettled();
    } catch (e: any) {
    } finally {
      setBusy(false);
    }
  }

  async function startLogs() {
    if (streaming) return;
    try {
      setLogs([]);
      await invoke("start_journal_stream", { name: SERVICE });
      const unlisten = await listen("daemon://logs", (ev: any) => {
        const p = ev?.payload as { service?: string; line?: string };
        if (p?.service && p?.service !== `${SERVICE}.service`) return;
        if (typeof p?.line === "string") {
          setLogs((prev) => [...prev, p.line!]);
        }
      });
      unlistenRef.current = unlisten;
      setStreaming(true);
    } catch (e: any) {
    }
  }

  async function stopLogs() {
    if (!streaming) return;
    try {
      await invoke("stop_journal_stream");
    } catch {}
    if (unlistenRef.current) {
      try { unlistenRef.current(); } catch {}
      unlistenRef.current = null;
    }
    setStreaming(false);
  }

  async function toggleLogs() { streaming ? await stopLogs() : await startLogs(); }

  async function restartService() {
    try {
      setBusy(true);
      await invoke("service_restart", { name: SERVICE });
      await checkStatus();
      void pollStatusUntilSettled();
    } catch (e: any) {
    } finally {
      setBusy(false);
    }
  }

  async function saveFlags() {
    try {
      setFlagsSaving(true);
      await invoke("write_startup_flags", { name: SERVICE, flags });
    } catch (e: any) {
      console.log(e)
    } finally {
      setFlagsSaving(false);
    }
  }

  return (
    <section style={{ display: "flex", flexDirection: "column", minHeight: "70vh" }}>
      <div className="toolbar">
        <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
          <div className="muted">{SERVICE}</div>
          <span className={`badge ${statusClass}`}>{status}</span>
        </div>
        <div style={{ marginLeft: "auto", display: "flex", gap: 8 }}>
          <button className="btn primary" disabled={busy || statusClass === 'warning'} onClick={toggleStartStop}>
            {isActive ? "Stop" : "Start"}
          </button>
          <button className="btn" disabled={busy || statusClass === 'warning'} onClick={restartService}>Restart</button>
          <button className="btn" onClick={toggleLogs}>{streaming ? "Stop Logs" : "View Logs"}</button>
        </div>
      </div>

      <div className="card" style={{ marginBottom: 12 }}>

        <div className="field" style={{ marginBottom: 8 }}>
          <label>Startup flags</label>
          <textarea
            className="input"
            placeholder="e.g., --tcp-port=30012 --http-port=8012 --relay=3.127.69.180:20018"
            rows={3}
            value={flags}
            onChange={(e) => setFlags(e.currentTarget.value)}
            disabled={flagsLoading}
          />
        </div>
        <div className="row">
          <div className="muted">Changes require restarting the service.</div>
          <button className="btn primary" type="button" onClick={saveFlags} disabled={flagsSaving || flagsLoading}>
            {flagsSaving ? "Saving..." : "Save"}
          </button>
        </div>
      </div>

      <div className="card" style={{ flex: 1, display: "flex", flexDirection: "column", minHeight: 0 }}>
        <div className="muted" style={{ marginBottom: 8 }}>Logs</div>
        <pre className="logs" ref={logsRef}>
{logs.join("\n")}
        </pre>
      </div>
    </section>
  );
}
