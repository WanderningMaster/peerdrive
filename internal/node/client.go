package node

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net"
	"sort"
	"sync"
	"time"

	"github.com/WanderningMaster/peerdrive/configuration"
	"github.com/WanderningMaster/peerdrive/internal/id"
	"github.com/WanderningMaster/peerdrive/internal/routing"
	"github.com/WanderningMaster/peerdrive/internal/rpc"
)

func (n *Node) DialRpc(ctx context.Context, addr string, req rpc.RpcMessage) (rpc.RpcMessage, error) {
	defaults := configuration.Default()

	var zero rpc.RpcMessage
	ctx, cancel := context.WithTimeout(ctx, defaults.RpcTimeout)
	defer cancel()
	conn, err := net.DialTimeout("tcp", addr, defaults.RpcTimeout)
	if err != nil {
		return zero, err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(defaults.RpcTimeout))
	enc := json.NewEncoder(conn)
	dec := json.NewDecoder(bufio.NewReader(conn))
	if err := enc.Encode(req); err != nil {
		return zero, err
	}
	if err := dec.Decode(&zero); err != nil {
		return zero, err
	}
	return zero, nil
}

func (n *Node) Ping(ctx context.Context, addr string) error {
	_, err := n.DialRpc(ctx, addr, rpc.RpcMessage{Type: rpc.Ping, From: n.Contact()})
	if err == nil {
		// add to RT as live contact (synthetic ID unknown; ask peer for ID via response.From)
		// We sent our From, peer's response includes their From;
		// the DialRpc already decoded it into msg.From
	}
	return err
}

// Bootstrap: try to Ping known peers to seed the routing table.
func (n *Node) Bootstrap(ctx context.Context, peers []string) {
	for _, p := range peers {
		m, err := n.DialRpc(ctx, p, rpc.RpcMessage{Type: rpc.Ping, From: n.Contact()})
		if err != nil {
			continue
		}
		n.rt.Update(m.From)
	}
}

// Store replicates a key/value to the replicas closest to key.
func (n *Node) Store(ctx context.Context, key string, value []byte) error {
	defaults := configuration.Default()

	// Put locally first
	n.storeMu.Lock()
	n.store[key] = append([]byte(nil), value...)
	n.storeMu.Unlock()

	// Find k closest peers
	peers := n.iterativeFindNode(ctx, id.HashKey(key), defaults.KBucketK)
	// Send to up to "replicas" peers
	for i := 0; i < len(peers) && i < defaults.Replicas; i++ {
		_, err := n.DialRpc(ctx, peers[i].Addr, rpc.RpcMessage{Type: rpc.Store, From: n.Contact(), Key: key, Value: value})
		if err != nil {
			continue
		}
	}
	return nil
}

// Get performs an iterative lookup; if value is found at any peer, returns it.
func (n *Node) Get(ctx context.Context, key string) ([]byte, error) {
	defaults := configuration.Default()

	// Check local
	n.storeMu.RLock()
	if v, ok := n.store[key]; ok {
		n.storeMu.RUnlock()
		return append([]byte(nil), v...), nil
	}
	n.storeMu.RUnlock()
	// Ask peers
	visited := make(map[string]bool)
	target := id.HashKey(key)
	cands := n.rt.Closest(target, defaults.Alpha)
	for len(cands) > 0 {
		next := cands
		if len(next) > defaults.Alpha {
			next = next[:defaults.Alpha]
		}
		var mu sync.Mutex
		var found []byte
		var wg sync.WaitGroup
		for _, peer := range next {
			if visited[peer.Addr] {
				continue
			}
			visited[peer.Addr] = true
			wg.Add(1)
			go func(p routing.Contact) {
				defer wg.Done()
				m, err := n.DialRpc(ctx, p.Addr, rpc.RpcMessage{Type: rpc.FindValue, From: n.Contact(), Key: key})
				if err != nil {
					return
				}
				n.rt.Update(m.From)
				if m.Found {
					mu.Lock()
					if found == nil {
						found = append([]byte(nil), m.Value...)
					}
					mu.Unlock()
					return
				}
				mu.Lock()
				cands = append(cands, m.Nodes...)
				mu.Unlock()
			}(peer)
		}
		wg.Wait()
		if found != nil {
			return found, nil
		}
		// Deduplicate and sort cands by distance
		cands = uniqAndSortByDist(cands, target, visited)
		// Trim to kBucketK to bound growth
		if len(cands) > defaults.KBucketK {
			cands = cands[:defaults.KBucketK]
		}
	}
	return nil, errors.New("not found")
}

func (n *Node) iterativeFindNode(ctx context.Context, target id.NodeID, want int) []routing.Contact {
	defaults := configuration.Default()

	visited := make(map[string]bool)
	shortlist := n.rt.Closest(target, defaults.KBucketK)
	shortlist = uniqAndSortByDist(shortlist, target, visited)

	for {
		progress := false
		batch := shortlist
		if len(batch) > defaults.Alpha {
			batch = batch[:defaults.Alpha]
		}
		var mu sync.Mutex
		var wg sync.WaitGroup
		for _, peer := range batch {
			if visited[peer.Addr] {
				continue
			}
			visited[peer.Addr] = true
			wg.Add(1)
			go func(p routing.Contact) {
				defer wg.Done()
				m, err := n.DialRpc(ctx, p.Addr, rpc.RpcMessage{Type: rpc.FindNode, From: n.Contact(), Key: target.String()})
				if err != nil {
					return
				}
				n.rt.Update(m.From)
				mu.Lock()
				oldBest := bestDist(shortlist, target)
				shortlist = append(shortlist, m.Nodes...)
				shortlist = uniqAndSortByDist(shortlist, target, visited)
				newBest := bestDist(shortlist, target)
				if newBest.Cmp(oldBest) < 0 {
					progress = true
				}
				mu.Unlock()
			}(peer)
		}
		wg.Wait()
		if !progress {
			break
		}
		if len(shortlist) >= want {
			break
		}
	}
	if len(shortlist) > want {
		shortlist = shortlist[:want]
	}
	return shortlist
}

func bestDist(cs []routing.Contact, target id.NodeID) *big.Int {
	defaults := configuration.Default()

	if len(cs) == 0 {
		return big.NewInt(1).Lsh(big.NewInt(1), uint(defaults.IdBits))
	}
	best := id.XorDist(cs[0].ID, target)
	for _, c := range cs[1:] {
		d := id.XorDist(c.ID, target)
		if d.Cmp(best) < 0 {
			best = d
		}
	}
	return best
}

func uniqAndSortByDist(cs []routing.Contact, target id.NodeID, visited map[string]bool) []routing.Contact {
	m := map[string]routing.Contact{}
	for _, c := range cs {
		if c.ID == (id.NodeID{}) {
			continue
		}
		k := c.ID.String() + "@" + c.Addr
		m[k] = c
	}
	out := make([]routing.Contact, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		return id.XorDist(out[i].ID, target).Cmp(id.XorDist(out[j].ID, target)) < 0
	})
	// Remove already visited addresses from the front
	i := 0
	for i < len(out) && visited[out[i].Addr] {
		i++
	}
	return out[i:]
}
