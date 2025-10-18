package dag

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync/atomic"

	"github.com/WanderningMaster/peerdrive/internal/block"
	"github.com/WanderningMaster/peerdrive/internal/util"
	"github.com/fxamacker/cbor/v2"
)

type NodePayload struct {
	V      uint8    `cbor:"v"`
	Size   uint64   `cbor:"size"`
	Fanout uint16   `cbor:"fanout"`
	CIDs   [][]byte `cbor:"cids"`
	Spans  []uint64 `cbor:"spans"`
	_      struct{} `cbor:",toarray"`
}

type ManifestPayload struct {
	V      uint8    `cbor:"v"`
	Size   uint64   `cbor:"size"`
	Chunk  uint32   `cbor:"chunk"`
	Fanout uint16   `cbor:"fanout"`
	Root   []byte   `cbor:"root"`
	Name   string   `cbor:"name,omitempty"`
	Mime   string   `cbor:"mime,omitempty"`
	_      struct{} `cbor:",toarray"`
}

type DagBuilder struct {
	ChunkSize int
	Fanout    int
	Codec     string // "raw" for leaves, "cbor" for nodes/manifest
	Store     BlockPutGetter
}

type BlockGetter interface {
	GetBlock(ctx context.Context, c block.CID) (*block.Block, error)
}

type BlockPutGetter interface {
	PutBlock(ctx context.Context, b *block.Block) error
	GetBlock(ctx context.Context, c block.CID) (*block.Block, error)
}

func DefaultBuilder(store BlockPutGetter) *DagBuilder {
	return &DagBuilder{ChunkSize: 1 << 20, Fanout: 256, Codec: "cbor", Store: store}
}

// BuildFromReader ingests r, builds the Merkle DAG, stores all blocks, and
// returns the manifest block and its CID.
func (b *DagBuilder) BuildFromReader(ctx context.Context, name string, r io.Reader) (*block.Block, block.CID, error) {
	if b.ChunkSize <= 0 || b.Fanout <= 1 {
		return nil, block.CID{}, errors.New("invalid builder params")
	}

	type leaf struct {
		cid  block.CID
		size uint64
	}
	leaves := make([]leaf, 0, 1024)
	var total uint64
	buf := make([]byte, b.ChunkSize)
	for {
		n, err := io.ReadFull(r, buf)
		if err == io.ErrUnexpectedEOF {
			// short final chunk
		} else if err == io.EOF {
			break
		} else if err != nil && err != io.ErrUnexpectedEOF {
			return nil, block.CID{}, err
		}
		if n == 0 {
			break
		}

		payload := make([]byte, n)
		copy(payload, buf[:n])
		leafBlock, err := block.BuildBlock(block.BlockData, "raw", payload)
		if err != nil {
			return nil, block.CID{}, err
		}
		if err := b.Store.PutBlock(ctx, leafBlock); err != nil {
			return nil, block.CID{}, err
		}
		leaves = append(leaves, leaf{cid: leafBlock.CID, size: uint64(n)})
		total += uint64(n)
		if n < b.ChunkSize {
			break
		}
	}

	// represent empty file with an empty data block
	if len(leaves) == 0 {
		empty, err := block.BuildBlock(block.BlockData, "raw", nil)
		if err != nil {
			return nil, block.CID{}, err
		}
		if err := b.Store.PutBlock(ctx, empty); err != nil {
			return nil, block.CID{}, err
		}
		leaves = append(leaves, leaf{cid: empty.CID, size: 0})
	}

	type pair struct {
		cid  block.CID
		span uint64
	}
	cur := make([]pair, 0, len(leaves))
	for _, lf := range leaves {
		cur = append(cur, pair{cid: lf.cid, span: lf.size})
	}

	enc := util.Must(cbor.CanonicalEncOptions().EncMode())
	for len(cur) > 1 {
		next := make([]pair, 0, (len(cur)+b.Fanout-1)/b.Fanout)
		for i := 0; i < len(cur); i += b.Fanout {
			j := min(i+b.Fanout, len(cur))
			grp := cur[i:j]

			var nodeSize uint64
			cids := make([][]byte, 0, len(grp))
			spans := make([]uint64, 0, len(grp))
			for _, p := range grp {
				cids = append(cids, p.cid.ToBytes())
				spans = append(spans, p.span)
				nodeSize += p.span
			}

			payload := NodePayload{V: 1, Size: nodeSize, Fanout: uint16(b.Fanout), CIDs: cids, Spans: spans}
			var buf bytes.Buffer
			if err := enc.NewEncoder(&buf).Encode(payload); err != nil {
				return nil, block.CID{}, fmt.Errorf("encode node payload: %w", err)
			}
			nodeBlock, err := block.BuildBlock(block.BlockNode, "cbor", buf.Bytes())
			if err != nil {
				return nil, block.CID{}, err
			}
			if err := b.Store.PutBlock(ctx, nodeBlock); err != nil {
				return nil, block.CID{}, err
			}
			next = append(next, pair{cid: nodeBlock.CID, span: nodeSize})
		}
		cur = next
	}

	root := cur[0]

	mp := ManifestPayload{V: 1, Size: total, Chunk: uint32(b.ChunkSize), Fanout: uint16(b.Fanout), Root: root.cid.ToBytes(), Name: name}
	var mbytes bytes.Buffer
	if err := enc.NewEncoder(&mbytes).Encode(mp); err != nil {
		return nil, block.CID{}, fmt.Errorf("encode manifest: %w", err)
	}
	mblk, err := block.BuildBlock(block.BlockManifest, "cbor", mbytes.Bytes())
	if err != nil {
		return nil, block.CID{}, err
	}
	if err := b.Store.PutBlock(ctx, mblk); err != nil {
		return nil, block.CID{}, err
	}
	return mblk, mblk.CID, nil
}

