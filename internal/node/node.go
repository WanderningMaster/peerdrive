package node

import (
	"context"
	"net"
	"sync"
	"sync/atomic"

	"github.com/WanderningMaster/peerdrive/internal/block"
	"github.com/WanderningMaster/peerdrive/internal/id"
	"github.com/WanderningMaster/peerdrive/internal/routing"
)

type Node struct {
	ID   id.NodeID
	Addr string

	rt        *routing.RoutingTable
	storeMu   sync.RWMutex
	store     map[string][]byte
	blockProv BlockProvider

	ln      net.Listener
	closing atomic.Bool
}

// BlockProvider supplies local blocks to serve via RPC.
// Any type with GetBlock(ctx, cid) (*block.Block, error) satisfies it.
type BlockProvider interface {
	GetBlockLocal(ctx context.Context, c block.CID) (*block.Block, error)
}

func NewNode(addr string) *Node {
	id := id.RandomID()
	n := &Node{
		ID:    id,
		Addr:  addr,
		rt:    routing.NewRoutingTable(id),
		store: make(map[string][]byte),
	}
	return n
}
func NewNodeWithId(addr string, id id.NodeID) *Node {
	n := &Node{
		ID:    id,
		Addr:  addr,
		rt:    routing.NewRoutingTable(id),
		store: make(map[string][]byte),
	}
	return n
}

func (n *Node) Contact() routing.Contact { return routing.Contact{ID: n.ID, Addr: n.Addr} }

// SetBlockProvider wires a local block provider for serving FetchBlock RPCs.
func (n *Node) SetBlockProvider(p BlockProvider) { n.blockProv = p }
