package relay

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync"

	"github.com/WanderningMaster/peerdrive/internal/logging"
	"github.com/WanderningMaster/peerdrive/internal/rpc"
)

// Server implements a simple inbound relay that maintains long-lived
// registrations from nodes and forwards client requests to them over
// that attached stream, correlating responses by ReqID.
type Server struct {
	ln net.Listener

	// Attached nodes by hex node ID string
	muAttached sync.RWMutex
	attached   map[string]*attachedConn

	// Pending client responses keyed by reqId
	muPending sync.Mutex
	pending   map[string]*clientWaiter
}

type attachedConn struct {
	id     string
	conn   net.Conn
	enc    *json.Encoder
	writeM sync.Mutex
}

type clientWaiter struct {
	enc *json.Encoder
	c   net.Conn
}

func NewServer() *Server {
	return &Server{
		attached: make(map[string]*attachedConn),
		pending:  make(map[string]*clientWaiter),
	}
}

func (s *Server) ListenAndServe(addr string) error {
	ctx := logging.WithPrefix(context.Background(), "relay")
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.ln = ln

	logging.Logf(ctx, "listening on %s", addr)

	for {
		c, err := ln.Accept()
		if err != nil {
			return err
		}
		go s.handleConn(c)
	}
}

func (s *Server) handleConn(c net.Conn) {
	dec := json.NewDecoder(bufio.NewReader(c))
	enc := json.NewEncoder(c)

	var f Frame
	if err := dec.Decode(&f); err != nil {
		_ = c.Close()
		return
	}
	switch f.Type {
	case Register:
		s.handleAttach(c, dec, enc, f)
	case ClientRequest:
		s.handleClient(c, dec, enc, f)
	case Whoami:
		s.handlWhoami(c, dec, enc, f)
	default:
		_ = c.Close()
		return
	}
}

func (s *Server) handlWhoami(c net.Conn, dec *json.Decoder, enc *json.Encoder, first Frame) {
	remoteHost, _, err := net.SplitHostPort(c.RemoteAddr().String())
	if err != nil {
		_ = enc.Encode(Frame{Type: Whoami, ReqID: first.ReqID, Error: "whoami failed"})
		return
	}

	p := rpc.RpcMessage{
		Value: []byte(remoteHost),
	}
	_ = enc.Encode(Frame{Type: Whoami, ReqID: first.ReqID, Payload: p})
}

func (s *Server) handleAttach(c net.Conn, dec *json.Decoder, enc *json.Encoder, first Frame) {
	ctx := logging.WithPrefix(context.Background(), "relay")

	if first.TargetID == "" {
		_ = c.Close()
		return
	}
	a := &attachedConn{id: first.TargetID, conn: c, enc: enc}
	s.muAttached.Lock()
	if old, ok := s.attached[first.TargetID]; ok {
		_ = old.conn.Close()
	}
	s.attached[first.TargetID] = a
	s.muAttached.Unlock()
	logging.Logf(ctx, "attached node %s", first.TargetID[:8])

	defer func() {
		s.muAttached.Lock()
		if cur, ok := s.attached[first.TargetID]; ok && cur.conn == c {
			delete(s.attached, first.TargetID)
		}
		s.muAttached.Unlock()
		_ = c.Close()
		logging.Logf(ctx, "relay: detached node %s", first.TargetID[:8])
	}()

	for {
		var f Frame
		if err := dec.Decode(&f); err != nil {
			return
		}
		if f.Type != DeliverResponse {
			continue
		}
		s.muPending.Lock()
		waiter, ok := s.pending[f.ReqID]
		if ok {
			delete(s.pending, f.ReqID)
		}
		s.muPending.Unlock()
		if !ok {
			continue
		}
		_ = waiter.enc.Encode(Frame{Type: ClientResponse, ReqID: f.ReqID, Payload: f.Payload})
		_ = waiter.c.Close()
	}
}

func (s *Server) handleClient(c net.Conn, dec *json.Decoder, enc *json.Encoder, first Frame) {
	if first.TargetID == "" || first.ReqID == "" {
		_ = enc.Encode(Frame{Type: ClientResponse, ReqID: first.ReqID, Error: "bad request"})
		_ = c.Close()
		return
	}
	a, err := s.getAttached(first.TargetID)
	if err != nil {
		_ = enc.Encode(Frame{Type: ClientResponse, ReqID: first.ReqID, Error: "target not attached"})
		_ = c.Close()
		return
	}

	s.muPending.Lock()
	s.pending[first.ReqID] = &clientWaiter{enc: enc, c: c}
	s.muPending.Unlock()

	// Forward to attached node
	a.writeM.Lock()
	err = a.enc.Encode(Frame{Type: DeliverRequest, ReqID: first.ReqID, Payload: first.Payload})
	a.writeM.Unlock()
	if err != nil {
		s.muPending.Lock()
		delete(s.pending, first.ReqID)
		s.muPending.Unlock()
		_ = enc.Encode(Frame{Type: ClientResponse, ReqID: first.ReqID, Error: "forward failed"})
		_ = c.Close()
		return
	}

	// Keep connection open for the response; read and ignore any extra frames from client
	// If client disconnects early, we'll still deliver the response but write will fail.
	var dummy Frame
	for dec.Decode(&dummy) == nil {
		// ignore
	}
}

func (s *Server) getAttached(id string) (*attachedConn, error) {
	s.muAttached.RLock()
	a, ok := s.attached[id]
	s.muAttached.RUnlock()
	if !ok {
		return nil, errors.New("not attached")
	}
	return a, nil
}
