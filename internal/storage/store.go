package storage

import (
	"context"

	"github.com/WanderningMaster/peerdrive/internal/block"
)

type Store interface {
	PutBlock(ctx context.Context, b *block.Block) error
	GetBlock(ctx context.Context, c block.CID) (*block.Block, error)
}

type BlockFetcher interface {
	FetchBlock(ctx context.Context, cid block.CID) ([]byte, error)
	Announce(ctx context.Context, cid block.CID) error
}
