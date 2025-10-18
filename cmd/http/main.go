package cmd

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/WanderningMaster/peerdrive/configuration"
	"github.com/WanderningMaster/peerdrive/internal/block"
	blockfetcher "github.com/WanderningMaster/peerdrive/internal/block-fetcher"
	"github.com/WanderningMaster/peerdrive/internal/dag"
	"github.com/WanderningMaster/peerdrive/internal/node"
	"github.com/WanderningMaster/peerdrive/internal/storage"
	"github.com/WanderningMaster/peerdrive/internal/util"
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
		ctx := util.WithLogPrefix(r.Context(), "client")

		key := r.URL.Query().Get("key")
		val := r.URL.Query().Get("value")
		if key == "" {
			http.Error(w, "key required", 400)
			return
		}
		err := n.Store(ctx, key, []byte(val))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		_, _ = io.WriteString(w, "ok\n")
	})
	mux.HandleFunc("/get", func(w http.ResponseWriter, r *http.Request) {
		ctx := util.WithLogPrefix(r.Context(), "client")

		key := r.URL.Query().Get("key")
		if key == "" {
			http.Error(w, "key required", 400)
			return
		}
		val, err := n.Get(ctx, key)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		_, _ = io.WriteString(w, string(val)+"\n")
	})

	mux.HandleFunc("/dfs/{cid}", func(w http.ResponseWriter, r *http.Request) {
		ctx := util.WithLogPrefix(r.Context(), "client")

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

		b, err := dag.Fetch(ctx, blockstore, cid)
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
		ctx := util.WithLogPrefix(r.Context(), "client")

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
		_, cid, err := builder.BuildFromReader(ctx, "example.txt", inFile)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		cidStr, _ := cid.Encode()
		defer inFile.Close()

		_, _ = io.WriteString(w, fmt.Sprintf("%s\n", cidStr))
	})

	mux.HandleFunc("/bootstrap", func(w http.ResponseWriter, r *http.Request) {
		ctx := util.WithLogPrefix(r.Context(), "client")

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
		go n.Bootstrap(ctx, peers)
		_, _ = io.WriteString(w, "ok\n")
	})
	go func() { _ = http.ListenAndServe(fmt.Sprintf(":%d", httpPort), mux) }()
}

func BootstrapHttpClient(conf *configuration.UserConfig, boot *string, relayAddr *string) {
	tcpAddr := fmt.Sprintf("0.0.0.0:%d", conf.TcpPort)
	n := node.NewNodeWithId(tcpAddr, conf.NodeId)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		ctxServer := util.WithLogPrefix(ctx, "server")
		if err := n.ListenAndServe(ctxServer); err != nil {
			log.Fatal(err)
		}
	}()

	m, _ := n.WhoAmI(ctx, "3.127.69.180:20018")
	n.SetAdvertisedAddr(net.JoinHostPort(string(m.Value), strconv.Itoa(conf.TcpPort)))
	startHTTP(n, conf.HttpPort)

	// Attach to relay if provided by flag or config
	raddr := ""
	if relayAddr != nil && *relayAddr != "" {
		raddr = *relayAddr
	}
	if raddr == "" && conf.Relay != "" {
		raddr = conf.Relay
	}

	if raddr != "" {
		go func() {
			ctxRelay := util.WithLogPrefix(ctx, "relay")
			if err := n.AttachRelay(ctxRelay, raddr); err != nil {
				log.Printf("failed to attach relay: %v", err)
			}
		}()
	}
	time.Sleep(time.Second * 3)

	if *boot != "" {
		peers := strings.Split(*boot, ",")
		for i := range peers {
			peers[i] = strings.TrimSpace(peers[i])
		}
		ctxClient := util.WithLogPrefix(ctx, "client")
		n.Bootstrap(ctxClient, peers)
	}
	n.StartMaintenance(ctx)

	_, _ = daemon.SdNotify(false, daemon.SdNotifyReady)

	select {}
}
