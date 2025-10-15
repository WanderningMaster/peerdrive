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

	time.Sleep(250 * time.Millisecond)

	// - Node n2 knows about Node n1
	// - Node n3 knows only about Node n2
	n2.Bootstrap(ctx, []string{addr1})
	n3.Bootstrap(ctx, []string{addr2})

	f1 := blockfetcher.New(n1)
	f2 := blockfetcher.New(n2)
	f3 := blockfetcher.New(n3)
	mem1 := storage.NewMemStore(storage.WithFetcher(f1))
	mem2 := storage.NewMemStore(storage.WithFetcher(f2))
	mem3 := storage.NewMemStore(storage.WithFetcher(f3))
	n1.SetBlockProvider(mem1)
	n2.SetBlockProvider(mem2)
	n3.SetBlockProvider(mem3)

	builder := dag.DagBuilder{ChunkSize: 1 << 16, Fanout: 256, Codec: "cbor", Store: mem1}

	payload := make([]byte, 512*1024)
	if _, err := rand.Read(payload); err != nil {
		log.Fatalf("rand: %v", err)
	}

	_, cid, err := builder.BuildFromReader(ctx, "random.bin", bytes.NewReader(payload))
	if err != nil {
		log.Fatalf("build: %v", err)
	}
	cidStr, _ := cid.Encode()
	fmt.Printf("Manifest CID: %s\n", cidStr)

	time.Sleep(200 * time.Millisecond)

	// Pin the manifest so GC preserves the full DAG, then run GC.
	if err := mem1.Pin(ctx, cid); err != nil {
		log.Fatalf("pin: %v", err)
	}
	freed, err := mem1.GC(ctx)
	if err != nil {
		log.Fatalf("gc (pinned): %v", err)
	}
	fmt.Printf("GC (pinned) freed: %d blocks\n", freed)

	if err := dag.Verify(ctx, mem3, cid); err != nil {
		log.Fatalf("verify failed: %v", err)
	}
	fmt.Println("Verify OK: node3 retrieved and verified all blocks")

	// Demonstrate GC freeing data after unpinning the manifest.
	if err := mem1.Unpin(ctx, cid); err != nil {
		log.Fatalf("unpin: %v", err)
	}
	freed, err = mem1.GC(ctx)
	if err != nil {
		log.Fatalf("gc (after unpin): %v", err)
	}
	fmt.Printf("GC (after unpin) freed: %d blocks\n", freed)

	time.Sleep(time.Second * 1)

	if err := dag.Verify(ctx, mem3, cid); err == nil {
		fmt.Println("Verify OK(after unpin+GC): node3 retrieved and verified all blocks")
	}
	if err := dag.Verify(ctx, mem2, cid); err != nil {
		fmt.Println("Verify Failed (after unpin+GC): node2")
		fmt.Println("node2 never fetched so block not cached")
	}
}
