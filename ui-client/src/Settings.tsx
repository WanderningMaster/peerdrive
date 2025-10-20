import React, { useEffect, useMemo, useState } from "react";
import { invoke } from "@tauri-apps/api/core";
import { bytesToHex } from "./util";

export default function SettingsPage() {
  const [nodeId, setNodeId] = useState<number[]>([]);
  const [tcpPort, setTcpPort] = useState<string>("");
  const [httpPort, setHttpPort] = useState<string>("");
  const [relay, setRelay] = useState("");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState<string>("");

  const nodeIdHex = useMemo(() => {
    return bytesToHex(nodeId);
  }, [nodeId]);

  useEffect(() => {
    let mounted = true;
    (async () => {
      try {
        setLoading(true);
        const cfg = await invoke<any>("read_user_config");
        if (!mounted) return;
        setNodeId(Array.isArray(cfg?.nodeId) ? cfg.nodeId : []);
        setTcpPort(cfg?.tcpPort ? String(cfg.tcpPort) : "");
        setHttpPort(cfg?.httpPort ? String(cfg.httpPort) : "");
        setRelay(cfg?.relay || "");
      } catch (e: any) {
        setMessage(String(e));
      } finally {
        setLoading(false);
      }
    })();
    return () => {
      mounted = false;
    };
  }, []);

  async function save() {
    setMessage("");
    setSaving(true);
    try {
      const cfg = {
        nodeId,
        tcpPort: tcpPort ? Number(tcpPort) : 0,
        httpPort: httpPort ? Number(httpPort) : 0,
        relay: relay || null,
      };
      await invoke("write_user_config", { cfg });
      setMessage("File Saved. Changes require restarting the service.");
    } catch (e: any) {
      setMessage(String(e));
    } finally {
      setSaving(false);
    }
  }

  return (
    <section>
      <div className="card">
        <form className="form">
          <div className="field">
            <label>Node ID</label>
            <input
              className="input"
              placeholder="e.g., 12D3KooW..."
              readOnly
              value={nodeIdHex.slice(0,12)}
              title="Generated automatically on first run"
            />
          </div>

          <div className="field">
            <label>TCP Port</label>
            <input
              className="input"
              type="number"
              min={1}
              max={65535}
              placeholder="e.g., 30012"
              value={tcpPort}
              onChange={(e) => setTcpPort(e.currentTarget.value)}
            />
          </div>

          <div className="field">
            <label>HTTP Port</label>
            <input
              className="input"
              type="number"
              min={1}
              max={65535}
              placeholder="e.g., 8012"
              value={httpPort}
              onChange={(e) => setHttpPort(e.currentTarget.value)}
            />
          </div>

          <div className="field">
            <label>Relay (optional)</label>
            <input
              className="input"
              placeholder="host:port (e.g., 3.127.69.180:20018)"
              value={relay}
              onChange={(e) => setRelay(e.currentTarget.value)}
            />
            <div className="muted">Leave empty to disable relay.</div>
          </div>

          <div className="row">
            <div className="muted">{loading ? "Loading..." : message}</div>
            <button className="btn primary" type="button" disabled={saving || loading} onClick={save}>
              {saving ? "Saving..." : "Save"}
            </button>
          </div>
        </form>
      </div>
    </section>
  );
}
