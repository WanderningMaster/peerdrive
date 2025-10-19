package api

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/WanderningMaster/peerdrive/configuration"
	"github.com/WanderningMaster/peerdrive/internal/block"
	"github.com/WanderningMaster/peerdrive/internal/id"
	"github.com/WanderningMaster/peerdrive/internal/node"
	"github.com/WanderningMaster/peerdrive/internal/service"
	daemon "github.com/coreos/go-systemd/v22/daemon"
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
		outPath := r.URL.Query().Get("out")
		if outPath == "" {
			writeErr(w, 400, "out required")
			return
		}
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
		if err := os.WriteFile(outPath, b, 0644); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, map[string]any{"ok": true, "out": outPath, "bytes": len(b)})
	})

	mux.HandleFunc("/dfs/put", func(w http.ResponseWriter, r *http.Request) {
		inPath := r.URL.Query().Get("in")
		if inPath == "" {
			writeErr(w, 400, "in required")
			return
		}
		cidStr, err := svc.AddFromPath(r.Context(), inPath)
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
		out := make([]string, 0, len(pins))
		for _, c := range pins {
			if s, err := c.Encode(); err == nil {
				out = append(out, s)
			}
		}
		writeJSON(w, out)
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

	_, _ = daemon.SdNotify(false, daemon.SdNotifyReady)
	select {}
}
