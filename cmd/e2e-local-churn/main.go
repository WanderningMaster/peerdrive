package main

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"flag"
	"fmt"
	"log"
	mrand "math/rand"
	"sort"
	"strings"
	"time"

	"github.com/WanderningMaster/peerdrive/configuration"
	"github.com/WanderningMaster/peerdrive/internal/block"
	blockfetcher "github.com/WanderningMaster/peerdrive/internal/block-fetcher"
	"github.com/WanderningMaster/peerdrive/internal/dag"
	"github.com/WanderningMaster/peerdrive/internal/node"
	"github.com/WanderningMaster/peerdrive/internal/storage"
)

// e2e-local-churn: same as e2e-local but randomly restarts nodes during trials.

type simNode struct {
	addr string
	n    *node.Node
	mem  *storage.MemStore
	stop func()
}

func main() {
	// Cluster / workload flags (mirrors cmd/e2e-local)
	var (
		nodes       = flag.Int("nodes", 25, "number of nodes in the cluster")
		basePort    = flag.Int("base-port", 9100, "base TCP port for nodes")
		warmup      = flag.Duration("warmup", 750*time.Millisecond, "time to wait after starting nodes before bootstrap")
		settle      = flag.Duration("settle", 1*time.Second, "time to wait after build before measurements")
		trials      = flag.Int("trials", 50, "number of fetch trials")
		payloadSize = flag.Int("size", 2<<20, "payload size in bytes")
		chunkSize   = flag.Int("chunk", 1<<20, "DAG chunk size in bytes")
		fanout      = flag.Int("fanout", 256, "DAG fanout for tree nodes")
		fetchPar    = flag.Int("fetch-par", 16, "concurrency during fetch (parallel block downloads)")
	)

	// Churn flags
	var (
		churnInterval  = flag.Duration("churn-interval", 3*time.Second, "interval between churn attempts")
		churnProb      = flag.Float64("churn-prob", 1, "probability to restart one random node each interval [0..1]")
		churnStartWait = flag.Duration("churn-after", 2*time.Second, "delay after bootstrap before starting churn")
		churnPeerFan   = flag.Int("churn-peers", 5, "number of peers to connect on restart (bootstrap fan)")
		// Simulated randomized node downtime and per-request serve latency
		downTimeMin   = flag.Duration("down-time-min", 200*time.Millisecond, "minimum simulated node down-time on churn")
		downTimeMax   = flag.Duration("down-time-max", 1500*time.Millisecond, "maximum simulated node down-time on churn")
		serveDelayMin = flag.Duration("serve-delay-min", 2*time.Millisecond, "minimum per-RPC serve delay when serving blocks")
		serveDelayMax = flag.Duration("serve-delay-max", 20*time.Millisecond, "maximum per-RPC serve delay when serving blocks")
	)

	// Config flags (from configuration.Config, excluding IdBits)
	var (
		kBucketK          = flag.Int("k", configuration.Default().KBucketK, "k-bucket size (K)")
		alpha             = flag.Int("alpha", configuration.Default().Alpha, "alpha concurrency for lookups")
		replicas          = flag.Int("replicas", configuration.Default().Replicas, "replica count for DHT values")
		rpcTimeout        = flag.Duration("rpc", configuration.Default().RpcTimeout, "RPC timeout")
		bucketRefresh     = flag.Duration("bucket-refresh", configuration.Default().BucketRefresh, "routing table refresh interval")
		recordTTL         = flag.Duration("record-ttl", configuration.Default().RecordTTL, "DHT record TTL")
		republishInterval = flag.Duration("republish", configuration.Default().RepublishInterval, "republish interval for origin values")
		gcInterval        = flag.Duration("gc", configuration.Default().GCInterval, "DHT GC interval")
		revalidate        = flag.Duration("revalidate", configuration.Default().RevalidateInterval, "peer revalidation interval")
		maxValueSize      = flag.Int("max-value", configuration.Default().MaxValueSize, "max DHT value size (bytes)")
		failureThreshold  = flag.Int("fail-threshold", configuration.Default().FailureThreshold, "peer failure threshold before eviction")
		softPinTTL        = flag.Duration("soft-ttl", configuration.Default().SoftPinTTL, "soft pin TTL for in-memory blockstore")
	)

	flag.Parse()

	// Prepare config from flags
	conf := configuration.Default()
	conf.KBucketK = *kBucketK
	conf.Alpha = *alpha
	conf.Replicas = *replicas
	conf.RpcTimeout = *rpcTimeout
	conf.BucketRefresh = *bucketRefresh
	conf.RecordTTL = *recordTTL
	conf.RepublishInterval = *republishInterval
	conf.GCInterval = *gcInterval
	conf.RevalidateInterval = *revalidate
	conf.MaxValueSize = *maxValueSize
	conf.FailureThreshold = *failureThreshold

	if *nodes <= 0 {
		log.Fatalf("nodes must be > 0")
	}
	if *payloadSize < 0 || *chunkSize <= 0 || *fanout < 2 {
		log.Fatalf("invalid DAG params: size=%d chunk=%d fanout=%d", *payloadSize, *chunkSize, *fanout)
	}
	if *churnProb < 0.0 || *churnProb > 1.0 {
		log.Fatalf("churn-prob must be in [0..1]")
	}

	// Seed PRNG for test randomness
	mrand.Seed(time.Now().UnixNano())

	// Start cluster
	sims := startCluster(*nodes, *basePort, conf, *softPinTTL, *serveDelayMin, *serveDelayMax)
	defer stopCluster(sims)

	// Warmup, then bootstrap
	time.Sleep(*warmup)
	bootstrapCluster(sims)

	// Start churn after a short delay
	time.Sleep(*churnStartWait)
	churnCtx, churnCancel := contextWithCancel()
	go runChurn(churnCtx, sims, conf, *softPinTTL, *churnInterval, *churnProb, *churnPeerFan, *downTimeMin, *downTimeMax, *serveDelayMin, *serveDelayMax)

	// Measure availability and latency over randomized upload/download trials
	fmt.Println("=== Configuration (local-only with churn) ===")
	fmt.Printf("nodes=%d basePort=%d size=%d chunk=%d fanout=%d fetch-par=%d\n",
		*nodes, *basePort, *payloadSize, *chunkSize, *fanout, *fetchPar)
	fmt.Printf("K=%d alpha=%d replicas=%d rpc=%s refresh=%s ttl=%s republish=%s gc=%s revalidate=%s maxValue=%d failThresh=%d softTTL=%s\n",
		conf.KBucketK, conf.Alpha, conf.Replicas, conf.RpcTimeout, conf.BucketRefresh, conf.RecordTTL,
		conf.RepublishInterval, conf.GCInterval, conf.RevalidateInterval, conf.MaxValueSize, conf.FailureThreshold, *softPinTTL)
	fmt.Printf("churn: interval=%s prob=%.2f after=%s peers=%d down-time=[%s..%s] serve-delay=[%s..%s]\n", *churnInterval, *churnProb, *churnStartWait, *churnPeerFan, *downTimeMin, *downTimeMax, *serveDelayMin, *serveDelayMax)

	res := runTrials(sims, *trials, *fetchPar, conf, *payloadSize, *chunkSize, *fanout, *settle)
	churnCancel()
	printResults(res)
}

