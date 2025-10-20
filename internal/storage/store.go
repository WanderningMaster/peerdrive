package storage

import (
	"context"

	"github.com/WanderningMaster/peerdrive/internal/block"
)

type Store interface {
	PutBlock(ctx context.Context, b *block.Block) error
	GetBlock(ctx context.Context, c block.CID) (*block.Block, error)
	GetBlockLocal(ctx context.Context, c block.CID) (*block.Block, error)

	// Pin adds a hard pin which protects the CID (and its DAG) from GC.
	Pin(ctx context.Context, c block.CID) error
	// Pin with ttl
	PinSoft(ctx context.Context, c block.CID) error
	Unpin(ctx context.Context, c block.CID) error
	ListPins(ctx context.Context) ([]block.CID, error)
	GC(ctx context.Context) (freed int, err error)

	// Stats returns the number of stored blocks and the total bytes occupied by them.
	Stats(ctx context.Context) (blocks int, bytes int64, err error)
}

type BlockFetcher interface {
	FetchBlock(ctx context.Context, cid block.CID) ([]byte, error)
	Announce(ctx context.Context, cid block.CID) error
	Unannounce(ctx context.Context, cid block.CID) error
}
