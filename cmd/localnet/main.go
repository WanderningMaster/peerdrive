package main

import (
    "context"
    "fmt"
    "log"
    mrand "math/rand"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/WanderningMaster/peerdrive/internal/node"
    "github.com/WanderningMaster/peerdrive/internal/storage"
)

type simNode struct {
	addr   string
	n      *node.Node
	ctx    context.Context
	cancel context.CancelFunc
	alive  bool
}

func main() {
	const numNodes = 50
	const basePort = 9200
	const warmup = 750 * time.Millisecond

	mrand.Seed(time.Now().UnixNano())

	nodes := make([]*simNode, numNodes)
	addrs := make([]string, numNodes)
	for i := range numNodes {
		addrs[i] = fmt.Sprintf("127.0.0.1:%d", basePort+i)
		startNode(nodes, i, addrs[i])
	}

	time.Sleep(warmup)

	for i := 1; i < numNodes; i++ {
		prev := addrs[:i]
		m := min(i, 5)
		perm := mrand.Perm(i)
		peers := make([]string, 0, m)
		for j := range m {
			peers = append(peers, prev[perm[j]])
		}
		go nodes[i].n.Bootstrap(nodes[i].ctx, peers)
	}

	bs := addrs[mrand.Intn(len(addrs))]
	fmt.Println(bs)

	log.Printf("Started %d nodes on 127.0.0.1:%d-%d; bootstrap: %s", numNodes, basePort, basePort+numNodes-1, bs)

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	<-ch
	log.Printf("Interrupt received, shutting down...")

	for _, sn := range nodes {
		if sn != nil && sn.alive {
			sn.cancel()
			sn.alive = false
		}
	}
}

func startNode(nodes []*simNode, i int, addr string) {
    ctx, cancel := context.WithCancel(context.Background())
    n := node.NewNode(addr)
    // Attach a simple in-memory block provider to serve FetchBlock RPCs
    mem := storage.NewMemStore()
    n.SetBlockProvider(mem)
    sn := nodes[i]
    if sn == nil {
        sn = &simNode{}
        nodes[i] = sn
    }
	sn.addr = addr
	sn.n = n
	sn.ctx = ctx
	sn.cancel = cancel
	sn.alive = true
	go func() {
		if err := n.ListenAndServe(ctx); err != nil {
			log.Printf("node %s stopped: %v", n.ID.String()[:8], err)
		}
	}()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
