package node

import (
	"bytes"
	"context"
	"fmt"

	"github.com/WanderningMaster/peerdrive/internal/block"
	"github.com/WanderningMaster/peerdrive/internal/util"
	"github.com/fxamacker/cbor/v2"
)

type ProviderRecord struct {
	V      uint8  `cbor:"v"`
	CID    []byte `cbor:"cid"`
	PeerID []byte `cbor:"peer"`
	Addr   []byte `cbor:"addrs"`
	Relay  []byte `cbor:"relay,omitempty"`
}

func (ps *Node) PutProviderRecord(ctx context.Context, cid block.CID) error {
	rec := ProviderRecord{
		V:      0,
		CID:    cid.ToBytes(),
		PeerID: ps.ID[:],
		Addr:   []byte(ps.advertisedAddr()),
	}
	if ps.relayAddr != "" {
		rec.Relay = []byte(ps.relayAddr)
	}

	enc := util.Must(cbor.CanonicalEncOptions().EncMode())
	var buf bytes.Buffer
	if err := enc.NewEncoder(&buf).Encode(rec); err != nil {
		return fmt.Errorf("encode node payload: %w", err)
	}
	cidStr, _ := cid.Encode()
	ps.Store(ctx, cidStr, buf.Bytes())

	return nil
}

func (ps *Node) GetProviderRecord(ctx context.Context, cid block.CID) ([]ProviderRecord, error) {
	cidStr, _ := cid.Encode()

	bb, err := ps.GetClosest(ctx, cidStr)
	if err != nil {
		return nil, fmt.Errorf("unknown cid: %w", err)
	}

	var mps []ProviderRecord
	dec := util.Must(cbor.DecOptions{TimeTag: cbor.DecTagIgnored}.DecMode())
	for _, b := range bb {
		var mp ProviderRecord
		if err := dec.Unmarshal(b, &mp); err != nil {
			continue
		}

		mps = append(mps, mp)
	}

	return mps, nil
}

// DeleteProviderRecord removes the local provider record for the given CID.
// Remote replicas stored via DHT will naturally expire based on TTL.
func (ps *Node) DeleteProviderRecord(ctx context.Context, cid block.CID) error {
	cidStr, _ := cid.Encode()
	ps.storeMu.Lock()
	delete(ps.store, cidStr)
	ps.storeMu.Unlock()
	return nil
}
