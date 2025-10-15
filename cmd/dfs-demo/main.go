package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"time"

	"github.com/WanderningMaster/peerdrive/internal/block"
	"github.com/WanderningMaster/peerdrive/internal/dag"
	"github.com/WanderningMaster/peerdrive/internal/node"
	"github.com/WanderningMaster/peerdrive/internal/storage"
)

type Fetcher struct {
	node *node.Node
}

func (f *Fetcher) FetchBlock(ctx context.Context, cid block.CID) ([]byte, error) {
	pr, err := f.node.GetProviderRecord(ctx, cid)
	if err != nil {
		return nil, err
	}
	return f.node.FetchBlock(ctx, string(pr.Addr), cid)
}

func (f *Fetcher) Announce(ctx context.Context, cid block.CID) error {
	return f.node.PutProviderRecord(ctx, cid)
}

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

	f1 := &Fetcher{node: n1}
	f2 := &Fetcher{node: n2}
	f3 := &Fetcher{node: n3}
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

	if err := dag.Verify(ctx, mem3, cid); err != nil {
		log.Fatalf("verify failed: %v", err)
	}
	fmt.Println("Verify OK: node3 retrieved and verified all blocks")
}
