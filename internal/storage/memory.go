package storage

import (
	"context"
	"errors"
	"sync"

	"github.com/WanderningMaster/peerdrive/internal/block"
)

var (
	ErrNotFound = errors.New("block not found")
)

type MemStore struct {
	mu      sync.RWMutex
	store   map[block.CID][]byte
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
	}
	for _, o := range options {
		o(m)
	}

	return m
}

func (s *MemStore) PutBlock(ctx context.Context, b *block.Block) error {
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

	if s.fetcher != nil {
		return s.fetcher.Announce(ctx, b.CID)
	}
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
		// _ = s.local.PutBlock(ctx, blk)
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
