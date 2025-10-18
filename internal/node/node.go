package node

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/WanderningMaster/peerdrive/configuration"
	"github.com/WanderningMaster/peerdrive/internal/block"
	"github.com/WanderningMaster/peerdrive/internal/id"
	"github.com/WanderningMaster/peerdrive/internal/routing"
)

type Node struct {
	ID             id.NodeID
	Addr           string
	AdvertisedAddr string

	rt        *routing.RoutingTable
	storeMu   sync.RWMutex
	store     map[string]kvRecord
	blockProv BlockProvider

	ln      net.Listener
	closing atomic.Bool

	failMu    sync.Mutex
	FailCount map[string]int // key: id@addr

	conf configuration.Config

	needRelay bool

	// relay attachment state
	relayAddr string
}

// BlockProvider supplies local blocks to serve via RPC.
// Any type with GetBlock(ctx, cid) (*block.Block, error) satisfies it.
type BlockProvider interface {
	GetBlockLocal(ctx context.Context, c block.CID) (*block.Block, error)
}

type kvRecord struct {
	Value   []byte
	Expires time.Time
	Origin  bool
}

func NewNode(addr string) *Node {
	id := id.RandomID()
	n := &Node{
		ID:        id,
		Addr:      addr,
		rt:        routing.NewRoutingTable(id),
		store:     make(map[string]kvRecord),
		FailCount: make(map[string]int),
		conf:      configuration.Default(),
	}
	return n
}
func NewNodeWithId(addr string, id id.NodeID) *Node {
	n := &Node{
		ID:        id,
		Addr:      addr,
		rt:        routing.NewRoutingTable(id),
		store:     make(map[string]kvRecord),
		FailCount: make(map[string]int),
		conf:      configuration.Default(),
	}
	return n
}
func (n *Node) SetBlockProvider(p BlockProvider) { n.blockProv = p }
func (n *Node) SetAdvertisedAddr(addr string)    { n.AdvertisedAddr = addr }

func (n *Node) Contact() routing.Contact {
	return routing.Contact{ID: n.ID, Addr: n.advertisedAddr(), Relay: n.relayAddr}
}

func (n *Node) advertisedAddr() string {
	addr := n.Addr
	if n.AdvertisedAddr != "" {
		addr = n.AdvertisedAddr
	}

	return addr
}

func (n *Node) StartMaintenance(ctx context.Context) {
	go n.gcLoop(ctx)
	go n.republishLoop(ctx)
	go n.refreshLoop(ctx)
	go n.revalidateLoop(ctx)
}

func (n *Node) WithConfig(conf configuration.Config) *Node {
	n.conf = conf
	return n
}

func (n *Node) gcLoop(ctx context.Context) {
	t := time.NewTicker(n.conf.GCInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			now := time.Now()
			n.storeMu.Lock()
			for k, rec := range n.store {
				if now.After(rec.Expires) {
					delete(n.store, k)
				}
			}
			n.storeMu.Unlock()
		}
	}
}

func (n *Node) republishLoop(ctx context.Context) {
	t := time.NewTicker(n.conf.RepublishInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			now := time.Now()
			n.storeMu.RLock()
			type item struct {
				key     string
				val     []byte
				expires time.Time
				origin  bool
			}
			items := []item{}
			for k, rec := range n.store {
				if rec.Origin {
					items = append(items, item{key: k, val: append([]byte(nil), rec.Value...), expires: rec.Expires, origin: rec.Origin})
				}
			}
			n.storeMu.RUnlock()
			for _, it := range items {
				// Republish when the remaining TTL is less than or equal to the republish interval
				remaining := it.expires.Sub(now)
				if it.origin && remaining <= n.conf.RepublishInterval {
					_ = n.Store(ctx, it.key, it.val)
				}
			}
		}
	}
}

func (n *Node) refreshLoop(ctx context.Context) {
	t := time.NewTicker(n.conf.BucketRefresh)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			target := id.RandomID()
			_ = n.IterativeFindNode(ctx, target, n.conf.KBucketK)
		}
	}
}

func (n *Node) revalidateLoop(ctx context.Context) {
	t := time.NewTicker(n.conf.RevalidateInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			target := id.RandomID()
			sample := n.rt.Closest(target, n.conf.Alpha)
			for _, c := range sample {
				if err := n.Ping(ctx, c.Addr); err != nil {
					n.onRpcFailure(c)
				}
			}
		}
	}
}

func (n *Node) onRpcFailure(c routing.Contact) {
	key := c.ID.String() + "@" + c.Addr
	n.failMu.Lock()
	n.FailCount[key] = n.FailCount[key] + 2
	count := n.FailCount[key]
	n.failMu.Unlock()
	if count >= n.conf.FailureThreshold {
		_ = n.rt.Remove(c)
		n.failMu.Lock()
		delete(n.FailCount, key)
		n.failMu.Unlock()
	}
}

func (n *Node) onRpcSuccess(c routing.Contact) {
	key := c.ID.String() + "@" + c.Addr
	n.failMu.Lock()
	delete(n.FailCount, key)
	n.failMu.Unlock()
}
