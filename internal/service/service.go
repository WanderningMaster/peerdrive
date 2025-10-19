package service

import (
	"context"
	"log"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/WanderningMaster/peerdrive/configuration"
	"github.com/WanderningMaster/peerdrive/internal/block"
	blockfetcher "github.com/WanderningMaster/peerdrive/internal/block-fetcher"
	"github.com/WanderningMaster/peerdrive/internal/dag"
	"github.com/WanderningMaster/peerdrive/internal/id"
	"github.com/WanderningMaster/peerdrive/internal/node"
	"github.com/WanderningMaster/peerdrive/internal/storage"
)

type Service struct {
	n       *node.Node
	store   storage.Store
	builder dag.DagBuilder
	conf    *configuration.UserConfig
}

func New(n *node.Node, conf *configuration.UserConfig, useMemStore bool) *Service {
	fetcher := blockfetcher.New(n)
	var blockstore storage.Store
	if useMemStore {
		blockstore = storage.NewMemStore(storage.WithFetcher(fetcher))
	} else {
		blockstore, _ = storage.NewDiskStore(conf.BlockstorePath, storage.DiskWithFetcher(fetcher))
	}
	n.SetBlockProvider(blockstore)

	builder := dag.DagBuilder{
		ChunkSize: 1 << 20,
		Fanout:    256,
		Codec:     "cbor",
		Store:     blockstore,
	}

	return &Service{n: n, store: blockstore, builder: builder, conf: conf}
}

func (s *Service) Node() *node.Node { return s.n }

func (s *Service) ID() string    { return s.n.ID.String() }
func (s *Service) Addr() string  { return s.n.Contact().Addr }
func (s *Service) Relay() string { return s.n.Contact().Relay }

func (s *Service) Put(ctx context.Context, key string, val []byte) error {
	return s.n.Store(ctx, key, val)
}
func (s *Service) Get(ctx context.Context, key string) ([]byte, error) { return s.n.Get(ctx, key) }

func (s *Service) AddFromPath(ctx context.Context, inPath string) (string, error) {
	f, err := os.Open(inPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	_, cid, err := s.builder.BuildFromReader(ctx, "example.txt", f)
	if err != nil {
		return "", err
	}
	return cid.Encode()
}

func (s *Service) Fetch(ctx context.Context, cid block.CID) ([]byte, error) {
	return dag.Fetch(ctx, s.store, cid)
}

func (s *Service) Pin(ctx context.Context, cid block.CID) error      { return s.store.Pin(ctx, cid) }
func (s *Service) Unpin(ctx context.Context, cid block.CID) error    { return s.store.Unpin(ctx, cid) }
func (s *Service) ListPins(ctx context.Context) ([]block.CID, error) { return s.store.ListPins(ctx) }

func (s *Service) Closest(target id.NodeID, k int) []any {
	contacts := s.n.ClosestContacts(target, k)
	out := make([]any, 0, len(contacts))
	type outContact struct{ ID, Addr, Relay string }
	for _, c := range contacts {
		out = append(out, outContact{ID: c.ID.String(), Addr: c.Addr, Relay: c.Relay})
	}
	return out
}

func (s *Service) Bootstrap(ctx context.Context, peers []string) { s.n.Bootstrap(ctx, peers) }

func (s *Service) StartNode(ctx context.Context) {
	go func() {
		if err := s.n.ListenAndServe(ctx); err != nil {
			log.Printf("node server exited: %v", err)
		}
	}()
}

func (s *Service) AttachRelay(ctx context.Context, addr string) <-chan error {
	ch := make(chan error, 1)
	if addr == "" {
		ch <- nil
		return ch
	}
	go func() {
		attached := make(chan struct{}, 1)
		go func() {
			err := s.n.AttachRelay(ctx, addr)
			select {
			case <-attached:
			default:
				if err != nil {
					ch <- err
				} else {
					ch <- nil
				}
			}
			if err != nil {
				log.Printf("failed to attach relay: %v", err)
			}
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(25 * time.Millisecond):
				if s.Relay() != "" {
					select {
					case attached <- struct{}{}:
					default:
					}
					select {
					case ch <- nil:
					default:
					}
					return
				}
			}
		}
	}()
	return ch
}

func (s *Service) Start(ctx context.Context, relayAddr string, peers []string) {
	s.StartNode(ctx)

	m, _ := s.n.WhoAmI(ctx, "3.127.69.180:20018")
	s.n.SetAdvertisedAddr(net.JoinHostPort(string(m.Value), strconv.Itoa(s.conf.TcpPort)))

	if relayAddr == "" && s.conf.Relay != "" {
		relayAddr = s.conf.Relay
	}
	attached := s.AttachRelay(ctx, relayAddr)
	if len(peers) > 0 {
		select {
		case err := <-attached:
			if err != nil {
				log.Printf("relay attach failed: %v", err)
			}
			s.n.Bootstrap(ctx, peers)
		case <-ctx.Done():
			return
		}
	}
	s.n.StartMaintenance(ctx)
}
