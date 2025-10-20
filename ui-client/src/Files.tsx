import React, { useEffect, useState } from "react";
import { invoke } from "@tauri-apps/api/core";
import { open } from "@tauri-apps/plugin-dialog";

type PinInfo = { cid: string; name?: string; size?: number; mime?: string };

export default function FilesPage() {
  const [pins, setPins] = useState<PinInfo[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string>("");
  const [httpPort, setHttpPort] = useState<number>(0);
  const [previewCid, setPreviewCid] = useState<string | null>(null);
  const [previewName, setPreviewName] = useState<string>("");
  const [previewMime, setPreviewMime] = useState<string>("");
  const [uploading, setUploading] = useState(false);
  const [cidInput, setCidInput] = useState("");
  const [pinning, setPinning] = useState(false);

  function formatBytes(n?: number): string {
    if (typeof n !== "number" || !isFinite(n) || n < 0) return "—";
    const KB = 1024;
    const MB = 1024 * 1024;
    if (n >= MB) {
      const v = n / MB;
      const dec = v >= 10 ? 0 : 1;
      return `${v.toFixed(dec)} MB`;
    }
    if (n >= KB) {
      const v = n / KB;
      const dec = v >= 10 ? 0 : 1;
      return `${v.toFixed(dec)} KB`;
    }
    return `${Math.floor(n)} B`;
  }

  async function loadPins() {
    try {
      setLoading(true);
      setError("");
      const res = await invoke<any>("list_pins");
      const arr: PinInfo[] = Array.isArray(res) ? res.map((p: any) => ({
        cid: String(p?.cid || ""),
        name: p?.name ? String(p.name) : undefined,
        mime: p?.mime ? String(p.mime) : undefined,
        size: p?.size ? Number(p.size) : undefined
      })) : [];
      setPins(arr);
    } catch (e: any) {
      setError(String(e));
    } finally {
      setLoading(false);
    }
  }

  async function loadConfig() {
    try {
      const cfg = await invoke<any>("read_user_config");
      const p = Number(cfg?.httpPort || 0);
      setHttpPort(Number.isFinite(p) ? p : 0);
    } catch (e) {
      // ignore, FilesPage already gated on service active
    }
  }

  useEffect(() => { void loadConfig(); void loadPins(); }, []);

  function openPreview(p: PinInfo) {
    setPreviewCid(p.cid);
    setPreviewName(p.name || p.cid);
    setPreviewMime(p.mime || "");
  }

  function openPreviewFromCid() {
    const cid = cidInput.trim();
    if (!cid) return;
    setError("");
    setPreviewCid(cid);
    setPreviewName(cid);
    setPreviewMime("");
  }

  async function onUpload() {
    try {
      setError("");
      setUploading(true);
      const selection = await open({ multiple: false, directory: false });
      if (!selection) return; // cancelled
      const path = Array.isArray(selection) ? selection[0] : selection;
      if (typeof path !== "string" || path.trim() === "") return;
      await invoke<string>("add_and_pin_file", { path });
      await loadPins();
    } catch (e: any) {
      setError(String(e));
    } finally {
      setUploading(false);
    }
  }
  function closePreview() {
    setPreviewCid(null);
    setPreviewName("");
    setPreviewMime("");
  }

  async function pinPreviewCid() {
    const cid = (previewCid || "").trim();
    if (!cid) return;
    try {
      setPinning(true);
      setError("");
      await invoke("pin_by_cid", { cid });
      await loadPins();
    } catch (e: any) {
      setError(String(e));
    } finally {
      setPinning(false);
    }
  }

  return (
    <section>
      <div className="toolbar">
        <button className="btn" onClick={loadPins} disabled={loading}>
          {loading ? "Refreshing..." : "Refresh"}
        </button>
        <button className="btn primary" onClick={onUpload} disabled={uploading}>
          {uploading ? "Uploading..." : "Upload"}
        </button>
        <div style={{ display: "inline-flex", gap: 8, alignItems: "center", marginLeft: 12 }}>
          <input
            className="input"
            placeholder="Enter CID..."
            value={cidInput}
            onChange={(e) => setCidInput(e.currentTarget.value)}
            style={{ width: 320 }}
          />
          <button className="btn" onClick={openPreviewFromCid} disabled={!cidInput.trim()}>
            Preview CID
          </button>
        </div>
      </div>

      <div className="card">
        {error && <div className="card danger" style={{ marginBottom: 12 }}>{error}</div>}

        {pins.length === 0 ? (
          <div className="muted">No pins.</div>
        ) : (
          <table className="table">
            <thead>
              <tr>
                <th>Name</th>
                <th>Size</th>
                <th>Type</th>
                <th>CID</th>
              </tr>
            </thead>
            <tbody>
              {pins.map((p, i) => (
                <tr key={i}>
                  <td>
                    <button className="link" onClick={() => openPreview(p)} title="Preview">
                      <strong>{p.name || p.cid}</strong>
                    </button>
                  </td>
                  <td className="muted">{formatBytes(p.size)}</td>
                  <td className="muted">{p.mime || ""}</td>
                  <td className="muted"><code style={{ fontSize: 12 }}>{p.cid}</code></td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {previewCid && (
        <div style={{ position: "fixed", inset: 0, background: "#0009", display: "flex", alignItems: "center", justifyContent: "center", zIndex: 1000 }}>
          <div className="card" style={{ width: "90vw", height: "85vh", display: "flex", flexDirection: "column" }}>
            <div className="row" style={{ marginBottom: 8 }}>
              <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
                <strong>{previewName}</strong>
                <span className="muted" style={{ fontSize: 12 }}>{previewCid}</span>
              </div>
              <div style={{ marginLeft: "auto", display: "flex", gap: 8 }}>
                <button className="btn" onClick={pinPreviewCid} disabled={pinning || !previewCid} title="Pin this file">
                  {pinning ? "Pinning..." : "Pin"}
                </button>
                <button className="btn" onClick={closePreview}>Close</button>
              </div>
            </div>
            <div style={{ flex: 1, minHeight: 0 }}>
              {httpPort ? (
                (() => {
                  const url = `http://127.0.0.1:${httpPort}/dfs/${previewCid}`;
                  const mime = (previewMime || "").toLowerCase();
                  const isImage = mime.startsWith("image/");
                  const isVideo = mime.startsWith("video/");
                  const isAudio = mime.startsWith("audio/");

                  if (isImage) {
                    return (
                      <div style={{ width: "100%", height: "100%", display: "flex", alignItems: "center", justifyContent: "center", background: "#111" }}>
                        <img src={url} alt={previewName} style={{ maxWidth: "100%", maxHeight: "100%", objectFit: "contain", display: "block" }} />
                      </div>
                    );
                  }
                  if (isVideo) {
                    return (
                      <div style={{ width: "100%", height: "100%", display: "flex", alignItems: "center", justifyContent: "center", background: "#000" }}>
                        <video src={url} controls style={{ width: "100%", height: "100%", objectFit: "contain", background: "#000" }} />
                      </div>
                    );
                  }
                  if (isAudio) {
                    return (
                      <div style={{ width: "100%", height: "100%", display: "flex", alignItems: "center", justifyContent: "center" }}>
                        <audio src={url} controls style={{ width: "90%" }} />
                      </div>
                    );
                  }

                  // Default to iframe for text and other types
                  return (
                    <iframe title="preview" src={url} style={{ border: 0, width: "100%", height: "100%", background: "white" }} />
                  );
                })()
              ) : (
                <div className="muted">HTTP port not configured.</div>
              )}
            </div>
          </div>
        </div>
      )}
    </section>
  );
}
