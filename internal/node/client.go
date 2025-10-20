package node

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net"
	"sort"
	"sync"
	"time"

	"github.com/WanderningMaster/peerdrive/internal/block"
	"github.com/WanderningMaster/peerdrive/internal/id"
	"github.com/WanderningMaster/peerdrive/internal/logging"
	"github.com/WanderningMaster/peerdrive/internal/routing"
	"github.com/WanderningMaster/peerdrive/internal/rpc"
)

func (n *Node) DialRpc(ctx context.Context, c routing.Contact, req rpc.RpcMessage) (rpc.RpcMessage, error) {
	if c.Relay != "" {
		return n.DialRpcViaRelay(ctx, c.Relay, c.ID.String(), req)
	}
	return n._dialRpc(ctx, c.Addr, req)
}

func (n *Node) _dialRpc(ctx context.Context, addr string, req rpc.RpcMessage) (rpc.RpcMessage, error) {
	ctx = logging.WithPrefix(ctx, logging.ClientPrefix)

	var zero rpc.RpcMessage
	ctx, cancel := context.WithTimeout(ctx, n.conf.RpcTimeout)
	defer cancel()
	conn, err := net.DialTimeout("tcp", addr, n.conf.RpcTimeout)
	if err != nil {
		return zero, err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(n.conf.RpcTimeout))
	enc := json.NewEncoder(conn)
	dec := json.NewDecoder(bufio.NewReader(conn))

	logging.Logf(ctx, "-> %s to %s key=%s size=%d", req.Type, addr, req.Key, len(req.Value))
	if err := enc.Encode(req); err != nil {
		return zero, err
	}
	if err := dec.Decode(&zero); err != nil {
		return zero, err
	}

	logging.Logf(ctx, "<- %s from %s found=%v nodes=%d size=%d", zero.Type, zero.From.Addr, zero.Found, len(zero.Nodes), len(zero.Value))
	return zero, nil
}

func (n *Node) Ping(ctx context.Context, addr string) error {
	m, err := n.DialRpc(ctx, routing.Contact{Addr: addr}, rpc.RpcMessage{Type: rpc.Ping, From: n.Contact()})
	if err == nil {
		n.rt.Update(m.From)
		n.onRpcSuccess(m.From)
	}
	return err
}

func (n *Node) Bootstrap(ctx context.Context, peers []string) {
	for _, p := range peers {
		m, err := n.DialRpc(ctx, routing.Contact{Addr: p}, rpc.RpcMessage{Type: rpc.Ping, From: n.Contact()})
		if err != nil {
			continue
		}
		n.rt.Update(m.From)
	}
}

func (n *Node) Store(ctx context.Context, key string, value []byte) error {
	// Put locally first
	n.storeMu.Lock()
	n.store[key] = kvRecord{Value: append([]byte(nil), value...), Expires: time.Now().Add(n.conf.RecordTTL), Origin: true}
	n.storeMu.Unlock()

	// Find k closest peers
	peers := n.IterativeFindNode(ctx, id.HashKey(key), n.conf.KBucketK)

	// Send to up to "replicas" peers
	for i := 0; i < len(peers) && i < n.conf.Replicas; i++ {
		_, err := n.DialRpc(ctx, peers[i], rpc.RpcMessage{Type: rpc.Store, From: n.Contact(), Key: key, Value: value})
		if err != nil {
			n.onRpcFailure(peers[i])
			continue
		}
		n.onRpcSuccess(peers[i])
	}
	return nil
}

func (n *Node) Get(ctx context.Context, key string) ([]byte, error) {
	// Check local
	n.storeMu.RLock()
	if rec, ok := n.store[key]; ok {
		if time.Now().Before(rec.Expires) {
			v := append([]byte(nil), rec.Value...)
			n.storeMu.RUnlock()
			return v, nil
		}
	}
	n.storeMu.RUnlock()
	// Ask peers
	visited := make(map[string]bool)
	target := id.HashKey(key)
	cands := n.rt.Closest(target, n.conf.KBucketK)
	for len(cands) > 0 {
		next := cands
		if len(next) > n.conf.Alpha {
			next = next[:n.conf.Alpha]
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
				m, err := n.DialRpc(ctx, p, rpc.RpcMessage{Type: rpc.FindValue, From: n.Contact(), Key: key})
				if err != nil {
					n.onRpcFailure(p)
					return
				}
				n.rt.Update(m.From)
				n.onRpcSuccess(m.From)
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
			n.storeMu.Lock()
			n.store[key] = kvRecord{Value: append([]byte(nil), found...), Expires: time.Now().Add(n.conf.RecordTTL), Origin: false}
			n.storeMu.Unlock()
			return found, nil
		}
		cands = uniqAndSortByDist(cands, target, visited)
		if len(cands) > n.conf.KBucketK {
			cands = cands[:n.conf.KBucketK]
		}
	}
	return nil, errors.New("not found")
}

