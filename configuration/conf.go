package configuration

import (
	"time"
)

type Config struct {
	IdBits     int
	KBucketK   int
	Alpha      int
	Replicas   int
	RpcTimeout time.Duration
}

func Default() Config {
	return Config{
		IdBits:     256,
		KBucketK:   20,
		Alpha:      3,
		Replicas:   3,
		RpcTimeout: 2 * time.Second,
	}
}
