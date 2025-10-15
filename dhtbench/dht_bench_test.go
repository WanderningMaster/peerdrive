package dhtbench

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/WanderningMaster/peerdrive/internal/node"
)

var (
	flagNumNodes        = flag.Int("bench.nodes", 32, "number of DHT nodes to launch")
	flagBootstrapDegree = flag.Int("bench.bootstrap", 3, "bootstrap degree per node (random peers chosen from already-started nodes)")
	flagValueBytes      = flag.Int("bench.valbytes", 64, "value size in bytes for Store")
	flagSeed            = flag.Int64("bench.seed", 1, "PRNG seed for reproducibility")
	flagListenBasePort  = flag.Int("bench.baseport", 40000, "base TCP port for first node (increments by 1 per node)")
	flagOpTimeoutMS     = flag.Int("bench.timeout_ms", 1000, "per-operation context timeout in ms (Store/Get)")
	flagWarmupMS        = flag.Int("bench.warmup_ms", 500, "warmup sleep after cluster bootstrap in ms")
)

func BenchmarkDHT_StoreGet(b *testing.B) {
	r := rand.New(rand.NewSource(*flagSeed))

	ctx := b.Context()

	addrs := make([]string, *flagNumNodes)
	nodes := make([]*node.Node, *flagNumNodes)
	for i := 0; i < *flagNumNodes; i++ {
		addr := fmt.Sprintf("127.0.0.1:%d", *flagListenBasePort+i)
		addrs[i] = addr

		n := node.NewNode(addr)

		go func(n *node.Node) {
			n.ListenAndServe(ctx)
		}(n)

		nodes[i] = n
	}

	for i := 0; i < *flagNumNodes; i++ {
		peerCount := min(*flagBootstrapDegree, i)
		if peerCount == 0 {
			continue
		}

		peerIdxs := uniqueRandomInts(r, peerCount, 0, i)
		peers := make([]string, 0, len(peerIdxs))
		for _, idx := range peerIdxs {
			peers = append(peers, addrs[idx])
		}

		nodes[i].Bootstrap(ctx, peers)
	}

	time.Sleep(time.Duration(*flagWarmupMS) * time.Millisecond)

	var storeAttempts int64
	var storeErrors int64
	var getAttempts int64
	var getErrors int64
	var hits int64

	var kv sync.Map

	valBuf := make([]byte, *flagValueBytes)
	timeout := time.Duration(*flagOpTimeoutMS) * time.Millisecond

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		gr := rand.New(rand.NewSource(r.Int63()))
		for pb.Next() {
			key := randKey(gr)
			val := randBytes(gr, capOr(*flagValueBytes, 1), valBuf)

			storeIdx := gr.Intn(*flagNumNodes)
			getIdx := gr.Intn(*flagNumNodes)

			sctx, scancel := context.WithTimeout(ctx, timeout)
			if err := nodes[storeIdx].Store(sctx, key, val); err != nil {
				atomic.AddInt64(&storeErrors, 1)
				scancel()
				continue
			}
			scancel()
			kv.Store(key, val)

			atomic.AddInt64(&getAttempts, 1)
			gctx, gcancel := context.WithTimeout(ctx, timeout)
			got, err := nodes[getIdx].Get(gctx, key)
			if err != nil {
				atomic.AddInt64(&getErrors, 1)
				gcancel()
				continue
			}
			gcancel()

			if expAny, ok := kv.Load(key); ok {
				exp := expAny.([]byte)
				if bytesEqual(exp, got) {
					atomic.AddInt64(&hits, 1)
				}
			}
		}
	})
	b.StopTimer()

	totalGets := atomic.LoadInt64(&getAttempts)
	totalStores := atomic.LoadInt64(&storeAttempts)
	hitRate := float64(atomic.LoadInt64(&hits)) / float64(max64(1, totalGets))
	errRate := float64(atomic.LoadInt64(&storeErrors)+atomic.LoadInt64(&getErrors)) / float64(max64(1, totalGets+totalStores))

	b.ReportMetric(hitRate, "hit_rate")
	b.ReportMetric(errRate, "err_rate")
	b.ReportMetric(float64(totalStores+totalGets), "ops_total")
	b.ReportMetric(float64(totalGets), "gets")
	b.ReportMetric(float64(totalStores), "stores")
}

func uniqueRandomInts(r *rand.Rand, n, minIncl, maxExcl int) []int {
	if n <= 0 {
		return nil
	}
	if maxExcl-minIncl < n {
		all := make([]int, 0, maxExcl-minIncl)
		for i := minIncl; i < maxExcl; i++ {
			all = append(all, i)
		}
		r.Shuffle(len(all), func(i, j int) { all[i], all[j] = all[j], all[i] })
		return all[:n]
	}
	seen := make(map[int]struct{}, n)
	out := make([]int, 0, n)
	for len(out) < n {
		x := minIncl + r.Intn(maxExcl-minIncl)
		if _, ok := seen[x]; ok {
			continue
		}
		seen[x] = struct{}{}
		out = append(out, x)
	}
	return out
}

func randBytes(r *rand.Rand, n int, scratch []byte) []byte {
	if n <= 0 {
		return nil
	}
	if cap(scratch) < n {
		scratch = make([]byte, n)
	}
	b := scratch[:n]
	for i := range n {
		b[i] = byte(r.Intn(256))
	}
	out := make([]byte, n)
	copy(out, b)
	return out
}

func randKey(r *rand.Rand) string {
	const kb = 16
	buf := make([]byte, kb)
	for i := range buf {
		buf[i] = byte(r.Intn(256))
	}
	return fmt.Sprintf("%x", buf)
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func capOr(v, floor int) int {
	if v < floor {
		return floor
	}
	return v
}