// startCluster spins up N nodes with in-memory blockstores and applies the provided config.
func startCluster(n int, basePort int, conf configuration.Config, softTTL time.Duration, serveDelayMin, serveDelayMax time.Duration) []*simNode {
	sims := make([]*simNode, n)
	for i := 0; i < n; i++ {
		addr := fmt.Sprintf("127.0.0.1:%d", basePort+i)
		sn := &simNode{addr: addr}
		// Create node
		nd := node.NewNode(addr).WithConfig(conf)
		// In-memory blockstore with fetcher and soft TTL
		mem := storage.NewMemStore(
			storage.WithFetcher(blockfetcher.New(nd)),
			storage.WithSoftTTL(softTTL),
		)
		if serveDelayMax > 0 {
			nd.SetBlockProvider(newDelayedProvider(mem, serveDelayMin, serveDelayMax))
		} else {
			nd.SetBlockProvider(mem)
		}
		sn.n = nd
		sn.mem = mem
		// Start server
		ctx, cancel := contextWithCancel()
		sn.stop = cancel
		go func(n *node.Node, id string) {
			if err := n.ListenAndServe(ctx); err != nil {
				log.Printf("node %s stopped: %v", id, err)
			}
		}(nd, nd.ID.String()[:8])
		sims[i] = sn
	}
	return sims
}

func stopCluster(sims []*simNode) {
	for _, sn := range sims {
		if sn != nil && sn.stop != nil {
			sn.stop()
		}
	}
}

// bootstrapCluster connects each node i to a random subset of previous nodes for quick convergence.
func bootstrapCluster(sims []*simNode) {
	for i := 1; i < len(sims); i++ {
		prev := sims[:i]
		m := min(i, 5)
		perm := mrand.Perm(i)
		peers := make([]string, 0, m)
		for j := 0; j < m; j++ {
			peers = append(peers, prev[perm[j]].addr)
		}
		go sims[i].n.Bootstrap(contextBackground(), peers)
	}
}

