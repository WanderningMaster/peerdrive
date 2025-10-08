package id

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"math/big"
)

type NodeID [32]byte

func (id NodeID) String() string { return hex.EncodeToString(id[:]) }

func RandomID() NodeID {
	var id NodeID
	_, _ = rand.Read(id[:])
	return id
}

func HashKey(key string) NodeID {
	h := sha256.Sum256([]byte(key))
	return NodeID(h)
}

func XorDist(a, b NodeID) *big.Int {
	var x [32]byte
	for i := range 32 {
		x[i] = a[i] ^ b[i]
	}
	return new(big.Int).SetBytes(x[:])
}
