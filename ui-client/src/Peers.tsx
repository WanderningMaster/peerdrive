import React, { useEffect, useState } from "react";
import { invoke } from "@tauri-apps/api/core";
import { bytesToHex } from "./util";

type Contact = { id: string; addr: string; relay?: string };

export default function PeersPage() {
  const [contacts, setContacts] = useState<Contact[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string>("");

  async function load() {
    try {
      setLoading(true);
      setError("");
      const res = await invoke<any[]>("list_closest", { target: null, k: null });
      setContacts(Array.isArray(res) ? res.map((x) => ({
        id: bytesToHex(x.id),
        addr: x.addr,
        relay: x.relay
      })) : []);
    } catch (e: any) {
      setError(String(e));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
  }, []);

  return (
    <section>
      <div className="toolbar">
        <button className="btn" onClick={load} disabled={loading}>
          {loading ? "Refreshing..." : "Refresh"}
        </button>
      </div>

      {error && <div className="card danger" style={{ marginBottom: 12 }}>{error}</div>}

      <div className="card">
        {contacts.length === 0 ? (
          <div className="muted">No peers.</div>
        ) : (
          <table className="table">
            <thead>
              <tr>
                <th>Node ID</th>
                <th>Address</th>
                <th>Relay</th>
              </tr>
            </thead>
            <tbody>
              {contacts.map((c, i) => (
                <tr key={i}>
                  <td><code className="muted" style={{ fontSize: 12 }}>{c.id.slice(0, 12)}...</code></td>
                  <td><strong>{c.addr}</strong></td>
                  <td className="muted">{c.relay || ''}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </section>
  );
}
