package api

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/WanderningMaster/peerdrive/configuration"
	"github.com/WanderningMaster/peerdrive/internal/block"
	"github.com/WanderningMaster/peerdrive/internal/id"
	"github.com/WanderningMaster/peerdrive/internal/node"
	"github.com/WanderningMaster/peerdrive/internal/service"
)

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": msg})
}

// NewMux builds the HTTP mux from the provided service.
func NewMux(svc *service.Service) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/id", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"id":    svc.ID(),
			"addr":  svc.Addr(),
			"relay": svc.Relay(),
		})
	})

	mux.HandleFunc("/put", func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Query().Get("key")
		val := r.URL.Query().Get("value")
		if key == "" {
			writeErr(w, 400, "key required")
			return
		}
		if err := svc.Put(r.Context(), key, []byte(val)); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})

	mux.HandleFunc("/get", func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Query().Get("key")
		if key == "" {
			writeErr(w, 400, "key required")
			return
		}
		val, err := svc.Get(r.Context(), key)
		if err != nil {
			writeErr(w, 404, err.Error())
			return
		}
		writeJSON(w, map[string]any{"found": true, "value": string(val)})
	})

	mux.HandleFunc("/dfs/{cid}", func(w http.ResponseWriter, r *http.Request) {
		cidStr := r.PathValue("cid")
		cid, err := block.DecodeCID(cidStr)
		if err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		b, err := svc.Fetch(r.Context(), cid)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		// Always return raw content for preview/download with appropriate headers
		name, mime, err := svc.ManifestMeta(r.Context(), cid)
		if err != nil {
			// Not a manifest or decode error; continue with best-effort content type
			name, mime = "", ""
		}

		if mime == "" {
			if len(b) > 0 {
				if len(b) > 512 {
					w.Header().Set("Content-Type", http.DetectContentType(b[:512]))
				} else {
					w.Header().Set("Content-Type", http.DetectContentType(b))
				}
			} else {
				w.Header().Set("Content-Type", "application/octet-stream")
			}
		} else {
			w.Header().Set("Content-Type", mime)
		}
		if name != "" {
			w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", name))
			w.Header().Set("X-File-Name", name)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(b)
	})

	mux.HandleFunc("/dfs/put", func(w http.ResponseWriter, r *http.Request) {
		inPath := r.URL.Query().Get("in")
		if inPath == "" {
			writeErr(w, 400, "in required")
			return
		}
		compress := false
		if v := strings.TrimSpace(r.URL.Query().Get("compress")); v != "" {
			if v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes") || strings.EqualFold(v, "on") {
				compress = true
			}
		}
		var cidStr string
		var err error
		if compress {
			cidStr, err = svc.AddFromPathDistributed(r.Context(), inPath)
		} else {
			cidStr, err = svc.AddFromPath(r.Context(), inPath)
		}
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, map[string]any{"cid": cidStr})
	})

	mux.HandleFunc("/pin", func(w http.ResponseWriter, r *http.Request) {
		cidStr := strings.TrimSpace(r.URL.Query().Get("cid"))
		if cidStr == "" {
			writeErr(w, 400, "cid required")
			return
		}
		cid, err := block.DecodeCID(cidStr)
		if err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		if err := svc.Pin(r.Context(), cid); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})

	mux.HandleFunc("/unpin", func(w http.ResponseWriter, r *http.Request) {
		cidStr := strings.TrimSpace(r.URL.Query().Get("cid"))
		if cidStr == "" {
			writeErr(w, 400, "cid required")
			return
		}
		cid, err := block.DecodeCID(cidStr)
		if err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		if err := svc.Unpin(r.Context(), cid); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})

	mux.HandleFunc("/pins", func(w http.ResponseWriter, r *http.Request) {
		pins, err := svc.ListPins(r.Context())
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		// Directly return the enriched pin info: {cid, name, mime}
		writeJSON(w, pins)
	})

	mux.HandleFunc("/closest", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		var target id.NodeID
		targStr := strings.TrimSpace(q.Get("target"))
		if targStr == "" {
			target = svc.Node().ID
		} else {
			if b, err := hex.DecodeString(targStr); err == nil && len(b) == len(target) {
				copy(target[:], b)
			} else {
				writeErr(w, 400, "bad target id")
				return
			}
		}
		k := svc.Node().KBucketK()
		if ks := strings.TrimSpace(q.Get("k")); ks != "" {
			if v, err := strconv.Atoi(ks); err == nil && v > 0 {
				k = v
			}
		}
		writeJSON(w, svc.Closest(target, k))
	})

	mux.HandleFunc("/bootstrap", func(w http.ResponseWriter, r *http.Request) {
		peersParam := r.URL.Query().Get("peers")
		if peersParam == "" {
			writeErr(w, 400, "peers required")
			return
		}
		parts := strings.Split(peersParam, ",")
		peers := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				peers = append(peers, p)
			}
		}
		if len(peers) == 0 {
			writeErr(w, 400, "no valid peers provided")
			return
		}
		go svc.Bootstrap(r.Context(), peers)
		writeJSON(w, map[string]any{"ok": true, "peers": peers})
	})

	// Manually trigger blockstore garbage collection
	mux.HandleFunc("/gc", func(w http.ResponseWriter, r *http.Request) {
		// Mutating action; prefer POST
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed; use POST")
			return
		}
		freed, err := svc.GC(r.Context())
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, map[string]any{"freed": freed})
	})

	return mux
}

func BootstrapHttpClient(conf *configuration.UserConfig, boot *string, relayAddr *string, mem *bool) {
	tcpAddr := fmt.Sprintf("0.0.0.0:%d", conf.TcpPort)
	n := node.NewNodeWithId(tcpAddr, conf.NodeId)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	useMemStore := false
	if mem != nil {
		useMemStore = *mem
	}
	svc := service.New(n, conf, useMemStore)

	raddr := ""
	if relayAddr != nil && *relayAddr != "" {
		raddr = *relayAddr
	}
	var peers []string
	if boot != nil && *boot != "" {
		parts := strings.Split(*boot, ",")
		peers = make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				peers = append(peers, p)
			}
		}
	}

	svc.Start(ctx, raddr, peers)

	mux := NewMux(svc)
	go func() { _ = http.ListenAndServe(fmt.Sprintf(":%d", conf.HttpPort), mux) }()

	select {}
}