// buildLocal constructs the DAG and stores all blocks only in the local in-memory store.
func buildLocal(ctx context.Context, sn *simNode, size int, chunk int, fanout int) (block.CID, []byte) {
	if size < 0 {
		size = 0
	}
	payload := make([]byte, size)
	if _, err := crand.Read(payload); err != nil {
		log.Printf("rand: %v", err)
		return block.CID{}, nil
	}
	builder := dag.DagBuilder{ChunkSize: chunk, Fanout: fanout, Codec: "cbor", Store: sn.mem}
	_, cid, err := builder.BuildFromReader(ctx, "payload.bin", "application/octet-stream", bytes.NewReader(payload))
	if err != nil {
		log.Printf("build failed: %v", err)
		return block.CID{}, nil
	}
	return cid, payload
}

type trialResult struct {
	Successes int
	Failures  int
	Latencies []time.Duration
}

func runTrials(
	sims []*simNode,
	trials int,
	fetchPar int,
	conf configuration.Config,
	payloadSize int,
	chunkSize int,
	fanout int,
	settle time.Duration,
) trialResult {
	if trials <= 0 {
		trials = 1
	}
	lats := make([]time.Duration, 0, trials)
	succ := 0
	fail := 0
	for i := 0; i < trials; i++ {
		fmt.Printf("%d/%d\n", i+1, trials)
		// Choose random uploader node and build content locally only
		up := sims[mrand.Intn(len(sims))]
		bctx, bcancel := contextWithTimeout(30 * time.Second)
		cid, payload := buildLocal(bctx, up, payloadSize, chunkSize, fanout)
		bcancel()
		if (cid == block.CID{}) {
			fail++
			continue
		}

		// Allow local provider announcements to settle (via mem store)
		if settle > 0 {
			time.Sleep(settle)
		}

		// Fetch from a random (possibly different) node
		dn := sims[mrand.Intn(len(sims))]
		ctx, cancel := contextWithTimeout(maxDuration(5*conf.RpcTimeout, 10*time.Second))
		start := time.Now()
		data, err := dag.FetchParallel(ctx, dn.mem, cid, fetchPar)
		cancel()
		if err != nil || len(data) != len(payload) {
			fail++
			continue
		}
		succ++
		lats = append(lats, time.Since(start))
	}
	return trialResult{Successes: succ, Failures: fail, Latencies: lats}
}

// Churn management: periodically restarts random nodes with given probability.
func runChurn(ctx context.Context, sims []*simNode, conf configuration.Config, softTTL time.Duration, interval time.Duration, prob float64, peerFan int, downMin, downMax time.Duration, serveDelayMin, serveDelayMax time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if mrand.Float64() > prob {
				continue
			}
			if len(sims) == 0 {
				continue
			}
			idx := mrand.Intn(len(sims))
			restartNodeAt(sims, idx, conf, softTTL, peerFan, downMin, downMax, serveDelayMin, serveDelayMax)
		}
	}
}

func restartNodeAt(sims []*simNode, idx int, conf configuration.Config, softTTL time.Duration, peerFan int, downMin, downMax time.Duration, serveDelayMin, serveDelayMax time.Duration) {
	if idx < 0 || idx >= len(sims) {
		return
	}
	old := sims[idx]
	if old == nil {
		return
	}
	// Stop old node
	if old.stop != nil {
		old.stop()
	}
	// Simulated down-time window before the node is back
	time.Sleep(randDuration(downMin, downMax))

	// Start new node with a fresh in-memory store (simulate real restart)
	addr := old.addr
	nd := node.NewNode(addr).WithConfig(conf)
	mem := storage.NewMemStore(
		storage.WithFetcher(blockfetcher.New(nd)),
		storage.WithSoftTTL(softTTL),
	)
	if serveDelayMax > 0 {
		nd.SetBlockProvider(newDelayedProvider(mem, serveDelayMin, serveDelayMax))
	} else {
		nd.SetBlockProvider(mem)
	}
	ctx, cancel := contextWithCancel()
	sn := &simNode{addr: addr, n: nd, mem: mem, stop: cancel}
	sims[idx] = sn
	go func(n *node.Node, id string) {
		if err := n.ListenAndServe(ctx); err != nil {
			log.Printf("restarted node %s stopped: %v", id, err)
		}
	}(nd, nd.ID.String()[:8])

	// Bootstrap to a sample of peers
	peers := samplePeersExcept(sims, idx, min(peerFan, len(sims)-1))
	if len(peers) > 0 {
		go nd.Bootstrap(contextBackground(), peers)
	}
}

