package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/WanderningMaster/peerdrive/internal/block"
	"github.com/WanderningMaster/peerdrive/internal/dag"
	"github.com/syndtr/goleveldb/leveldb"
	lutil "github.com/syndtr/goleveldb/leveldb/util"
)

type DiskStore struct {
	db      *leveldb.DB
	baseDir string
	mu      sync.RWMutex
	fetcher BlockFetcher
	softTTL time.Duration
}

func DiskWithFetcher(fetcher BlockFetcher) func(*DiskStore) {
	return func(s *DiskStore) { s.fetcher = fetcher }
}

func DiskWithSoftTTL(ttl time.Duration) func(*DiskStore) {
	return func(s *DiskStore) { s.softTTL = ttl }
}

func NewDiskStore(baseDir string, options ...func(*DiskStore)) (*DiskStore, error) {
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, err
	}
	dbPath := filepath.Join(baseDir, "index")
	db, err := leveldb.OpenFile(dbPath, nil)
	if err != nil {
		return nil, err
	}
	s := &DiskStore{db: db, baseDir: baseDir}
	for _, o := range options {
		o(s)
	}
	if s.softTTL <= 0 {
		s.softTTL = 6 * time.Hour
	}
	return s, nil
}

func (s *DiskStore) Close() error { return s.db.Close() }

const cidBytesLen = 34

func blockKey(c block.CID) []byte {
	cidb := c.ToBytes()
	b := make([]byte, 1+len(cidb))
	b[0] = 'b'
	copy(b[1:], cidb)
	return b
}

func pinKey(c block.CID) []byte {
	cidb := c.ToBytes()
	b := make([]byte, 1+len(cidb))
	b[0] = 'p'
	copy(b[1:], cidb)
	return b
}

func softPinKey(c block.CID) []byte {
	cidb := c.ToBytes()
	b := make([]byte, 1+len(cidb))
	b[0] = 's'
	copy(b[1:], cidb)
	return b
}

func encodeExpiry(t time.Time) []byte {
	// store as big-endian int64 seconds
	b := make([]byte, 8)
	sec := t.Unix()
	for i := 7; i >= 0; i-- {
		b[i] = byte(sec & 0xff)
		sec >>= 8
	}
	return b
}

func decodeExpiry(b []byte) time.Time {
	if len(b) < 8 {
		return time.Unix(0, 0)
	}
	var sec int64
	for i := 0; i < 8; i++ {
		sec = (sec << 8) | int64(b[i])
	}
	return time.Unix(sec, 0)
}

func cidFromKey(k []byte) (block.CID, error) {
	if len(k) < 1+cidBytesLen {
		return block.CID{}, errors.New("bad key length")
	}
	return block.CidFromBytes(k[1 : 1+cidBytesLen])
}

func (s *DiskStore) blockRelPath(c block.CID) string {
	if enc, err := c.Encode(); err == nil && len(enc) > 0 {
		if len(enc) >= 5 {
			return filepath.Join("blocks", enc[1:3], enc[3:5], enc)
		}
		return filepath.Join("blocks", enc)
	}
	return ""
}

func (s *DiskStore) blockAbsPath(c block.CID) string {
	return filepath.Join(s.baseDir, s.blockRelPath(c))
}

func (s *DiskStore) PutBlock(ctx context.Context, b *block.Block) error {
	if err := s.PutBlockLocally(ctx, b); err != nil {
		return err
	}

	if s.fetcher != nil {
		return s.fetcher.Announce(ctx, b.CID)
	}
	return nil
}

func (s *DiskStore) PutBlockLocally(ctx context.Context, b *block.Block) error {
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
	abs := s.blockAbsPath(b.CID)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}

	// if the process crashes mid‑write, only the temp file is affected
	tmp := abs + ".tmp"
	if err := os.WriteFile(tmp, b.Bytes, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, abs); err != nil {
		return err
	}
	rel := s.blockRelPath(b.CID)
	return s.db.Put(blockKey(b.CID), []byte(rel), nil)
}

func (s *DiskStore) GetBlock(ctx context.Context, c block.CID) (*block.Block, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if blk, err := s.GetBlockLocal(ctx, c); err == nil {
		return blk, nil
	}
	if s.fetcher != nil {
		raw, err := s.fetcher.FetchBlock(ctx, c)
		if err != nil || len(raw) == 0 {
			return nil, ErrNotFound
		}
		blk, err := block.DecodeBlock(raw)
		if err != nil {
			return nil, err
		}
		if blk.CID == c {
			_ = s.PutBlockLocally(ctx, blk)
			if blk.Header.Type == block.BlockManifest {
				_ = s.fetcher.Announce(ctx, blk.CID)
			}
		}
		return blk, nil
	}
	return nil, ErrNotFound
}

