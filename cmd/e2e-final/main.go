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
	"github.com/WanderningMaster/peerdrive/internal/service"
	"github.com/WanderningMaster/peerdrive/internal/storage"
)

type simNode struct {
	addr string
	n    *node.Node
	mem  *storage.MemStore
	stop func()
}

func main() {
	// Cluster / workload flags
	var (
		nodes       = flag.Int("nodes", 25, "number of nodes in the cluster")
		basePort    = flag.Int("base-port", 9100, "base TCP port for nodes")
		warmup      = flag.Duration("warmup", 750*time.Millisecond, "time to wait after starting nodes before bootstrap")
		settle      = flag.Duration("settle", 1*time.Second, "time to wait after placement before measurements")
		trials      = flag.Int("trials", 150, "number of fetch trials")
		payloadSize = flag.Int("size", 2<<20, "payload size in bytes")
		chunkSize   = flag.Int("chunk", 1<<20, "DAG chunk size in bytes")
		fanout      = flag.Int("fanout", 256, "DAG fanout for tree nodes")
		fetchPar    = flag.Int("fetch-par", 16, "concurrency during fetch (parallel block downloads)")
		keepLocal   = flag.Float64("keep-local", 0.5, "fraction of non-manifest blocks to keep locally [0..1]")
		reuseBase   = flag.Bool("reuse-base", false, "reuse same base buffer across trials and mutate per upload")
		mutateBytes = flag.Int("mutate", 0, "number of random byte positions to mutate per upload when reusing base")
	)

	// Churn flags
	var (
		churnInterval  = flag.Duration("churn-interval", 250*time.Millisecond, "interval between churn attempts")
		churnProb      = flag.Float64("churn-prob", 1, "probability to restart one random node each interval [0..1]")
		churnStartWait = flag.Duration("churn-after", 2*time.Second, "delay after bootstrap before starting churn")
		churnPeerFan   = flag.Int("churn-peers", 5, "number of peers to connect on restart (bootstrap fan)")
		// Simulated randomized node downtime and per-request serve latency
		downTimeMin   = flag.Duration("down-time-min", 200*time.Millisecond, "minimum simulated node down-time on churn")
		downTimeMax   = flag.Duration("down-time-max", 1500*time.Millisecond, "maximum simulated node down-time on churn")
		serveDelayMin = flag.Duration("serve-delay-min", 0*time.Millisecond, "minimum per-RPC serve delay when serving blocks")
		serveDelayMax = flag.Duration("serve-delay-max", 0*time.Millisecond, "maximum per-RPC serve delay when serving blocks")
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
	if *keepLocal < 0.0 || *keepLocal > 1.0 {
		log.Fatalf("keep-local must be in [0..1]")
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
	fmt.Println("=== Configuration (with churn) ===")
	fmt.Printf("nodes=%d basePort=%d size=%d chunk=%d fanout=%d fetch-par=%d keep-local=%.2f reuse-base=%t mutate=%d\n",
		*nodes, *basePort, *payloadSize, *chunkSize, *fanout, *fetchPar, *keepLocal, *reuseBase, *mutateBytes)
	fmt.Printf("K=%d alpha=%d replicas=%d rpc=%s refresh=%s ttl=%s republish=%s gc=%s revalidate=%s maxValue=%d failThresh=%d softTTL=%s\n",
		conf.KBucketK, conf.Alpha, conf.Replicas, conf.RpcTimeout, conf.BucketRefresh, conf.RecordTTL,
		conf.RepublishInterval, conf.GCInterval, conf.RevalidateInterval, conf.MaxValueSize, conf.FailureThreshold, *softPinTTL)
	fmt.Printf("churn: interval=%s prob=%.2f after=%s peers=%d down-time=[%s..%s] serve-delay=[%s..%s]\n", *churnInterval, *churnProb, *churnStartWait, *churnPeerFan, *downTimeMin, *downTimeMax, *serveDelayMin, *serveDelayMax)

	res := runTrials(sims, *trials, *fetchPar, conf, *payloadSize, *chunkSize, *fanout, *keepLocal, *settle, *reuseBase, *mutateBytes)
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

func buildAndDistribute(ctx context.Context, sn *simNode, payload []byte, chunk int, fanout int, keepLocal float64) (block.CID, []byte) {
	ds := service.NewDistStore(sn.n, sn.mem, sn.n.Replicas(), service.KeepLocalSelector(true, keepLocal))
	builder := dag.DagBuilder{ChunkSize: chunk, Fanout: fanout, Codec: "cbor", Store: ds}
	_, cid, err := builder.BuildFromReader(ctx, "payload.bin", "application/octet-stream", bytes.NewReader(payload))
	if err != nil {
		log.Printf("build failed: %v", err)
		return block.CID{}, nil
	}
	return cid, payload
}

type trialResult struct {
	Successes     int
	Failures      int
	UploadLat     []time.Duration
	FetchLat      []time.Duration
	UploadTp      []float64
	FetchTp       []float64
	UploadedBytes int64
	FetchedBytes  int64
}

func runTrials(
	sims []*simNode,
	trials int,
	fetchPar int,
	conf configuration.Config,
	payloadSize int,
	chunkSize int,
	fanout int,
	keepLocal float64,
	settle time.Duration,
	reuseBase bool,
	mutate int,
) trialResult {
	if trials <= 0 {
		trials = 1
	}
	uplats := make([]time.Duration, 0, trials)
	fxlats := make([]time.Duration, 0, trials)
	uptps := make([]float64, 0, trials)
	fxtps := make([]float64, 0, trials)
	succ := 0
	fail := 0
	var uploaded int64
	var fetched int64
	var base []byte
	if reuseBase {
		if payloadSize < 0 {
			payloadSize = 0
		}
		base = make([]byte, payloadSize)
		if _, err := crand.Read(base); err != nil {
			log.Printf("rand base: %v", err)
			base = nil
		}
	}

	for i := 0; i < trials; i++ {
		// fmt.Printf("%d/%d\n", i+1, trials)
		// Choose random uploader node and build + distribute unique content
		up := sims[mrand.Intn(len(sims))]
		bctx, bcancel := contextWithTimeout(30 * time.Second)
		// Prepare payload: either fresh random, or mutated copy of a base buffer
		var payload []byte
		if reuseBase && base != nil {
			payload = mutateCopy(base, mutate)
		} else {
			if payloadSize < 0 {
				payloadSize = 0
			}
			payload = make([]byte, payloadSize)
			if _, err := crand.Read(payload); err != nil {
				log.Printf("rand: %v", err)
				bcancel()
				fail++
				continue
			}
		}
		upStart := time.Now()
		cid, payload := buildAndDistribute(bctx, up, payload, chunkSize, fanout, keepLocal)
		upDur := time.Since(upStart)
		bcancel()
		if (cid == block.CID{}) {
			fail++
			continue
		}
		uploaded += int64(len(payload))
		uplats = append(uplats, upDur)
		if upDur > 0 {
			uptps = append(uptps, bytesPerSec(float64(len(payload)), upDur))
		}

		// Allow provider/DHT records to settle before fetching
		if settle > 0 {
			time.Sleep(settle)
		}

		// Fetch from a random (possibly different) node
		dn := sims[mrand.Intn(len(sims))]
		for dn.n.ID == up.n.ID {
			dn = sims[mrand.Intn(len(sims))]
		}
		ctx, cancel := contextWithTimeout(maxDuration(5*conf.RpcTimeout, 10*time.Second))
		fxStart := time.Now()
		data, err := dag.FetchParallel(ctx, dn.mem, cid, fetchPar)
		cancel()
		if err != nil || len(data) != len(payload) {
			fail++
			continue
		}
		succ++
		fxDur := time.Since(fxStart)
		fxlats = append(fxlats, fxDur)
		fetched += int64(len(data))
		if fxDur > 0 {
			fxtps = append(fxtps, bytesPerSec(float64(len(data)), fxDur))
		}
	}
	return trialResult{Successes: succ, Failures: fail, UploadLat: uplats, FetchLat: fxlats, UploadTp: uptps, FetchTp: fxtps, UploadedBytes: uploaded, FetchedBytes: fetched}
}

// gcAndStats runs GC on all nodes, then aggregates cluster-wide stats.
func gcAndStats(sims []*simNode) (totalBlocks int, totalBytes int64) {
	var blocks int
	var bytes int64
	ctx := contextBackground()
	for _, sn := range sims {
		if sn == nil || sn.mem == nil {
			continue
		}
		b, by, err := sn.mem.Stats(ctx)
		if err != nil {
			continue
		}
		blocks += b
		bytes += by
	}
	fmt.Println("before GC")
	fmt.Println("blocks", blocks)
	fmt.Println("bytes", bytes)

	blocks = 0
	bytes = 0
	ctx = contextBackground()
	for _, sn := range sims {
		if sn == nil || sn.mem == nil {
			continue
		}
		_, _ = sn.mem.GC(ctx)
	}
	for _, sn := range sims {
		if sn == nil || sn.mem == nil {
			continue
		}
		b, by, err := sn.mem.Stats(ctx)
		if err != nil {
			continue
		}
		blocks += b
		bytes += by
	}
	return blocks, bytes
}

func printResults(r trialResult) {
	total := r.Successes + r.Failures
	availability := 0.0
	if total > 0 {
		availability = float64(r.Successes) / float64(total)
	}

	fmt.Println("=== Results ===")
	fmt.Printf("trials=%d success=%d fail=%d availability=%.2f%%\n", total, r.Successes, r.Failures, 100*availability)
	// Upload metrics
	if len(r.UploadLat) > 0 {
		sort.Slice(r.UploadLat, func(i, j int) bool { return r.UploadLat[i] < r.UploadLat[j] })
		uMin := r.UploadLat[0]
		uMax := r.UploadLat[len(r.UploadLat)-1]
		uMed := r.UploadLat[len(r.UploadLat)/2]
		uP95 := percentile(r.UploadLat, 0.95)
		uP99 := percentile(r.UploadLat, 0.99)
		uMean := meanDur(r.UploadLat)
		fmt.Printf("upload latency: min=%s p50=%s p95=%s p99=%s max=%s mean=%s\n", uMin, uMed, uP95, uP99, uMax, uMean)
	} else {
		fmt.Println("upload latency: no data")
	}

	// Fetch metrics
	if len(r.FetchLat) > 0 {
		sort.Slice(r.FetchLat, func(i, j int) bool { return r.FetchLat[i] < r.FetchLat[j] })
		fMin := r.FetchLat[0]
		fMax := r.FetchLat[len(r.FetchLat)-1]
		fMed := r.FetchLat[len(r.FetchLat)/2]
		fP95 := percentile(r.FetchLat, 0.95)
		fP99 := percentile(r.FetchLat, 0.99)
		fMean := meanDur(r.FetchLat)
		fmt.Printf("fetch latency:  min=%s p50=%s p95=%s p99=%s max=%s mean=%s\n", fMin, fMed, fP95, fP99, fMax, fMean)
	} else {
		fmt.Println("fetch latency: no data")
	}

	// Throughput metrics (bytes/sec). Report mean and p95; show totals too.
	if len(r.UploadTp) > 0 {
		uMeanTp := meanFloat(r.UploadTp)
		uP95Tp := percentileFloat(r.UploadTp, 0.95)
		fmt.Printf("upload throughput: mean=%.2f MB/s p95=%.2f MB/s total-bytes=%d\n", uMeanTp/1e6, uP95Tp/1e6, r.UploadedBytes)
	} else {
		fmt.Printf("upload throughput: no data, total-bytes=%d\n", r.UploadedBytes)
	}
	if len(r.FetchTp) > 0 {
		fMeanTp := meanFloat(r.FetchTp)
		fP95Tp := percentileFloat(r.FetchTp, 0.95)
		fmt.Printf("fetch throughput:  mean=%.2f MB/s p95=%.2f MB/s total-bytes=%d\n", fMeanTp/1e6, fP95Tp/1e6, r.FetchedBytes)
	} else {
		fmt.Printf("fetch throughput: no data, total-bytes=%d\n", r.FetchedBytes)
	}
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

func mutateCopy(base []byte, m int) []byte {
	out := make([]byte, len(base))
	copy(out, base)
	if m <= 0 || len(out) == 0 {
		return out
	}
	if m > len(out) {
		m = len(out)
	}
	// sample m unique indices to mutate
	// do this by shuffling a small prefix of indices
	idxs := make([]int, len(out))
	for i := range idxs {
		idxs[i] = i
	}
	mrand.Shuffle(len(idxs), func(i, j int) { idxs[i], idxs[j] = idxs[j], idxs[i] })
	for i := 0; i < m; i++ {
		pos := idxs[i]
		// simple mutation: XOR with random non-zero byte
		b := byte(mrand.Intn(255) + 1)
		out[pos] ^= b
	}
	return out
}

// numeric helpers
func bytesPerSec(n float64, d time.Duration) float64 {
	if d <= 0 {
		return 0
	}
	return n / d.Seconds()
}
func percentileFloat(xs []float64, p float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	ys := make([]float64, len(xs))
	copy(ys, xs)
	sort.Slice(ys, func(i, j int) bool { return ys[i] < ys[j] })
	if p <= 0 {
		return ys[0]
	}
	if p >= 1 {
		return ys[len(ys)-1]
	}
	idx := int(float64(len(ys)-1) * p)
	return ys[idx]
}
func meanFloat(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var s float64
	for _, v := range xs {
		s += v
	}
	return s / float64(len(xs))
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
