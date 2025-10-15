package blockfetcher

import (
	"context"

	"github.com/WanderningMaster/peerdrive/internal/block"
	"github.com/WanderningMaster/peerdrive/internal/node"
)

type Fetcher struct {
	node *node.Node
}

func New(node *node.Node) *Fetcher {
	return &Fetcher{
		node: node,
	}
}

func (f *Fetcher) FetchBlock(ctx context.Context, cid block.CID) ([]byte, error) {
	pr, err := f.node.GetProviderRecord(ctx, cid)
	if err != nil {
		return nil, err
	}
	return f.node.FetchBlock(ctx, string(pr.Addr), cid)
}

func (f *Fetcher) Announce(ctx context.Context, cid block.CID) error {
	return f.node.PutProviderRecord(ctx, cid)
}
