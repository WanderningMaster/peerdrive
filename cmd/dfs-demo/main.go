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

	time.Sleep(time.Second * 2)

	f1 := blockfetcher.New(n1)
	f2 := blockfetcher.New(n2)
	f3 := blockfetcher.New(n3)
	mem1 := storage.NewMemStore(storage.WithFetcher(f1), storage.WithSoftTTL(time.Second*1))
	mem2 := storage.NewMemStore(storage.WithFetcher(f2), storage.WithSoftTTL(time.Second*1))
	mem3 := storage.NewMemStore(storage.WithFetcher(f3), storage.WithSoftTTL(time.Second*1))
	n1.SetBlockProvider(mem1)
	n2.SetBlockProvider(mem2)
	n3.SetBlockProvider(mem3)

    // For the demo, keep only the manifest locally (direct-pinned)
    dist := service.NewDistStore(n1, mem1, n1.Replicas(), service.KeepLocalSelector(true, 0.0))
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
	time.Sleep(1 * time.Second)

    // Show per-node blockstore stats to demonstrate distribution
    b1, sz1, _ := mem1.Stats(ctx)
    b2, sz2, _ := mem2.Stats(ctx)
    b3, sz3, _ := mem3.Stats(ctx)
    fmt.Printf("n1 store: blocks=%d bytes=%d\n", b1, sz1)
    fmt.Printf("n2 store: blocks=%d bytes=%d\n", b2, sz2)
    fmt.Printf("n3 store: blocks=%d bytes=%d\n", b3, sz3)

    fmt.Println("payload=", len(payload))

    // Simulate reading the file on n1 which will cache blocks via soft pins
    if _, err := dag.FetchParallel(ctx, mem1, cid, 8); err != nil {
        log.Fatalf("fetch on n1 failed: %v", err)
    }
    b1, sz1, _ = mem1.Stats(ctx)
    fmt.Printf("after read: n1 store: blocks=%d bytes=%d\n", b1, sz1)

    // Wait beyond soft TTL so cached blocks become eligible for GC
    time.Sleep(time.Second * 2)
    freed, _ := mem1.GC(ctx)
    fmt.Println("n1 GC freed:", freed)
    b1, sz1, _ = mem1.Stats(ctx)
    fmt.Printf("after GC: n1 store: blocks=%d bytes=%d\n", b1, sz1)

    // Other nodes may also GC cached content
    freed, _ = mem2.GC(ctx)
    fmt.Println("n2 GC freed:", freed)
    freed, _ = mem3.GC(ctx)
    fmt.Println("n3 GC freed:", freed)

	// if err := dag.Verify(ctx, mem1, cid); err != nil {
	// 	log.Fatalf("verify failed: %v", err)
	// }
	// if err := dag.Verify(ctx, mem2, cid); err != nil {
	// 	log.Fatalf("verify failed: %v", err)
	// }
	// if err := dag.Verify(ctx, mem3, cid); err != nil {
	// 	log.Fatalf("verify failed: %v", err)
	// }

	// if err := mem1.Unpin(ctx, cid); err != nil {
	// 	log.Fatalf("unpin local: %v", err)
	// }

	// freed, _ = mem1.GC(ctx)
	// fmt.Println("freed after local GC:", freed)
	// freed, _ = mem2.GC(ctx)
	// fmt.Println("freed=", freed)
	// freed, _ = mem3.GC(ctx)
	// fmt.Println("freed=", freed)
}
