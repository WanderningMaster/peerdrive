package main

import (
	"context"
	crand "crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	mrand "math/rand"
	"os"
	"os/signal"
	"slices"
	"sync"
	"syscall"
	"time"

	"github.com/WanderningMaster/peerdrive/internal/node"
)

type simNode struct {
	addr   string
	n      *node.Node
	ctx    context.Context
	cancel context.CancelFunc
	alive  bool
}

type metrics struct {
	mu           sync.Mutex
	putOK        int
	putErr       int
	getOK        int
	getErr       int
	getMismatch  int
	putLatencies []time.Duration
	getLatencies []time.Duration
}

func (m *metrics) addPut(lat time.Duration, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err != nil {
		m.putErr++
		return
	}
	m.putOK++
	m.putLatencies = append(m.putLatencies, lat)
}

func (m *metrics) addGet(lat time.Duration, err error, mismatch bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err != nil {
		m.getErr++
		return
	}
	if mismatch {
		m.getMismatch++
	} else {
		m.getOK++
	}
	m.getLatencies = append(m.getLatencies, lat)
}

func pct(durs []time.Duration, p float64) time.Duration {
	if len(durs) == 0 {
		return 0
	}
	cp := append([]time.Duration(nil), durs...)
	slices.Sort(cp)
	idx := max(int(float64(len(cp)-1)*p), 0)
	if idx >= len(cp) {
		idx = len(cp) - 1
	}
	return cp[idx]
}

func avg(durs []time.Duration) time.Duration {
	if len(durs) == 0 {
		return 0
	}
	var sum time.Duration
	for _, d := range durs {
		sum += d
	}
	return time.Duration(int64(sum) / int64(len(durs)))
}

func main() {
	const numNodes = 50
	const basePort = 9200
	const warmup = 750 * time.Millisecond
	const duration = 20 * time.Second
	const workers = 24
	const churnEvery = 4 * time.Second
	const churnBatch = 4

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

	log.Printf("Bootstrapped %d nodes on 127.0.0.1:%d-%d", numNodes, basePort, basePort+numNodes-1)

	var kvsMu sync.RWMutex
	kvs := make(map[string][]byte)

	var mx metrics

	stopChurn := make(chan struct{})
	go func() {
		ticker := time.NewTicker(churnEvery)
		defer ticker.Stop()
		for {
			select {
			case <-stopChurn:
				return
			case <-ticker.C:
				victims := randSampleAlive(nodes, churnBatch)
				for _, i := range victims {
					if nodes[i].alive {
						nodes[i].cancel()
						nodes[i].alive = false
						log.Printf("churn: stopped node at %s", nodes[i].addr)
					}
				}
				time.Sleep(500 * time.Millisecond)
				for _, i := range victims {
					startNode(nodes, i, nodes[i].addr)
					peers := randSampleAddrs(addrs, 5)
					go nodes[i].n.Bootstrap(nodes[i].ctx, peers)
					log.Printf("churn: restarted node at %s", nodes[i].addr)
				}
			}
		}
	}()

	end := time.Now().Add(duration)
	var wg sync.WaitGroup
	for w := range workers {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for time.Now().Before(end) {
				// 40% puts, 60% gets
				if mrand.Intn(100) < 40 {
					// PUT
					writer := pickAlive(nodes)
					if writer < 0 {
						time.Sleep(50 * time.Millisecond)
						continue
					}
					key, val := randomKey(), randomValue(16, 64)
					t0 := time.Now()
					err := nodes[writer].n.Store(nodes[writer].ctx, key, val)
					mx.addPut(time.Since(t0), err)
					if err == nil {
						kvsMu.Lock()
						kvs[key] = append([]byte(nil), val...)
						kvsMu.Unlock()
					}
				} else {
					kvsMu.RLock()
					var key string
					for k := range kvs {
						key = k
						break
					}
					kvsMu.RUnlock()
					if key == "" {
						time.Sleep(10 * time.Millisecond)
						continue
					}
					reader := pickAlive(nodes)
					if reader < 0 {
						time.Sleep(20 * time.Millisecond)
						continue
					}
					kvsMu.RLock()
					want := kvs[key]
					kvsMu.RUnlock()
					t0 := time.Now()
					got, err := nodes[reader].n.Get(nodes[reader].ctx, key)
					lat := time.Since(t0)
					mismatch := err == nil && (len(got) != len(want) || (len(got) > 0 && string(got) != string(want)))
					mx.addGet(lat, err, mismatch)
				}
			}
		}(w)
	}

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	select {
	case <-ch:
		log.Printf("Interrupt received, stopping...")
	case <-time.After(duration):
	}

	close(stopChurn)
	wg.Wait()

	for _, sn := range nodes {
		if sn.alive {
			sn.cancel()
		}
	}

	log.Printf("--- Simulation Summary ---")
	log.Printf("Puts: ok=%d err=%d", mx.putOK, mx.putErr)
	log.Printf("Gets: ok=%d err=%d mismatch=%d", mx.getOK, mx.getErr, mx.getMismatch)
	log.Printf("Put latency: avg=%v p50=%v p95=%v p99=%v", avg(mx.putLatencies), pct(mx.putLatencies, 0.50), pct(mx.putLatencies, 0.95), pct(mx.putLatencies, 0.99))
	log.Printf("Get latency: avg=%v p50=%v p95=%v p99=%v", avg(mx.getLatencies), pct(mx.getLatencies, 0.50), pct(mx.getLatencies, 0.95), pct(mx.getLatencies, 0.99))
}

func startNode(nodes []*simNode, i int, addr string) {
	ctx, cancel := context.WithCancel(context.Background())
	n := node.NewNode(addr)
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

func pickAlive(nodes []*simNode) int {
	aliveIdx := make([]int, 0, len(nodes))
	for i, sn := range nodes {
		if sn != nil && sn.alive {
			aliveIdx = append(aliveIdx, i)
		}
	}
	if len(aliveIdx) == 0 {
		return -1
	}
	return aliveIdx[mrand.Intn(len(aliveIdx))]
}

func randSampleAlive(nodes []*simNode, n int) []int {
	aliveIdx := make([]int, 0, len(nodes))
	for i, sn := range nodes {
		if sn != nil && sn.alive {
			aliveIdx = append(aliveIdx, i)
		}
	}
	if len(aliveIdx) == 0 {
		return nil
	}
	if n > len(aliveIdx) {
		n = len(aliveIdx)
	}
	perm := mrand.Perm(len(aliveIdx))
	out := make([]int, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, aliveIdx[perm[i]])
	}
	return out
}

func randSampleAddrs(addrs []string, n int) []string {
	if n > len(addrs) {
		n = len(addrs)
	}
	perm := mrand.Perm(len(addrs))
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, addrs[perm[i]])
	}
	return out
}

func randomKey() string {
	var b [8]byte
	if _, err := crand.Read(b[:]); err != nil {
		// fallback to math/rand if crypto/rand fails
		for i := range b {
			b[i] = byte(mrand.Intn(256))
		}
	}
	return "k:" + hex.EncodeToString(b[:])
}

func randomValue(min, max int) []byte {
	if max <= min {
		max = min + 1
	}
	n := min + mrand.Intn(max-min)
	out := make([]byte, n)
	if _, err := crand.Read(out); err != nil {
		for i := range out {
			out[i] = byte(mrand.Intn(256))
		}
	}
	return out
}