func (s *DiskStore) GetBlockLocal(ctx context.Context, c block.CID) (*block.Block, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rel, err := s.db.Get(blockKey(c), nil)
	if err != nil {
		if err == leveldb.ErrNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	abs := filepath.Join(s.baseDir, string(rel))
	raw, err := os.ReadFile(abs)
	if err != nil {
		return nil, ErrNotFound
	}
	blk, err := block.DecodeBlock(raw)
	if err != nil {
		return nil, err
	}
	if blk.CID != c {
		return nil, errors.New("stored bytes CID mismatch")
	}
	// Refresh soft pin TTL if present
	if v, err2 := s.db.Get(softPinKey(c), nil); err2 == nil {
		_ = s.db.Put(softPinKey(c), encodeExpiry(time.Now().Add(s.softTTL)), nil)
		_ = v // silence unused in case of build tags
	}
	return blk, nil
}

func (s *DiskStore) Pin(ctx context.Context, c block.CID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Hard pin: ensure soft pin is cleared
	if err := s.db.Delete(softPinKey(c), nil); err != nil && err != leveldb.ErrNotFound {
		return err
	}
	// 'r' for recursive (default)
	return s.db.Put(pinKey(c), []byte{'r'}, nil)
}

func (s *DiskStore) PinDirect(ctx context.Context, c block.CID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Hard pin: ensure soft pin is cleared
	if err := s.db.Delete(softPinKey(c), nil); err != nil && err != leveldb.ErrNotFound {
		return err
	}
	// 'd' for direct
	return s.db.Put(pinKey(c), []byte{'d'}, nil)
}

func (s *DiskStore) PinSoft(ctx context.Context, c block.CID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Only create soft pin if no hard pin exists
	if _, err := s.db.Get(pinKey(c), nil); err == nil {
		return nil
	}
	return s.db.Put(softPinKey(c), encodeExpiry(time.Now().Add(s.softTTL)), nil)
}

func (s *DiskStore) Unpin(ctx context.Context, c block.CID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Remove both hard and soft pins
	_ = s.db.Delete(softPinKey(c), nil)
	return s.db.Delete(pinKey(c), nil)
}

func (s *DiskStore) ListPins(ctx context.Context) ([]block.CID, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Only list hard pins
	it := s.db.NewIterator(lutil.BytesPrefix([]byte{'p'}), nil)
	defer it.Release()
	out := make([]block.CID, 0, 128)
	for it.Next() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		c, err := cidFromKey(it.Key())
		if err != nil {
			continue
		}
		out = append(out, c)
	}
	if err := it.Error(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *DiskStore) GC(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	// Split roots into recursive and direct
	var recPins []block.CID
	var dirPins []block.CID
	itpHard := s.db.NewIterator(lutil.BytesPrefix([]byte{'p'}), nil)
	for itpHard.Next() {
		c, err := cidFromKey(itpHard.Key())
		if err != nil {
			continue
		}
		v := itpHard.Value()
		mode := byte('r')
		if len(v) > 0 {
			mode = v[0]
		}
		if mode == 'd' {
			dirPins = append(dirPins, c)
		} else {
			recPins = append(recPins, c)
		}
	}
	itpHard.Release()
	// Add unexpired soft pins as direct roots as well
	itp := s.db.NewIterator(lutil.BytesPrefix([]byte{'s'}), nil)
	now := time.Now()
	for itp.Next() {
		exp := decodeExpiry(itp.Value())
		if now.Before(exp) {
			if c, err := cidFromKey(itp.Key()); err == nil {
				dirPins = append(dirPins, c)
			}
		}
	}
	itp.Release()

	type rec struct {
		cid block.CID
		key []byte
		rel string
	}
	var all []rec
	it := s.db.NewIterator(lutil.BytesPrefix([]byte{'b'}), nil)
	for it.Next() {
		if err := ctx.Err(); err != nil {
			it.Release()
			return 0, err
		}
		k := append([]byte(nil), it.Key()...)
		v := append([]byte(nil), it.Value()...)
		c, err := cidFromKey(k)
		if err == nil {
			all = append(all, rec{cid: c, key: k, rel: string(v)})
		}
	}
	if err := it.Error(); err != nil {
		it.Release()
		return 0, err
	}
	it.Release()

	live := make(map[block.CID]struct{}, (len(recPins)+len(dirPins))*4)
	stack := make([]block.CID, 0, len(recPins))
	// mark direct pins live but don't traverse from them
	for _, c := range dirPins {
		live[c] = struct{}{}
	}
	// traverse from recursive roots
	stack = append(stack, recPins...)
	for len(stack) > 0 {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		last := len(stack) - 1
		c := stack[last]
		stack = stack[:last]
		if _, seen := live[c]; seen {
			continue
		}
		live[c] = struct{}{}
		blk, err := s.GetBlockLocal(ctx, c)
		if err != nil {
			continue
		}
		children, err := dag.ChildCIDsFromBlock(blk)
		if err != nil {
			continue
		}
		for _, ch := range children {
			if _, seen := live[ch]; !seen {
				stack = append(stack, ch)
			}
		}
	}

	var freed int
	for _, r := range all {
		if _, keep := live[r.cid]; keep {
			continue
		}
		_ = os.Remove(filepath.Join(s.baseDir, r.rel))
		if err := s.db.Delete(r.key, nil); err == nil {
			freed++
			if s.fetcher != nil {
				_ = s.fetcher.Unannounce(ctx, r.cid)
			}
		}
	}
	return freed, nil
}

func (s *DiskStore) Stats(ctx context.Context) (int, int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, 0, err
	}
	it := s.db.NewIterator(lutil.BytesPrefix([]byte{'b'}), nil)
	defer it.Release()
	var blocks int
	var bytes int64
	for it.Next() {
		if err := ctx.Err(); err != nil {
			return 0, 0, err
		}
		rel := string(it.Value())
		abs := filepath.Join(s.baseDir, rel)
		if fi, err := os.Stat(abs); err == nil {
			bytes += fi.Size()
		}
		blocks++
	}
	if err := it.Error(); err != nil {
		return 0, 0, err
	}
	return blocks, bytes, nil
}
