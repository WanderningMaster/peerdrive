package node

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"log"
	"net"
	"time"

	"github.com/WanderningMaster/peerdrive/internal/block"
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
	defaults := n.conf

	defer c.Close()
	dec := json.NewDecoder(bufio.NewReader(c))
	enc := json.NewEncoder(c)
	var m rpc.RpcMessage
	if err := dec.Decode(&m); err != nil {
		return
	}
	// Sanitize claimed sender address: keep claimed port, replace host with remote IP
	// to avoid poisoning while preserving listen port. If parsing fails, keep the claimed address.
	// if remoteHost, _, err := net.SplitHostPort(c.RemoteAddr().String()); err == nil {
	// 	if _, port, err2 := net.SplitHostPort(m.From.Addr); err2 == nil && port != "" {
	// 		m.From.Addr = net.JoinHostPort(remoteHost, port)
	// 	}
	// }
	n.rt.Update(m.From)

	switch m.Type {
	case rpc.Ping:
		_ = enc.Encode(rpc.RpcMessage{Type: rpc.Ping, From: n.Contact()})
	case rpc.Store:
		if m.Key == "" {
			return
		}

		// Ignore oversized
		if len(m.Value) > defaults.MaxValueSize {
			_ = enc.Encode(rpc.RpcMessage{Type: rpc.Store, From: n.Contact()})
			return
		}

		n.storeMu.Lock()
		n.store[m.Key] = kvRecord{Value: append([]byte(nil), m.Value...), Expires: time.Now().Add(defaults.RecordTTL), Origin: false}
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
		rec, ok := n.store[m.Key]
		n.storeMu.RUnlock()
		if ok {
			if time.Now().Before(rec.Expires) {
				_ = enc.Encode(rpc.RpcMessage{Type: rpc.FindValue, From: n.Contact(), Found: true, Value: rec.Value})
				return
			}

			n.storeMu.Lock()
			delete(n.store, m.Key)
			n.storeMu.Unlock()
		}
		nodes := n.rt.Closest(id.HashKey(m.Key), defaults.KBucketK)
		_ = enc.Encode(rpc.RpcMessage{Type: rpc.FindValue, From: n.Contact(), Found: false, Nodes: nodes})
	case rpc.FetchBlock:
		if m.Key == "" {
			return
		}
		cid, err := block.DecodeCID(m.Key)
		if err != nil {
			return
		}
		if n.blockProv == nil {
			_ = enc.Encode(rpc.RpcMessage{Type: rpc.FetchBlock, From: n.Contact(), Found: false})
			return
		}
		blk, err := n.blockProv.GetBlockLocal(context.TODO(), cid)
		if err != nil {
			return
		}
		if blk == nil {
			_ = enc.Encode(rpc.RpcMessage{Type: rpc.FetchBlock, From: n.Contact(), Found: false})
			return
		}
		err = blk.Serialize()
		if err != nil {
			return
		}
		_ = enc.Encode(rpc.RpcMessage{Type: rpc.FetchBlock, From: n.Contact(), Found: true, Value: blk.Bytes})
	}
}
