package cmd

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/WanderningMaster/peerdrive/configuration"
	"github.com/WanderningMaster/peerdrive/internal/block"
	blockfetcher "github.com/WanderningMaster/peerdrive/internal/block-fetcher"
	"github.com/WanderningMaster/peerdrive/internal/dag"
	"github.com/WanderningMaster/peerdrive/internal/node"
	"github.com/WanderningMaster/peerdrive/internal/storage"
	daemon "github.com/coreos/go-systemd/v22/daemon"
)

func startHTTP(n *node.Node, httpPort int) {
	fetcher := blockfetcher.New(n)
	blockstore := storage.NewMemStore(storage.WithFetcher(fetcher))
	n.SetBlockProvider(blockstore)

	builder := dag.DagBuilder{
		ChunkSize: 1 << 20,
		Fanout:    256,
		Codec:     "cbor",
		Store:     blockstore,
	}

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
			http.Error(w, err.Error(), 500)
			return
		}
		_, _ = io.WriteString(w, string(val)+"\n")
	})

	mux.HandleFunc("/dfs/{cid}", func(w http.ResponseWriter, r *http.Request) {
		cidStr := r.PathValue("cid")
		outPath := r.URL.Query().Get("out")
		if outPath == "" {
			http.Error(w, "out required", 400)
			return
		}

		cid, err := block.DecodeCID(cidStr)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		b, err := dag.Fetch(context.TODO(), blockstore, cid)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		err = os.WriteFile(outPath, b, 0644)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		_, _ = io.WriteString(w, "ok\n")
	})
	mux.HandleFunc("/dfs/put", func(w http.ResponseWriter, r *http.Request) {
		inPath := r.URL.Query().Get("in")
		if inPath == "" {
			http.Error(w, "in required", 400)
			return
		}

		inFile, err := os.Open(inPath)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		_, cid, err := builder.BuildFromReader(context.TODO(), "example.txt", inFile)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		cidStr, _ := cid.Encode()
		defer inFile.Close()

		_, _ = io.WriteString(w, fmt.Sprintf("%s\n", cidStr))
	})

	mux.HandleFunc("/bootstrap", func(w http.ResponseWriter, r *http.Request) {
		peersParam := r.URL.Query().Get("peers")
		if peersParam == "" {
			http.Error(w, "peers required", 400)
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
			http.Error(w, "no valid peers provided", 400)
			return
		}
		go n.Bootstrap(r.Context(), peers)
		_, _ = io.WriteString(w, "ok\n")
	})
	go func() { _ = http.ListenAndServe(fmt.Sprintf(":%d", httpPort), mux) }()
}

func BootstrapHttpClient(conf *configuration.UserConfig, boot *string) {
	tcpAddr := fmt.Sprintf("0.0.0.0:%d", conf.TcpPort)
	n := node.NewNodeWithId(tcpAddr, conf.NodeId)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := n.ListenAndServe(ctx); err != nil {
			log.Fatal(err)
		}
	}()

	startHTTP(n, conf.HttpPort)

	if *boot != "" {
		peers := strings.Split(*boot, ",")
		for i := range peers {
			peers[i] = strings.TrimSpace(peers[i])
		}
		n.Bootstrap(ctx, peers)
	}
	n.StartMaintenance(ctx)

	_, _ = daemon.SdNotify(false, daemon.SdNotifyReady)

	select {}
}
