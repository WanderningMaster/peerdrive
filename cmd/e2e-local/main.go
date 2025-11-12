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

// e2e-local: same benchmarking flow as cmd/e2e but uses only
// in-memory local stores; no distributed DistStore for placement.

type simNode struct {
    addr string
    n    *node.Node
    mem  *storage.MemStore
    stop func()
}

func main() {
    // Cluster / workload flags (mirrors cmd/e2e)
    var (
        nodes       = flag.Int("nodes", 25, "number of nodes in the cluster")
        basePort    = flag.Int("base-port", 9100, "base TCP port for nodes")
        warmup      = flag.Duration("warmup", 750*time.Millisecond, "time to wait after starting nodes before bootstrap")
        settle      = flag.Duration("settle", 1*time.Second, "time to wait after placement before measurements")
        trials      = flag.Int("trials", 50, "number of fetch trials")
        payloadSize = flag.Int("size", 2<<20, "payload size in bytes")
        chunkSize   = flag.Int("chunk", 1<<20, "DAG chunk size in bytes")
        fanout      = flag.Int("fanout", 256, "DAG fanout for tree nodes")
        fetchPar    = flag.Int("fetch-par", 16, "concurrency during fetch (parallel block downloads)")
        keepLocal   = flag.Float64("keep-local", 0.0, "ignored here; present for parity with e2e")
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

    // Seed PRNG for test randomness
    mrand.Seed(time.Now().UnixNano())

    // Start cluster
    sims := startCluster(*nodes, *basePort, conf, *softPinTTL)
    defer stopCluster(sims)

    // Warmup, then bootstrap
    time.Sleep(*warmup)
    bootstrapCluster(sims)

    // Measure availability and latency over randomized upload/download trials
    fmt.Println("=== Configuration (local-only) ===")
    fmt.Printf("nodes=%d basePort=%d size=%d chunk=%d fanout=%d fetch-par=%d keep-local=%.2f (ignored)\n",
        *nodes, *basePort, *payloadSize, *chunkSize, *fanout, *fetchPar, *keepLocal)
    fmt.Printf("K=%d alpha=%d replicas=%d rpc=%s refresh=%s ttl=%s republish=%s gc=%s revalidate=%s maxValue=%d failThresh=%d softTTL=%s\n",
        conf.KBucketK, conf.Alpha, conf.Replicas, conf.RpcTimeout, conf.BucketRefresh, conf.RecordTTL,
        conf.RepublishInterval, conf.GCInterval, conf.RevalidateInterval, conf.MaxValueSize, conf.FailureThreshold, *softPinTTL)

    res := runTrials(sims, *trials, *fetchPar, conf, *payloadSize, *chunkSize, *fanout, *settle)
    printResults(res)
}

// startCluster spins up N nodes with in-memory blockstores and applies the provided config.
func startCluster(n int, basePort int, conf configuration.Config, softTTL time.Duration) []*simNode {
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
        nd.SetBlockProvider(mem)
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
