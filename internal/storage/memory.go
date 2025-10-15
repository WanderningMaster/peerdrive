package storage

import (
	"context"
	"errors"
	"sync"

	"github.com/WanderningMaster/peerdrive/internal/block"
	"github.com/WanderningMaster/peerdrive/internal/dag"
)

var (
	ErrNotFound = errors.New("block not found")
)

type MemStore struct {
	mu      sync.RWMutex
	store   map[block.CID][]byte
	pins    map[block.CID]struct{}
	fetcher BlockFetcher
}

func WithFetcher(fetcher BlockFetcher) func(*MemStore) {
	return func(s *MemStore) {
		s.fetcher = fetcher
	}
}

func NewMemStore(options ...func(*MemStore)) *MemStore {
	m := &MemStore{
		store: make(map[block.CID][]byte),
		pins:  make(map[block.CID]struct{}),
	}
	for _, o := range options {
		o(m)
	}

	return m
}

func (s *MemStore) PutBlock(ctx context.Context, b *block.Block) error {
	err := s.PutBlockLocally(ctx, b)
	if err != nil {
		return err
	}

	if s.fetcher != nil {
		return s.fetcher.Announce(ctx, b.CID)
	}
	return nil
}

func (s *MemStore) PutBlockLocally(ctx context.Context, b *block.Block) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if b == nil {
		return errors.New("nil block")
	}
	if len(b.Bytes) == 0 {
		if err := b.Serialize(); err != nil {
			return err
		}
	}
	if (b.CID == block.CID{}) {
		if err := b.ComputeCID(); err != nil {
			return err
		}
	}

	cpy := make([]byte, len(b.Bytes))
	copy(cpy, b.Bytes)
	s.mu.Lock()
	s.store[b.CID] = cpy
	s.mu.Unlock()

	return nil
}

func (s *MemStore) GetBlock(ctx context.Context, c block.CID) (*block.Block, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if blk, err := s.GetBlockLocal(ctx, c); err == nil {
		return blk, nil
	} else {
		raw, err := s.fetcher.FetchBlock(ctx, c)
		if err != nil || len(raw) == 0 {
			return nil, ErrNotFound
		}
		blk, err := block.DecodeBlock(raw)
		if err != nil {
			return nil, err
		}

		if blk.CID == c {
			_ = s.PutBlockLocally(ctx, blk)

			// announce only manifest
			// this cache is accidental and probably would be GC'd soon
			if blk.Header.Type == block.BlockManifest {
				s.fetcher.Announce(ctx, blk.CID)
			}
		}
		return blk, nil
	}
}

func (s *MemStore) GetBlockLocal(ctx context.Context, c block.CID) (*block.Block, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	raw, ok := s.store[c]
	s.mu.RUnlock()

	if ok {
		cpy := make([]byte, len(raw))
		copy(cpy, raw)
		blk, err := block.DecodeBlock(cpy)
		if err != nil {
			return nil, err
		}
		if blk.CID != c {
			return nil, errors.New("stored bytes CID mismatch")
		}
		return blk, nil
	}

	return nil, ErrNotFound
}

func (s *MemStore) Pin(ctx context.Context, c block.CID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	s.pins[c] = struct{}{}
	s.mu.Unlock()
	return nil
}

func (s *MemStore) Unpin(ctx context.Context, c block.CID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.pins, c)
	s.mu.Unlock()
	return nil
}

func (s *MemStore) ListPins(ctx context.Context) ([]block.CID, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]block.CID, 0, len(s.pins))
	for c := range s.pins {
		out = append(out, c)
	}
	return out, nil
}

func (s *MemStore) GC(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	s.mu.RLock()
	pinsSnap := make([]block.CID, 0, len(s.pins))
	for c := range s.pins {
		pinsSnap = append(pinsSnap, c)
	}
	keysSnap := make([]block.CID, 0, len(s.store))
	for c := range s.store {
		keysSnap = append(keysSnap, c)
	}
	s.mu.RUnlock()

	live := make(map[block.CID]struct{}, len(pinsSnap)*4)
	stack := make([]block.CID, 0, len(pinsSnap))
	stack = append(stack, pinsSnap...)

	for len(stack) > 0 {
		// pop
		last := len(stack) - 1
		c := stack[last]
		stack = stack[:last]

		if _, seen := live[c]; seen {
			continue
		}
		live[c] = struct{}{}

		blk, err := s.GetBlockLocal(ctx, c)
		if err != nil {
			continue
		}

		children, err := dag.ChildCIDsFromBlock(blk)
		if err != nil {
			continue
		}
		for _, ch := range children {
			if _, seen := live[ch]; !seen {
				stack = append(stack, ch)
			}
		}
	}

	var freed int
	var toDelete []block.CID
	for _, c := range keysSnap {
		if _, keep := live[c]; !keep {
			toDelete = append(toDelete, c)
		}
	}

	s.mu.Lock()
	for _, c := range toDelete {
		if _, still := s.store[c]; !still {
			continue
		}
		delete(s.store, c)
		freed++
		if s.fetcher != nil {
			s.fetcher.Unannounce(ctx, c)
		}
	}
	s.mu.Unlock()

	return freed, nil
}
