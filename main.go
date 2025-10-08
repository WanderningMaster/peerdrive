package main

import (
	"fmt"

	"github.com/WanderningMaster/peerdrive/internal/block"
)

func main() {
	chunkBytes := []byte("hello world")
	blk, err := block.BuildBlock(block.BlockData, "raw", chunkBytes)
	if err != nil {
		panic(err)
	}
	cidStr, _ := blk.CID.Encode()
	fmt.Println("CID:", cidStr)
	fmt.Println("Block size (bytes):", len(blk.Bytes)) // header+payload

	decoded, err := block.DecodeBlock(blk.Bytes)
	if err != nil {
		panic(err)
	}
	fmt.Println("Type:", decoded.Header.Type, "Payload:", len(decoded.Payload), "bytes")
	fmt.Println(string(decoded.Payload))
}
