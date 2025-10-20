package node

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"time"

	"github.com/WanderningMaster/peerdrive/internal/logging"
	"github.com/WanderningMaster/peerdrive/internal/relay"
	"github.com/WanderningMaster/peerdrive/internal/rpc"
)

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
	logging.Logf(ctx, "node %s attached to relay %s", n.ID.String()[:8], relayAddr)

	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	// Read frames and handle requests via shared handler
	for {
		var f relay.Frame
		if err := dec.Decode(&f); err != nil {
			return err
		}
		if f.Type != relay.DeliverRequest {
			continue
		}
		// Update routing table with sender
		m := f.Payload
		n.rt.Update(m.From)
		// Reuse common handler
		resp, _ := handleRequest(ctx, n, m)
		// Send DeliverResponse back
		if err := enc.Encode(relay.Frame{Type: relay.DeliverResponse, ReqID: f.ReqID, Payload: resp}); err != nil {
			return err
		}
	}
}

func (n *Node) DialRpcViaRelay(ctx context.Context, relayAddr string, targetID string, req rpc.RpcMessage) (rpc.RpcMessage, error) {
	ctx = logging.WithPrefix(ctx, logging.RelayClientPrefix)

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
