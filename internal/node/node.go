package node

import (
	"net"
	"sync"
	"sync/atomic"

	"github.com/WanderningMaster/peerdrive/internal/id"
	"github.com/WanderningMaster/peerdrive/internal/routing"
)

type Node struct {
	ID   id.NodeID
	Addr string

	rt      *routing.RoutingTable
	storeMu sync.RWMutex
	store   map[string][]byte

	ln      net.Listener
	closing atomic.Bool
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

func (n *Node) Contact() routing.Contact { return routing.Contact{ID: n.ID, Addr: n.Addr} }