func samplePeersExcept(sims []*simNode, except int, m int) []string {
	if m <= 0 {
		return nil
	}
	idxs := make([]int, 0, len(sims)-1)
	for i := range sims {
		if i != except {
			idxs = append(idxs, i)
		}
	}
	if len(idxs) == 0 {
		return nil
	}
	// shuffle
	mrand.Shuffle(len(idxs), func(i, j int) { idxs[i], idxs[j] = idxs[j], idxs[i] })
	take := min(m, len(idxs))
	out := make([]string, 0, take)
	for i := 0; i < take; i++ {
		out = append(out, sims[idxs[i]].addr)
	}
	return out
}

// delayedProvider wraps a MemStore and injects a randomized delay before serving local block reads.
type delayedProvider struct {
	inner *storage.MemStore
	min   time.Duration
	max   time.Duration
}

func newDelayedProvider(inner *storage.MemStore, min, max time.Duration) *delayedProvider {
	if max < min {
		max = min
	}
	return &delayedProvider{inner: inner, min: min, max: max}
}

func (d *delayedProvider) sleep() {
	if d.max <= 0 {
		return
	}
	dur := randDuration(d.min, d.max)
	if dur > 0 {
		time.Sleep(dur)
	}
}

// Implement node.BlockProvider by forwarding to the inner store.
func (d *delayedProvider) GetBlockLocal(ctx context.Context, c block.CID) (*block.Block, error) {
	d.sleep()
	return d.inner.GetBlockLocal(ctx, c)
}
func (d *delayedProvider) PutBlock(ctx context.Context, b *block.Block) error {
	return d.inner.PutBlock(ctx, b)
}
func (d *delayedProvider) Pin(ctx context.Context, c block.CID) error { return d.inner.Pin(ctx, c) }
func (d *delayedProvider) PinSoft(ctx context.Context, c block.CID) error {
	return d.inner.PinSoft(ctx, c)
}
func (d *delayedProvider) Unpin(ctx context.Context, c block.CID) error { return d.inner.Unpin(ctx, c) }

// randDuration returns a random duration in [min, max]. If max <= 0, returns 0.
func randDuration(min, max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	if max < min {
		max = min
	}
	delta := max - min
	if delta <= 0 {
		return min
	}
	return min + time.Duration(mrand.Int63n(int64(delta)))
}

func printResults(r trialResult) {
	total := r.Successes + r.Failures
	availability := 0.0
	if total > 0 {
		availability = float64(r.Successes) / float64(total)
	}

	fmt.Println("=== Results ===")
	fmt.Printf("trials=%d success=%d fail=%d availability=%.2f%%\n", total, r.Successes, r.Failures, 100*availability)
	if len(r.Latencies) == 0 {
		fmt.Println("no successful fetches; no latency stats")
		return
	}

	sort.Slice(r.Latencies, func(i, j int) bool { return r.Latencies[i] < r.Latencies[j] })
	min := r.Latencies[0]
	max := r.Latencies[len(r.Latencies)-1]
	med := r.Latencies[len(r.Latencies)/2]
	p95 := percentile(r.Latencies, 0.95)
	p99 := percentile(r.Latencies, 0.99)
	mean := meanDur(r.Latencies)

	fmt.Printf("latency: min=%s p50=%s p95=%s p99=%s max=%s mean=%s\n", min, med, p95, p99, max, mean)
}

func percentile(ds []time.Duration, p float64) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	if p <= 0 {
		return ds[0]
	}
	if p >= 1 {
		return ds[len(ds)-1]
	}
	idx := int(float64(len(ds)-1) * p)
	return ds[idx]
}

func meanDur(ds []time.Duration) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	var sum time.Duration
	for _, d := range ds {
		sum += d
	}
	return time.Duration(int64(sum) / int64(len(ds)))
}

// Utilities: context helpers to avoid importing context everywhere above
// Minimal wrappers to keep imports tidy and explicit where used.
type emptyCtx struct{}

func contextBackground() context.Context           { return context.Background() }
func contextWithCancel() (context.Context, func()) { return context.WithCancel(context.Background()) }
func contextWithTimeout(d time.Duration) (context.Context, func()) {
	return context.WithTimeout(context.Background(), d)
}

// small helpers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

// Ensure context is imported
// Keep import list tidy even if editors reorder
// (alias to avoid "context" unused during refactors)
var _ = strings.Builder{} // keep strings import used
