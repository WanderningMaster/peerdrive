package node

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"net"
	"strconv"
	"time"

	"github.com/WanderningMaster/peerdrive/internal/block"
	"github.com/WanderningMaster/peerdrive/internal/id"
	"github.com/WanderningMaster/peerdrive/internal/logging"
	"github.com/WanderningMaster/peerdrive/internal/rpc"
)

func (n *Node) ListenAndServe(ctx context.Context) error {
	ctx = logging.WithPrefix(ctx, logging.ServerPrefix)

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
	logging.Logf(ctx, "node %s listening on %s", n.ID.String()[:8], n.Addr)
	for {
		c, err := ln.Accept()
		if err != nil {
			if n.closing.Load() {
				return nil
			}
			return err
		}
		go n.handleConn(ctx, c)
	}
}

func (n *Node) handleConn(ctx context.Context, c net.Conn) {
	defer c.Close()
	dec := json.NewDecoder(bufio.NewReader(c))
	enc := json.NewEncoder(c)
	var m rpc.RpcMessage
	if err := dec.Decode(&m); err != nil {
		logging.Logf(ctx, "decode error from %s: %v", c.RemoteAddr().String(), err)
		return
	}
	// Sanitize claimed sender address: keep claimed port, replace host with remote IP
	// to avoid poisoning while preserving listen port. If parsing fails, keep the claimed address.
	if remoteHost, _, err := net.SplitHostPort(c.RemoteAddr().String()); err == nil {
		if _, port, err2 := net.SplitHostPort(m.From.Addr); err2 == nil && port != "" {
			m.From.Addr = net.JoinHostPort(remoteHost, port)
		}
	}
	n.rt.Update(m.From)

	logging.Logf(ctx, "<- %s from %s@%s key=%s size=%d", m.Type, m.From.ID.String()[:8], m.From.Addr, m.Key, len(m.Value))

	resp, _ := handleRequest(n, m)
	_ = enc.Encode(resp)

	logging.Logf(ctx, "-> %s to %s", resp.Type, c.RemoteAddr().String())
}

func handleRequest(n *Node, m rpc.RpcMessage) (rpc.RpcMessage, string) {
	conf := n.conf

	switch m.Type {
	case rpc.Ping:
		resp := rpc.RpcMessage{Type: rpc.Ping, From: n.Contact()}
		return resp, ""

	case rpc.Store:
		if m.Key == "" {
			return rpc.RpcMessage{Type: rpc.Store, From: n.Contact()}, ""
		}
		if len(m.Value) > conf.MaxValueSize {
			return rpc.RpcMessage{Type: rpc.Store, From: n.Contact()}, ""
		}
		n.storeMu.Lock()
		n.store[m.Key] = kvRecord{Value: append([]byte(nil), m.Value...), Expires: time.Now().Add(conf.RecordTTL), Origin: false}
		n.storeMu.Unlock()
		return rpc.RpcMessage{Type: rpc.Store, From: n.Contact()}, "key=" + m.Key

	case rpc.FindNode:
		var target id.NodeID
		if b, err := hex.DecodeString(m.Key); err == nil && len(b) == len(target) {
			copy(target[:], b)
		}
		nodes := n.rt.Closest(target, conf.KBucketK)
		return rpc.RpcMessage{Type: rpc.FindNode, From: n.Contact(), Nodes: nodes}, "nodes=" + strconv.Itoa(len(nodes))

	case rpc.FindValue:
		if m.Key == "" {
			nodes := n.rt.Closest(id.HashKey(m.Key), conf.KBucketK)
			return rpc.RpcMessage{Type: rpc.FindValue, From: n.Contact(), Found: false, Nodes: nodes}, ""
		}
		n.storeMu.RLock()
		rec, ok := n.store[m.Key]
		n.storeMu.RUnlock()
		if ok {
			if time.Now().Before(rec.Expires) {
				return rpc.RpcMessage{Type: rpc.FindValue, From: n.Contact(), Found: true, Value: rec.Value}, "key=" + m.Key
			}
			n.storeMu.Lock()
			delete(n.store, m.Key)
			n.storeMu.Unlock()
		}
		nodes := n.rt.Closest(id.HashKey(m.Key), conf.KBucketK)
		return rpc.RpcMessage{Type: rpc.FindValue, From: n.Contact(), Found: false, Nodes: nodes}, "key=" + m.Key

	case rpc.FetchBlock:
		if m.Key == "" || n.blockProv == nil {
			return rpc.RpcMessage{Type: rpc.FetchBlock, From: n.Contact(), Found: false}, ""
		}
		cid, err := block.DecodeCID(m.Key)
		if err != nil {
			return rpc.RpcMessage{Type: rpc.FetchBlock, From: n.Contact(), Found: false}, ""
		}
		blk, err := n.blockProv.GetBlockLocal(context.TODO(), cid)
		if err != nil || blk == nil {
			return rpc.RpcMessage{Type: rpc.FetchBlock, From: n.Contact(), Found: false}, ""
		}
		if err := blk.Serialize(); err != nil {
			return rpc.RpcMessage{Type: rpc.FetchBlock, From: n.Contact(), Found: false}, ""
		}
		return rpc.RpcMessage{Type: rpc.FetchBlock, From: n.Contact(), Found: true, Value: blk.Bytes}, "key=" + m.Key
	}
	// Default fallthrough
	return rpc.RpcMessage{From: n.Contact()}, ""
}
