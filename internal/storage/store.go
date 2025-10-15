package storage

import (
	"context"

	"github.com/WanderningMaster/peerdrive/internal/block"
)

type Store interface {
	PutBlock(ctx context.Context, b *block.Block) error
	GetBlock(ctx context.Context, c block.CID) (*block.Block, error)

	Pin(ctx context.Context, c block.CID) error
	Unpin(ctx context.Context, c block.CID) error
	ListPins(ctx context.Context) ([]block.CID, error)
	GC(ctx context.Context) (freed int, err error)
}

type BlockFetcher interface {
	FetchBlock(ctx context.Context, cid block.CID) ([]byte, error)
	Announce(ctx context.Context, cid block.CID) error
	Unannounce(ctx context.Context, cid block.CID) error
}
