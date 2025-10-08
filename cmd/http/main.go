package cmd

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/WanderningMaster/peerdrive/internal/node"
)

func startHTTP(n *node.Node, httpPort string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/id", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, fmt.Sprintf("%s\n", n.ID.String()))
	})
	mux.HandleFunc("/put", func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Query().Get("key")
		val := r.URL.Query().Get("value")
		if key == "" {
			http.Error(w, "key required", 400)
			return
		}
		err := n.Store(r.Context(), key, []byte(val))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		_, _ = io.WriteString(w, "ok\n")
	})
	mux.HandleFunc("/get", func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Query().Get("key")
		if key == "" {
			http.Error(w, "key required", 400)
			return
		}
		val, err := n.Get(r.Context(), key)
		if err != nil {
			http.Error(w, err.Error(), 404)
			return
		}
		_, _ = io.WriteString(w, string(val)+"\n")
	})
	go func() { _ = http.ListenAndServe(httpPort, mux) }()
}

func BootstrapHttpClient(addr *string, boot *string) {
	n := node.NewNode(*addr)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start server
	go func() {
		if err := n.ListenAndServe(ctx); err != nil {
			log.Fatal(err)
		}
	}()

	// Simple HTTP helper on 808x: port = 808 + last digit(s) of tcp port
	httpPort := ":8081"
	if p := strings.Split(*addr, ":"); len(p) == 2 {
		if len(p[1]) >= 2 {
			httpPort = ":8" + p[1][1:]
		}
	}
	startHTTP(n, httpPort)

	// Bootstrap if provided
	if *boot != "" {
		peers := strings.Split(*boot, ",")
		for i := range peers {
			peers[i] = strings.TrimSpace(peers[i])
		}
		n.Bootstrap(ctx, peers)
	}

	// Keep alive
	select {}
}
