package blockfetcher

import (
	"context"

	"github.com/WanderningMaster/peerdrive/internal/block"
	"github.com/WanderningMaster/peerdrive/internal/id"
	"github.com/WanderningMaster/peerdrive/internal/node"
	"github.com/WanderningMaster/peerdrive/internal/routing"
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
	prs, err := f.node.GetProviderRecord(ctx, cid)
	if err != nil {
		return nil, err
	}

	for _, pr := range prs {
		b, _err := f._fetchBlock(ctx, pr, cid)
		err = _err
		if err != nil {
			continue
		}

		return b, nil
	}

	return nil, err
}

func (f *Fetcher) _fetchBlock(ctx context.Context, pr node.ProviderRecord, cid block.CID) ([]byte, error) {
	c := routing.Contact{Addr: string(pr.Addr)}
	if len(pr.Relay) != 0 {
		c.Relay = string(pr.Relay)
	}
	var pid id.NodeID
	if len(pr.PeerID) == len(pid) {
		copy(pid[:], pr.PeerID)
		c.ID = pid
	}
	return f.node.FetchBlock(ctx, c, cid)
}

func (f *Fetcher) Announce(ctx context.Context, cid block.CID) error {
	return f.node.PutProviderRecord(ctx, cid)
}

func (f *Fetcher) Unannounce(ctx context.Context, cid block.CID) error {
	return f.node.DeleteProviderRecord(ctx, cid)
}