func Verify(ctx context.Context, s BlockGetter, manifestCID block.CID) error {
	mblk, err := s.GetBlock(ctx, manifestCID)
	if err != nil {
		return err
	}
	if mblk.Header.Type != block.BlockManifest {
		return errors.New("not a manifest")
	}

	var mp ManifestPayload
	dec := util.Must(cbor.DecOptions{TimeTag: cbor.DecTagIgnored}.DecMode())
	if err := dec.Unmarshal(mblk.Payload, &mp); err != nil {
		return fmt.Errorf("manifest decode: %w", err)
	}
	root, err := block.CidFromBytes(mp.Root)
	if err != nil {
		return err
	}
	var visited uint64
	if err := verifySubtree(ctx, s, root, mp.Size, &visited, dec); err != nil {
		return err
	}
	return nil
}

func verifySubtree(ctx context.Context, s BlockGetter, c block.CID, expectSpan uint64, visited *uint64, dec cbor.DecMode) error {
	b, err := s.GetBlock(ctx, c)
	if err != nil {
		return err
	}
	// Integrity: recompute CID and compare against expected
	if b.CID != c {
		return errors.New("CID mismatch: corrupted data")
	}
	switch b.Header.Type {
	case block.BlockData:
		if uint64(len(b.Payload)) != expectSpan {
			// Allow a final short chunk when total size not multiple of chunk size,
			// but internal nodes should have encoded the exact span already.
			return fmt.Errorf("leaf span mismatch: have %d expect %d", len(b.Payload), expectSpan)
		}
		atomic.AddUint64(visited, 1)
		return nil
	case block.BlockNode:
		var np NodePayload
		if err := dec.Unmarshal(b.Payload, &np); err != nil {
			return fmt.Errorf("node decode: %w", err)
		}
		if np.Size != expectSpan {
			return fmt.Errorf("node size mismatch: have %d expect %d", np.Size, expectSpan)
		}
		if len(np.CIDs) != len(np.Spans) {
			return errors.New("node malformed: cids/spans length mismatch")
		}
		for i := range np.CIDs {
			childCID, err := block.CidFromBytes(np.CIDs[i])
			if err != nil {
				return err
			}
			if err := verifySubtree(ctx, s, childCID, np.Spans[i], visited, dec); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unexpected block type under root: %d", b.Header.Type)
	}
}

func Fetch(ctx context.Context, s BlockGetter, manifestCID block.CID) ([]byte, error) {
	mblk, err := s.GetBlock(ctx, manifestCID)
	if err != nil {
		return nil, err
	}
	if mblk.Header.Type != block.BlockManifest {
		return nil, errors.New("not a manifest")
	}

	dec := util.Must(cbor.DecOptions{TimeTag: cbor.DecTagIgnored}.DecMode())
	var mp ManifestPayload
	if err := dec.Unmarshal(mblk.Payload, &mp); err != nil {
		return nil, fmt.Errorf("manifest decode: %w", err)
	}
	root, err := block.CidFromBytes(mp.Root)
	if err != nil {
		return nil, err
	}

	out := make([]byte, mp.Size)
	if err := fetchRangeSeq(ctx, s, root, 0, mp.Size, out, dec); err != nil {
		return nil, err
	}
	return out, nil
}

func fetchRangeSeq(
	ctx context.Context,
	s BlockGetter,
	cid block.CID,
	base uint64,
	span uint64,
	out []byte,
	dec cbor.DecMode,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	b, err := s.GetBlock(ctx, cid)
	if err != nil {
		return err
	}
	if b.CID != cid {
		return errors.New("CID mismatch during fetch")
	}

	switch b.Header.Type {
	case block.BlockData:
		if uint64(len(b.Payload)) < span {
			return fmt.Errorf("leaf payload too small: have %d want %d", len(b.Payload), span)
		}
		copy(out[base:base+span], b.Payload[:span])
		return nil

	case block.BlockNode:
		var np NodePayload
		if err := dec.Unmarshal(b.Payload, &np); err != nil {
			return fmt.Errorf("node decode: %w", err)
		}
		if np.Size != span {
			return fmt.Errorf("node size mismatch: have %d want %d", np.Size, span)
		}
		if len(np.CIDs) != len(np.Spans) {
			return errors.New("node malformed: cids/spans length mismatch")
		}

		offset := base
		for i := range np.CIDs {
			childCID, err := block.CidFromBytes(np.CIDs[i])
			if err != nil {
				return err
			}
			childSpan := np.Spans[i]
			if err := fetchRangeSeq(ctx, s, childCID, offset, childSpan, out, dec); err != nil {
				return err
			}
			offset += childSpan
		}
		return nil

	default:
		return fmt.Errorf("unexpected block type during fetch: %d", b.Header.Type)
	}
}

// Fetch reconstructs the full file into a byte slice, using up to parallel
// active fetches. It validates every block against its CID.
func FetchParallel(ctx context.Context, s BlockGetter, manifestCID block.CID, parallel int) ([]byte, error) {
	if parallel <= 0 {
		parallel = 16
	}
	mblk, err := s.GetBlock(ctx, manifestCID)
	if err != nil {
		return nil, err
	}
	if mblk.Header.Type != block.BlockManifest {
		return nil, errors.New("not a manifest")
	}
	dec := util.Must(cbor.DecOptions{TimeTag: cbor.DecTagIgnored}.DecMode())
	var mp ManifestPayload
	if err := dec.Unmarshal(mblk.Payload, &mp); err != nil {
		return nil, fmt.Errorf("manifest decode: %w", err)
	}
	root, err := block.CidFromBytes(mp.Root)
	if err != nil {
		return nil, err
	}

	out := make([]byte, mp.Size)
	sem := make(chan struct{}, parallel)
	errCh := make(chan error, 1)
	done := make(chan struct{})

	go func() {
		errCh <- fetchRange(ctx, s, root, 0, mp.Size, out, sem, dec)
		close(done)
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-done:
		if err := <-errCh; err != nil {
			return nil, err
		}
		return out, nil
	}
}

func fetchRange(ctx context.Context, s BlockGetter, cid block.CID, base uint64, span uint64, out []byte, sem chan struct{}, dec cbor.DecMode) error {
	select {
	case sem <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	b, err := s.GetBlock(ctx, cid)
	<-sem
	if err != nil {
		return err
	}
	if b.CID != cid {
		return errors.New("CID mismatch during fetch")
	}

	switch b.Header.Type {
	case block.BlockData:
		copy(out[base:base+span], b.Payload[:span])
		return nil
	case block.BlockNode:
		var np NodePayload
		if err := dec.Unmarshal(b.Payload, &np); err != nil {
			return fmt.Errorf("node decode: %w", err)
		}
		if np.Size != span {
			return fmt.Errorf("node size mismatch: have %d want %d", np.Size, span)
		}

		// Launch children respecting order (for correctness) and allowing parallelism.
		offset := base
		tasks := 0
		errCh := make(chan error, len(np.CIDs))
		for i := range np.CIDs {
			childCID, err := block.CidFromBytes(np.CIDs[i])
			if err != nil {
				return err
			}
			childSpan := np.Spans[i]
			// tail-call small subtrees inline to save goroutines
			if childSpan <= 1<<16 { // heuristic
				if err := fetchRange(ctx, s, childCID, offset, childSpan, out, sem, dec); err != nil {
					return err
				}
			} else {
				tasks++
				go func(c block.CID, off, sp uint64) {
					errCh <- fetchRange(ctx, s, c, off, sp, out, sem, dec)
				}(childCID, offset, childSpan)
			}
			offset += childSpan
		}
		for i := 0; i < tasks; i++ {
			if err := <-errCh; err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unexpected block type during fetch: %d", b.Header.Type)
	}
}

func ChildCIDsFromBlock(b *block.Block) ([]block.CID, error) {
	switch b.Header.Type {
	case block.BlockData:
		return nil, nil
	case block.BlockNode:
		dec, err := cbor.DecOptions{TimeTag: cbor.DecTagIgnored}.DecMode()
		if err != nil {
			return nil, err
		}
		var np NodePayload
		if err := dec.Unmarshal(b.Payload, &np); err != nil {
			return nil, err
		}
		out := make([]block.CID, 0, len(np.CIDs))
		for _, raw := range np.CIDs {
			c, err := block.CidFromBytes(raw)
			if err != nil {
				return nil, err
			}
			out = append(out, c)
		}
		return out, nil
	case block.BlockManifest:
		dec, err := cbor.DecOptions{TimeTag: cbor.DecTagIgnored}.DecMode()
		if err != nil {
			return nil, err
		}
		var mp ManifestPayload
		if err := dec.Unmarshal(b.Payload, &mp); err != nil {
			return nil, err
		}
		c, err := block.CidFromBytes(mp.Root)
		if err != nil {
			return nil, err
		}
		return []block.CID{c}, nil
	default:
		return nil, errors.New("unknown block type")
	}
}
