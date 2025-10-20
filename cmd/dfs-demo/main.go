package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"time"

	blockfetcher "github.com/WanderningMaster/peerdrive/internal/block-fetcher"
	"github.com/WanderningMaster/peerdrive/internal/dag"
	"github.com/WanderningMaster/peerdrive/internal/node"
	"github.com/WanderningMaster/peerdrive/internal/service"
	"github.com/WanderningMaster/peerdrive/internal/storage"
)

func startNode(ctx context.Context, addr string) *node.Node {
	n := node.NewNode(addr)
	go func() {
		if err := n.ListenAndServe(ctx); err != nil {
			log.Printf("node %s stopped: %v", n.ID.String()[:8], err)
		}
	}()
	return n
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addr1 := "127.0.0.1:9011"
	addr2 := "127.0.0.1:9012"
	addr3 := "127.0.0.1:9013"

	n1 := startNode(ctx, addr1)

	n2 := startNode(ctx, addr2)
	n3 := startNode(ctx, addr3)

	time.Sleep(750 * time.Millisecond)

	n2.Bootstrap(ctx, []string{addr1})
	n3.Bootstrap(ctx, []string{addr2})
	n1.Bootstrap(ctx, []string{addr2, addr3})
	n3.Bootstrap(ctx, []string{addr1})

	f1 := blockfetcher.New(n1)
	f2 := blockfetcher.New(n2)
	f3 := blockfetcher.New(n3)
	mem1 := storage.NewMemStore(storage.WithFetcher(f1))
	mem2 := storage.NewMemStore(storage.WithFetcher(f2))
	mem3 := storage.NewMemStore(storage.WithFetcher(f3))
	n1.SetBlockProvider(mem1)
	n2.SetBlockProvider(mem2)
	n3.SetBlockProvider(mem3)

	dist := service.NewDistStore(n1, mem1, n1.Replicas(), service.KeepLocalSelector(true, 0.20))
	builder := dag.DagBuilder{ChunkSize: 1 << 16, Fanout: 256, Codec: "cbor", Store: dist}

	payload := make([]byte, 512*1024)
	if _, err := rand.Read(payload); err != nil {
		log.Fatalf("rand: %v", err)
	}

	_, cid, err := builder.BuildFromReader(ctx, "random.bin", "application/octet-stream", bytes.NewReader(payload))
	if err != nil {
		log.Fatalf("build: %v", err)
	}
	cidStr, _ := cid.Encode()
	fmt.Printf("Manifest CID: %s\n", cidStr)

	// Allow a brief moment for provider records to be stored
	time.Sleep(500 * time.Millisecond)

	freed, _ := mem1.GC(ctx)
	fmt.Println("freed=", freed)
	freed, _ = mem2.GC(ctx)
	fmt.Println("freed=", freed)
	freed, _ = mem3.GC(ctx)
	fmt.Println("freed=", freed)
	if err := dag.Verify(ctx, mem1, cid); err != nil {
		log.Fatalf("verify failed: %v", err)
	}
	if err := dag.Verify(ctx, mem2, cid); err != nil {
		log.Fatalf("verify failed: %v", err)
	}
	if err := dag.Verify(ctx, mem3, cid); err != nil {
		log.Fatalf("verify failed: %v", err)
	}

}
