package block

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/fxamacker/cbor/v2"
)

type BlockType uint8

const (
	BlockData     BlockType = 1 // raw file chunk
	BlockNode     BlockType = 2 // internal DAG node
	BlockManifest BlockType = 3 // top-level file manifest
)

type BlockHeader struct {
	V     uint8     `cbor:"v"` // header/version for parsing
	Type  BlockType `cbor:"type"`
	Size  uint64    `cbor:"size"`
	Codec string    `cbor:"codec,omitempty"` // "raw", "cbor", ...
	// reserved / future fields:
	_ struct{} `cbor:",toarray"` // fixed ordering; helps determinism across encoders
}

// Block is the concatenation of the encoded header and the raw payload.
// The CID is computed over header||payload.
type Block struct {
	CID     CID
	Header  BlockHeader
	Payload []byte
	Bytes   []byte
}

func (b *Block) Serialize() error {
	if b.Payload == nil {
		b.Payload = []byte{}
	}
	b.Header.Size = uint64(len(b.Payload))

	encOpts := cbor.CanonicalEncOptions()
	enc, err := encOpts.EncMode()
	if err != nil {
		return err
	}
	var hdr bytes.Buffer
	if err := enc.NewEncoder(&hdr).Encode(b.Header); err != nil {
		return fmt.Errorf("encode header: %w", err)
	}

	b.Bytes = append(hdr.Bytes(), b.Payload...)
	return nil
}

func (b *Block) ComputeCID() error {
	if len(b.Bytes) == 0 {
		return errors.New("Serialize() first")
	}
	c, err := NewCID(b.Bytes)
	if err != nil {
		return err
	}
	b.CID = c
	return nil
}

func BuildBlock(typ BlockType, codec string, payload []byte) (*Block, error) {
	b := &Block{
		Header: BlockHeader{
			V:     1,
			Type:  typ,
			Size:  uint64(len(payload)),
			Codec: codec,
		},
		Payload: payload,
	}
	if err := b.Serialize(); err != nil {
		return nil, err
	}
	if err := b.ComputeCID(); err != nil {
		return nil, err
	}
	return b, nil
}

func DecodeBlock(raw []byte) (*Block, error) {
	decMode, err := cbor.DecOptions{TimeTag: cbor.DecTagIgnored}.DecMode()
	if err != nil {
		return nil, err
	}

	// Decode the first CBOR item into RawMessage to capture exact header bytes.
	var rawHdr cbor.RawMessage
	if err := decMode.NewDecoder(bytes.NewReader(raw)).Decode(&rawHdr); err != nil {
		return nil, fmt.Errorf("decode header: %w", err)
	}
	headerLen := len(rawHdr)
	if headerLen < 1 || len(raw) < headerLen {
		return nil, errors.New("bad header length")
	}

	// Unmarshal the header bytes into the struct (uses same DecMode).
	var hdr BlockHeader
	if err := decMode.Unmarshal(rawHdr, &hdr); err != nil {
		return nil, fmt.Errorf("decode header: %w", err)
	}

	if uint64(len(raw)-headerLen) < hdr.Size {
		return nil, errors.New("truncated payload")
	}
	payload := raw[headerLen : headerLen+int(hdr.Size)]

	b := &Block{
		Header:  hdr,
		Payload: payload,
		Bytes:   raw[:headerLen+int(hdr.Size)],
	}
	if err := b.ComputeCID(); err != nil {
		return nil, err
	}
	return b, nil
}

func Join(a, b []byte) []byte {
	buf := make([]byte, 4+len(a)+len(b))
	binary.BigEndian.PutUint32(buf[:4], uint32(len(a)))
	copy(buf[4:], a)
	copy(buf[4+len(a):], b)
	return buf
}
