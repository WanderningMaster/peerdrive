package node

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"log"
	"net"

	"github.com/WanderningMaster/peerdrive/configuration"
	"github.com/WanderningMaster/peerdrive/internal/id"
	"github.com/WanderningMaster/peerdrive/internal/rpc"
)

func (n *Node) ListenAndServe(ctx context.Context) error {
	ln, err := net.Listen("tcp", n.Addr)
	if err != nil {
		return err
	}
	n.ln = ln
	go func() {
		<-ctx.Done()
		n.closing.Store(true)
		_ = n.ln.Close()
	}()
	log.Printf("node %s listening on %s", n.ID.String()[:8], n.Addr)
	for {
		c, err := ln.Accept()
		if err != nil {
			if n.closing.Load() {
				return nil
			}
			return err
		}
		go n.handleConn(c)
	}
}

func (n *Node) handleConn(c net.Conn) {
	defaults := configuration.Default()

	defer c.Close()
	dec := json.NewDecoder(bufio.NewReader(c))
	enc := json.NewEncoder(c)
	var m rpc.RpcMessage
	if err := dec.Decode(&m); err != nil {
		return
	}
	// Touch sender in routing table
	n.rt.Update(m.From)
	switch m.Type {
	case rpc.Ping:
		_ = enc.Encode(rpc.RpcMessage{Type: rpc.Ping, From: n.Contact()})
	case rpc.Store:
		if m.Key == "" {
			return
		}
		n.storeMu.Lock()
		n.store[m.Key] = append([]byte(nil), m.Value...)
		n.storeMu.Unlock()
		_ = enc.Encode(rpc.RpcMessage{Type: rpc.Store, From: n.Contact()})
	case rpc.FindNode:
		var target id.NodeID
		if b, err := hex.DecodeString(m.Key); err == nil && len(b) == len(target) {
			copy(target[:], b)
		}
		nodes := n.rt.Closest(target, defaults.KBucketK)
		_ = enc.Encode(rpc.RpcMessage{Type: rpc.FindNode, From: n.Contact(), Nodes: nodes})
	case rpc.FindValue:
		if m.Key == "" {
			return
		}
		n.storeMu.RLock()
		val, ok := n.store[m.Key]
		n.storeMu.RUnlock()
		if ok {
			_ = enc.Encode(rpc.RpcMessage{Type: rpc.FindValue, From: n.Contact(), Found: true, Value: val})
			return
		}
		nodes := n.rt.Closest(id.HashKey(m.Key), defaults.KBucketK)
		_ = enc.Encode(rpc.RpcMessage{Type: rpc.FindValue, From: n.Contact(), Found: false, Nodes: nodes})
	}
}
