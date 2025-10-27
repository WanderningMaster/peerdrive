package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	nethttp "net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/WanderningMaster/peerdrive/configuration"
	"github.com/WanderningMaster/peerdrive/internal/block"
	blockfetcher "github.com/WanderningMaster/peerdrive/internal/block-fetcher"
	"github.com/WanderningMaster/peerdrive/internal/dag"
	"github.com/WanderningMaster/peerdrive/internal/id"
	"github.com/WanderningMaster/peerdrive/internal/logging"
	"github.com/WanderningMaster/peerdrive/internal/node"
	"github.com/WanderningMaster/peerdrive/internal/routing"
	"github.com/WanderningMaster/peerdrive/internal/storage"
	"github.com/WanderningMaster/peerdrive/internal/util"
	daemon "github.com/coreos/go-systemd/v22/daemon"
	"github.com/fxamacker/cbor/v2"
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
		blockstore = storage.NewMemStore(
			storage.WithFetcher(fetcher),
			storage.WithSoftTTL(configuration.Default().SoftPinTTL),
		)
	} else {
		blockstore, _ = storage.NewDiskStore(conf.BlockstorePath,
			storage.DiskWithFetcher(fetcher),
			storage.DiskWithSoftTTL(configuration.Default().SoftPinTTL),
		)
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
	name := filepath.Base(inPath)
	if strings.TrimSpace(name) == "" || name == "." || name == string(filepath.Separator) {
		name = "file"
	}

	f, err := os.Open(inPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	// Peek first bytes to detect MIME
	header := make([]byte, 512)
	n, _ := io.ReadFull(f, header)
	header = header[:n]
	ctype := nethttp.DetectContentType(header)
	if ctype == "application/octet-stream" {
		if ext := strings.ToLower(filepath.Ext(name)); ext != "" {
			if t := mime.TypeByExtension(ext); t != "" {
				ctype = t
			}
		}
	}

	// Reconstruct full reader including consumed header bytes
	r := io.MultiReader(bytes.NewReader(header), f)

	_, cid, err := s.builder.BuildFromReader(ctx, name, ctype, r)
	if err != nil {
		return "", err
	}
	return cid.Encode()
}

// AddFromPathDistributed builds a DAG and distributes blocks across peers
// instead of storing everything locally. By default keeps only the manifest locally.
func (s *Service) AddFromPathDistributed(ctx context.Context, inPath string) (string, error) {
	name := filepath.Base(inPath)
	if strings.TrimSpace(name) == "" || name == "." || name == string(filepath.Separator) {
		name = "file"
	}

	f, err := os.Open(inPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	header := make([]byte, 512)
	n, _ := io.ReadFull(f, header)
	header = header[:n]
	ctype := nethttp.DetectContentType(header)
	if ctype == "application/octet-stream" {
		if ext := strings.ToLower(filepath.Ext(name)); ext != "" {
			if t := mime.TypeByExtension(ext); t != "" {
				ctype = t
			}
		}
	}

	r := io.MultiReader(bytes.NewReader(header), f)

	// Build a temporary builder that uses a distributed store wrapper
	ds := NewDistStore(s.n, s.store, s.n.Replicas(), KeepLocalSelector(true, 0.2))
	builder := dag.DagBuilder{
		ChunkSize: s.builder.ChunkSize,
		Fanout:    s.builder.Fanout,
		Codec:     s.builder.Codec,
		Store:     ds,
	}

	_, cid, err := builder.BuildFromReader(ctx, name, ctype, r)
	if err != nil {
		return "", err
	}
	return cid.Encode()
}

func (s *Service) Fetch(ctx context.Context, cid block.CID) ([]byte, error) {
	return dag.FetchParallel(ctx, s.store, cid, 16)
}

func (s *Service) Pin(ctx context.Context, cid block.CID) error   { return s.store.Pin(ctx, cid) }
func (s *Service) Unpin(ctx context.Context, cid block.CID) error { return s.store.Unpin(ctx, cid) }

type PinInfo struct {
	CID  string `json:"cid"`
	Name string `json:"name,omitempty"`
	Size int    `json:"size,omitempty"`
	Mime string `json:"mime,omitempty"`
}

func (s *Service) ListPins(ctx context.Context) ([]PinInfo, error) {
	cids, err := s.store.ListPins(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]PinInfo, 0, len(cids))
	dec := util.Must(cbor.DecOptions{TimeTag: cbor.DecTagIgnored}.DecMode())
	for _, c := range cids {
		// Only include manifest pins for user-visible pins list
		b, err := s.store.GetBlock(ctx, c)
		if err != nil || b == nil || b.Header.Type != block.BlockManifest {
			continue
		}
		var pi PinInfo
		if enc, err := c.Encode(); err == nil {
			pi.CID = enc
		}
		var mp dag.ManifestPayload
		if err := dec.Unmarshal(b.Payload, &mp); err == nil {
			pi.Name = mp.Name
			pi.Mime = mp.Mime
			pi.Size = int(mp.Size)
		}
		out = append(out, pi)
	}
	return out, nil
}

func (s *Service) ManifestMeta(ctx context.Context, cid block.CID) (string, string, error) {
	b, err := s.store.GetBlock(ctx, cid)
	if err != nil {
		return "", "", err
	}
	if b == nil || b.Header.Type != block.BlockManifest {
		return "", "", errors.New("not a manifest")
	}
	var mp dag.ManifestPayload
	dec := util.Must(cbor.DecOptions{TimeTag: cbor.DecTagIgnored}.DecMode())
	if err := dec.Unmarshal(b.Payload, &mp); err != nil {
		return "", "", err
	}
	return mp.Name, mp.Mime, nil
}

func (s *Service) Closest(target id.NodeID, k int) []routing.Contact {
	return s.n.ClosestContacts(target, k)
}

func (s *Service) Bootstrap(ctx context.Context, peers []string) { s.n.Bootstrap(ctx, peers) }

func (s *Service) GC(ctx context.Context) (int, error) { return s.store.GC(ctx) }

func (s *Service) StoreStats(ctx context.Context) (int, int64, error) { return s.store.Stats(ctx) }

func (s *Service) StartNode(ctx context.Context) {
	ctx = logging.WithPrefix(ctx, logging.ServerPrefix)

	go func() {
		if err := s.n.ListenAndServe(ctx); err != nil {
			log.Printf("node server exited: %v", err)
		}
	}()
}

func (s *Service) AttachRelay(ctx context.Context, addr string) <-chan error {
	ctx = logging.WithPrefix(ctx, "relay_client")

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

	// FIXME???
	m, _ := s.n.WhoAmI(ctx, "52.59.95.49:20018")
	fmt.Println(string(m.Value))
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
	s.startReprovider(ctx, time.Hour*6)
	s.startBlockstoreGC(ctx, time.Hour)

	_, _ = daemon.SdNotify(false, daemon.SdNotifyReady)
}

// launches a background loop that periodically scans pinned
// roots and announces provider records for all locally present DAG blocks.
func (s *Service) startReprovider(ctx context.Context, interval time.Duration) {
	ctx = logging.WithPrefix(ctx, "reprovider")

	logging.Logf(ctx, "reprovider loop starting interval=%s", interval)
	go func() {
		// Run one pass after startup
		s.reprovideOnce(ctx)

		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				logging.Logf(ctx, "reprovider loop stopped")
				return
			case <-t.C:
				s.reprovideOnce(ctx)
			}
		}
	}()
}

