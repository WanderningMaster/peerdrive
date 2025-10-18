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
    pr, err := f.node.GetProviderRecord(ctx, cid)
    if err != nil {
        return nil, err
    }
    c := routing.Contact{Addr: string(pr.Addr)}
    if len(pr.Relay) != 0 {
        c.Relay = string(pr.Relay)
    }
    // Include provider ID so relay dialing can route to the correct target.
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