func (n *Node) GetClosest(ctx context.Context, key string) ([][]byte, error) {
	founds := [][]byte{}
	n.storeMu.RLock()
	if rec, ok := n.store[key]; ok {
		if time.Now().Before(rec.Expires) {
			v := append([]byte(nil), rec.Value...)
			founds = append(founds, v)
		}
	}
	n.storeMu.RUnlock()
	// Ask peers
	visited := make(map[string]bool)
	target := id.HashKey(key)
	cands := n.rt.Closest(target, n.conf.Alpha)

	for len(cands) > 0 {
		next := cands
		if len(next) > n.conf.Alpha {
			next = next[:n.conf.Alpha]
		}
		var mu sync.Mutex
		var batchFounds [][]byte
		var wg sync.WaitGroup
		for _, peer := range next {
			if visited[peer.Addr] {
				continue
			}
			visited[peer.Addr] = true
			wg.Add(1)
			go func(p routing.Contact) {
				defer wg.Done()
				m, err := n.DialRpc(ctx, p, rpc.RpcMessage{Type: rpc.FindValue, From: n.Contact(), Key: key})
				if err != nil {
					n.onRpcFailure(p)
					return
				}
				n.rt.Update(m.From)
				n.onRpcSuccess(m.From)
				if m.Found {
					mu.Lock()
					batchFounds = append(batchFounds, append([]byte(nil), m.Value...))
					mu.Unlock()
					return
				}
				mu.Lock()
				cands = append(cands, m.Nodes...)
				mu.Unlock()
			}(peer)
		}
		wg.Wait()
		if len(batchFounds) > 0 {
			n.storeMu.Lock()
			n.store[key] = kvRecord{Value: append([]byte(nil), batchFounds[0]...), Expires: time.Now().Add(n.conf.RecordTTL), Origin: false}
			n.storeMu.Unlock()

			founds = append(founds, batchFounds...)
		}
		cands = uniqAndSortByDist(cands, target, visited)
		if len(cands) > n.conf.KBucketK {
			cands = cands[:n.conf.KBucketK]
		}
	}
	if len(founds) == 0 {
		return nil, errors.New("not found")
	}
	return founds, nil
}

func (n *Node) IterativeFindNode(ctx context.Context, target id.NodeID, want int) []routing.Contact {
	visited := make(map[string]bool)
	shortlist := n.rt.Closest(target, n.conf.KBucketK)
	shortlist = uniqAndSortByDist(shortlist, target, visited)

	for {
		progress := false
		batch := shortlist
		if len(batch) > n.conf.Alpha {
			batch = batch[:n.conf.Alpha]
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
				m, err := n.DialRpc(ctx, p, rpc.RpcMessage{Type: rpc.FindNode, From: n.Contact(), Key: target.String()})
				if err != nil {
					n.onRpcFailure(p)
					return
				}
				n.rt.Update(m.From)
				n.onRpcSuccess(m.From)
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

func (n *Node) FetchBlock(ctx context.Context, c routing.Contact, cid block.CID) ([]byte, error) {
	key, err := cid.Encode()
	if err != nil {
		return nil, err
	}
	m, err := n.DialRpc(ctx, c, rpc.RpcMessage{Type: rpc.FetchBlock, From: n.Contact(), Key: key})
	if err != nil {
		return nil, err
	}
	if !m.Found {
		return nil, fmt.Errorf("Block not found")
	}

	return m.Value, nil
}

func bestDist(cs []routing.Contact, target id.NodeID) *big.Int {
	if len(cs) == 0 {
		return new(big.Int).Lsh(big.NewInt(1), 256)
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
	i := 0
	for i < len(out) && visited[out[i].Addr] {
		i++
	}
	return out[i:]
}