func (s *Service) reprovideOnce(ctx context.Context) {
	start := time.Now()

	// FIXME: should reprovide soft pins as well
	pins, err := s.store.ListPins(ctx)
	if err != nil {
		logging.Logf(ctx, "reprovide: list pins error: %v", err)
		return
	}
	visited := make(map[block.CID]struct{}, len(pins)*4)
	announced := 0
	for _, root := range pins {
		stack := []block.CID{root}
		for len(stack) > 0 {
			last := len(stack) - 1
			c := stack[last]
			stack = stack[:last]

			if _, seen := visited[c]; seen {
				continue
			}
			visited[c] = struct{}{}

			blk, err := s.store.GetBlockLocal(ctx, c)
			if err != nil || blk == nil {
				continue
			}

			if err := s.n.PutProviderRecord(ctx, c); err == nil {
				announced++
			}

			children, err := dag.ChildCIDsFromBlock(blk)
			if err != nil {
				continue
			}
			stack = append(stack, children...)

			select {
			case <-ctx.Done():
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
	dur := time.Since(start)
	logging.Logf(ctx, "reprovide pass pins=%d visited=%d announced=%d dur=%s", len(pins), len(visited), announced, dur)
}

func (s *Service) startBlockstoreGC(ctx context.Context, interval time.Duration) {
	ctx = logging.WithPrefix(ctx, "gc_store")

	if interval <= 0 {
		return
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				freed, err := s.store.GC(ctx)
				if err != nil && err != context.Canceled {
					log.Printf("GC error: %v", err)
				} else if freed > 0 {
					log.Printf("GC freed %d blocks", freed)
				}
			}
		}
	}()
}
