package service

import (
	"context"
	"encoding/binary"
	"fmt"

	"github.com/WanderningMaster/peerdrive/internal/block"
	"github.com/WanderningMaster/peerdrive/internal/dag"
	"github.com/WanderningMaster/peerdrive/internal/id"
	"github.com/WanderningMaster/peerdrive/internal/routing"
)

// distStore implements dag.BlockPutGetter. It distributes PutBlock writes
// to remote peers and optionally stores certain blocks locally (e.g., manifests).
type distStore struct {
	n         NodePutter
	local     dag.BlockPutGetter
	replicas  int
	keepLocal func(*block.Block) bool
}

// NodePutter captures the subset of node methods used by distStore.
type NodePutter interface {
	IterativeFindNode(ctx context.Context, target id.NodeID, want int) []routing.Contact
	PutBlock(ctx context.Context, c routing.Contact, b *block.Block) error
	KBucketK() int
}

func NewDistStore(n NodePutter, local dag.BlockPutGetter, replicas int, keepLocal func(*block.Block) bool) *distStore {
	if replicas <= 0 {
		replicas = 1
	}
	if keepLocal == nil {
		keepLocal = func(b *block.Block) bool { return b.Header.Type == block.BlockManifest }
	}
	return &distStore{n: n, local: local, replicas: replicas, keepLocal: keepLocal}
}

func (s *distStore) PutBlock(ctx context.Context, b *block.Block) error {
	if b == nil {
		return nil
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

	// Choose peers by Kademlia distance to the CID.
	key, err := b.CID.Encode()
	if err != nil {
		return err
	}
	target := id.HashKey(key)
	cands := s.n.IterativeFindNode(ctx, target, s.n.KBucketK())
	// send to up to replicas peers and count successful stores
	successes := 0
	for _, peer := range cands {
		if successes >= s.replicas {
			break
		}
		if err := s.n.PutBlock(ctx, peer, b); err == nil {
			successes++
		} else {
			fmt.Println("FAILED TO STORE BLOCK REMOTELY", err)
		}
	}

	// Always keep selected blocks locally (e.g., manifest),
	// and also fall back to local store if no remote accepted the block
	if s.keepLocal(b) || successes == 0 {
		if err := s.local.PutBlock(ctx, b); err != nil {
			return err
		}
		type localPinner interface {
			Pin(ctx context.Context, c block.CID) error
		}
		if p, ok := any(s.local).(localPinner); ok {
			if err := p.Pin(ctx, b.CID); err != nil {
				return err
			}
		} else {
			return fmt.Errorf("local store does not support pinning")
		}
		return nil
	}
	return nil
}

func (s *distStore) GetBlock(ctx context.Context, c block.CID) (*block.Block, error) {
	return s.local.GetBlock(ctx, c)
}

func KeepLocalSelector(manifestAlways bool, fraction float64) func(*block.Block) bool {
	if fraction <= 0 {
		return func(b *block.Block) bool {
			return manifestAlways && b.Header.Type == block.BlockManifest
		}
	}
	if fraction >= 1 {
		return func(b *block.Block) bool { return true }
	}
	// Precompute threshold for uint64 range
	max := ^uint64(0)
	threshold := uint64(float64(max) * fraction)
	return func(b *block.Block) bool {
		if manifestAlways && b.Header.Type == block.BlockManifest {
			return true
		}
		// Deterministic sampling using the first 8 bytes of CID digest
		v := binary.BigEndian.Uint64(b.CID.Digest[:8])
		return v <= threshold
	}
}
