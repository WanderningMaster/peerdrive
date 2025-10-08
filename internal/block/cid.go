package block

import (
	"fmt"

	mbase "github.com/multiformats/go-multibase"
	// Uncomment if you want BLAKE3 support:
	"lukechampine.com/blake3"
)

const CIDVersion uint8 = 1

type CID struct {
	Version CIDVersionField
	Digest  [32]byte
}

type CIDVersionField uint8

func (c CID) Encode() (string, error) {
	buf := make([]byte, 1+1+len(c.Digest))
	buf[0] = byte(c.Version)
	copy(buf[2:], c.Digest[:])
	return mbase.Encode(mbase.Base32, buf)
}

func DecodeCID(s string) (CID, error) {
	_, raw, err := mbase.Decode(s)
	if err != nil {
		return CID{}, err
	}
	if len(raw) != 34 {
		return CID{}, fmt.Errorf("bad CID length: %d", len(raw))
	}
	var c CID
	c.Version = CIDVersionField(raw[0])
	copy(c.Digest[:], raw[2:])
	return c, nil
}

func computeDigest(data []byte) ([32]byte, error) {
	return blake3.Sum256(data), nil
}

func NewCID(bytes []byte) (CID, error) {
	sum, err := computeDigest(bytes)
	if err != nil {
		return CID{}, err
	}
	return CID{
		Version: CIDVersionField(CIDVersion),
		Digest:  sum,
	}, nil
}
