package block

import (
    "testing"
)

func TestCIDEncodeDecodeRoundTrip(t *testing.T) {
    data := []byte("hello world")
    c1, err := NewCID(data)
    if err != nil {
        t.Fatalf("NewCID error: %v", err)
    }
    s, err := c1.Encode()
    if err != nil {
        t.Fatalf("Encode error: %v", err)
    }
    c2, err := DecodeCID(s)
    if err != nil {
        t.Fatalf("DecodeCID error: %v", err)
    }
    if c1 != c2 {
        t.Fatalf("CID mismatch after round-trip: got %+v want %+v", c2, c1)
    }
}

func TestNewCIDDeterminismAndDifference(t *testing.T) {
    a := []byte("abc")
    b := []byte("abc")
    c := []byte("abd")

    ca1, _ := NewCID(a)
    ca2, _ := NewCID(b)
    if ca1 != ca2 {
        t.Fatalf("CID not deterministic for same input")
    }

    cc, _ := NewCID(c)
    if ca1 == cc {
        t.Fatalf("different inputs produced identical CID")
    }
}

