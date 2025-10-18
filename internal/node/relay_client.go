package node

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net"
	"time"

	"github.com/WanderningMaster/peerdrive/internal/block"
	"github.com/WanderningMaster/peerdrive/internal/id"
	"github.com/WanderningMaster/peerdrive/internal/relay"
	"github.com/WanderningMaster/peerdrive/internal/rpc"
)

// AttachRelay establishes a long-lived connection to an inbound relay server
// and serves incoming DeliverRequest frames over the attached stream.
func (n *Node) AttachRelay(ctx context.Context, relayAddr string) error {
	conn, err := net.Dial("tcp", relayAddr)
	if err != nil {
		return err
	}
	n.relayAddr = relayAddr
	enc := json.NewEncoder(conn)
	dec := json.NewDecoder(bufio.NewReader(conn))
	// Register
	if err := enc.Encode(relay.Frame{Type: relay.Register, TargetID: n.ID.String()}); err != nil {
		_ = conn.Close()
		return err
	}
	log.Printf("node %s attached to relay %s", n.ID.String()[:8], relayAddr)

	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	// Read frames and handle requests inline (no refactor to a shared process function)
	for {
		var f relay.Frame
		if err := dec.Decode(&f); err != nil {
			return err
		}
		if f.Type != relay.DeliverRequest {
			continue
		}
		// Handle RPC payload similarly to TCP server, inline switch
		var resp rpc.RpcMessage
		m := f.Payload
		// Update routing table with sender
		n.rt.Update(m.From)
		switch m.Type {
		case rpc.Ping:
			resp = rpc.RpcMessage{Type: rpc.Ping, From: n.Contact()}
		case rpc.Store:
			if m.Key == "" {
				// respond empty
				resp = rpc.RpcMessage{Type: rpc.Store, From: n.Contact()}
			} else if len(m.Value) > n.conf.MaxValueSize {
				resp = rpc.RpcMessage{Type: rpc.Store, From: n.Contact()}
			} else {
				n.storeMu.Lock()
				n.store[m.Key] = kvRecord{Value: append([]byte(nil), m.Value...), Expires: time.Now().Add(n.conf.RecordTTL), Origin: false}
				n.storeMu.Unlock()
				resp = rpc.RpcMessage{Type: rpc.Store, From: n.Contact()}
			}
		case rpc.FindNode:
			var target id.NodeID
			if b, err := hex.DecodeString(m.Key); err == nil && len(b) == len(target) {
				copy(target[:], b)
			}
			nodes := n.rt.Closest(target, n.conf.KBucketK)
			resp = rpc.RpcMessage{Type: rpc.FindNode, From: n.Contact(), Nodes: nodes}
		case rpc.FindValue:
			if m.Key == "" {
				resp = rpc.RpcMessage{Type: rpc.FindValue, From: n.Contact(), Found: false}
				break
			}
			n.storeMu.RLock()
			rec, ok := n.store[m.Key]
			n.storeMu.RUnlock()
			if ok {
				if time.Now().Before(rec.Expires) {
					resp = rpc.RpcMessage{Type: rpc.FindValue, From: n.Contact(), Found: true, Value: rec.Value}
				} else {
					n.storeMu.Lock()
					delete(n.store, m.Key)
					n.storeMu.Unlock()
					nodes := n.rt.Closest(id.HashKey(m.Key), n.conf.KBucketK)
					resp = rpc.RpcMessage{Type: rpc.FindValue, From: n.Contact(), Found: false, Nodes: nodes}
				}
			} else {
				nodes := n.rt.Closest(id.HashKey(m.Key), n.conf.KBucketK)
				resp = rpc.RpcMessage{Type: rpc.FindValue, From: n.Contact(), Found: false, Nodes: nodes}
			}
		case rpc.FetchBlock:
			if m.Key == "" || n.blockProv == nil {
				resp = rpc.RpcMessage{Type: rpc.FetchBlock, From: n.Contact(), Found: false}
				break
			}
			cid, err := block.DecodeCID(m.Key)
			if err != nil {
				resp = rpc.RpcMessage{Type: rpc.FetchBlock, From: n.Contact(), Found: false}
				break
			}
			blk, err := n.blockProv.GetBlockLocal(context.TODO(), cid)
			if err != nil || blk == nil {
				resp = rpc.RpcMessage{Type: rpc.FetchBlock, From: n.Contact(), Found: false}
				break
			}
			if err := blk.Serialize(); err != nil {
				resp = rpc.RpcMessage{Type: rpc.FetchBlock, From: n.Contact(), Found: false}
				break
			}
			resp = rpc.RpcMessage{Type: rpc.FetchBlock, From: n.Contact(), Found: true, Value: blk.Bytes}
		default:
			resp = rpc.RpcMessage{From: n.Contact()}
		}
		// Send DeliverResponse back
		if err := enc.Encode(relay.Frame{Type: relay.DeliverResponse, ReqID: f.ReqID, Payload: resp}); err != nil {
			return err
		}
	}
}

// DialRpcViaRelay performs a single RPC to a target reachable via a relay server.
func (n *Node) DialRpcViaRelay(ctx context.Context, relayAddr string, targetID string, req rpc.RpcMessage) (rpc.RpcMessage, error) {
	var zero rpc.RpcMessage
	conn, err := net.DialTimeout("tcp", relayAddr, n.conf.RpcTimeout)
	if err != nil {
		return zero, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(n.conf.RpcTimeout))
	enc := json.NewEncoder(conn)
	dec := json.NewDecoder(bufio.NewReader(conn))
	reqID := fmt.Sprintf("%x-%d", n.ID[:4], rand.Int63())
	if err := enc.Encode(relay.Frame{Type: relay.ClientRequest, ReqID: reqID, TargetID: targetID, Payload: req}); err != nil {
		return zero, err
	}
	var f relay.Frame
	if err := dec.Decode(&f); err != nil {
		return zero, err
	}
	if f.Type != relay.ClientResponse || f.ReqID != reqID {
		return zero, fmt.Errorf("unexpected relay response")
	}
	if f.Error != "" {
		return zero, fmt.Errorf(f.Error)
	}
	return f.Payload, nil
}

func (n *Node) WhoAmI(ctx context.Context, relayAddr string) (rpc.RpcMessage, error) {
	var zero rpc.RpcMessage
	conn, err := net.DialTimeout("tcp", relayAddr, n.conf.RpcTimeout)
	if err != nil {
		return zero, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(n.conf.RpcTimeout))
	enc := json.NewEncoder(conn)
	dec := json.NewDecoder(bufio.NewReader(conn))
	reqID := fmt.Sprintf("%x-%d", n.ID[:4], rand.Int63())
	if err := enc.Encode(relay.Frame{Type: relay.Whoami, ReqID: reqID}); err != nil {
		return zero, err
	}
	var f relay.Frame
	if err := dec.Decode(&f); err != nil {
		return zero, err
	}
	if f.Type != relay.Whoami || f.ReqID != reqID {
		return zero, fmt.Errorf("unexpected relay response")
	}
	if f.Error != "" {
		return zero, fmt.Errorf(f.Error)
	}
	return f.Payload, nil
}
