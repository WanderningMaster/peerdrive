package block

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

func TestSerializeAndComputeCID(t *testing.T) {
	payload := []byte("hello")
	b := &Block{
		Header:  BlockHeader{V: 1, Type: BlockData, Codec: "raw"},
		Payload: payload,
	}

	if err := b.Serialize(); err != nil {
		t.Fatalf("Serialize() error: %v", err)
	}

	if got, want := b.Header.Size, uint64(len(payload)); got != want {
		t.Fatalf("header size mismatch: got %d want %d", got, want)
	}
	if len(b.Bytes) == 0 {
		t.Fatalf("Serialize() did not populate Bytes")
	}

	if err := b.ComputeCID(); err != nil {
		t.Fatalf("ComputeCID() error: %v", err)
	}

	c, err := NewCID(b.Bytes)
	if err != nil {
		t.Fatalf("NewCID() error: %v", err)
	}
	if b.CID != c {
		t.Fatalf("CID mismatch: got %+v want %+v", b.CID, c)
	}
}

func TestBuildBlockAndDecodeRoundTrip(t *testing.T) {
	payload := []byte("abc123")
	b1, err := BuildBlock(BlockData, "raw", payload)
	if err != nil {
		t.Fatalf("BuildBlock error: %v", err)
	}

	if b1.Header.V != 1 {
		t.Fatalf("unexpected header version: %d", b1.Header.V)
	}
	if b1.Header.Type != BlockData {
		t.Fatalf("unexpected header type: %d", b1.Header.Type)
	}
	if b1.Header.Size != uint64(len(payload)) {
		t.Fatalf("unexpected header size: %d", b1.Header.Size)
	}
	if b1.Header.Codec != "raw" {
		t.Fatalf("unexpected header codec: %q", b1.Header.Codec)
	}

	b2, err := DecodeBlock(b1.Bytes)
	if err != nil {
		t.Fatalf("DecodeBlock error: %v", err)
	}

	if b2.Header != b1.Header {
		t.Fatalf("decoded header mismatch: got %+v want %+v", b2.Header, b1.Header)
	}
	if !bytes.Equal(b2.Payload, payload) {
		t.Fatalf("decoded payload mismatch: got %q want %q", string(b2.Payload), string(payload))
	}
	if !bytes.Equal(b2.Bytes, b1.Bytes) {
		t.Fatalf("decoded raw bytes mismatch")
	}
	if b2.CID != b1.CID {
		t.Fatalf("decoded CID mismatch: got %+v want %+v", b2.CID, b1.CID)
	}

	b3, err := BuildBlock(BlockData, "raw", payload)
	if err != nil {
		t.Fatalf("BuildBlock #2 error: %v", err)
	}
	if b3.CID != b1.CID {
		t.Fatalf("CID not deterministic: got %+v want %+v", b3.CID, b1.CID)
	}
	if !bytes.Equal(b3.Bytes, b1.Bytes) {
		t.Fatalf("Bytes not deterministic")
	}
}

func TestDecodeBlockTruncatedPayload(t *testing.T) {
	payload := bytes.Repeat([]byte{'x'}, 10)
	b, err := BuildBlock(BlockData, "raw", payload)
	if err != nil {
		t.Fatalf("BuildBlock error: %v", err)
	}

	if len(b.Bytes) < 3 {
		t.Fatalf("unexpectedly short block bytes")
	}
	truncated := b.Bytes[:len(b.Bytes)-5]
	_, err = DecodeBlock(truncated)
	if err == nil {
		t.Fatalf("expected error for truncated payload, got nil")
	}
	if !strings.Contains(err.Error(), "truncated payload") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestComputeCIDRequiresSerializeFirst(t *testing.T) {
	var b Block
	if err := b.ComputeCID(); err == nil {
		t.Fatalf("expected error when Bytes is empty")
	} else if !strings.Contains(err.Error(), "Serialize() first") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestJoin(t *testing.T) {
	a := []byte{1, 2, 3}
	b := []byte{4, 5}
	out := Join(a, b)

	if len(out) != 4+len(a)+len(b) {
		t.Fatalf("length mismatch: got %d want %d", len(out), 4+len(a)+len(b))
	}

	n := binary.BigEndian.Uint32(out[:4])
	if int(n) != len(a) {
		t.Fatalf("prefix length mismatch: got %d want %d", n, len(a))
	}

	if !bytes.Equal(out[4:4+len(a)], a) {
		t.Fatalf("first payload mismatch")
	}
	if !bytes.Equal(out[4+len(a):], b) {
		t.Fatalf("second payload mismatch")
	}
}

func TestDecodeBlockInvalidHeader(t *testing.T) {
	raw := []byte{0xff}
	if _, err := DecodeBlock(raw); err == nil {
		t.Fatalf("expected decode header error, got nil")
	} else if !strings.HasPrefix(err.Error(), "decode header:") {
		t.Fatalf("unexpected error prefix: %v", err)
	}
}
