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
		Alpha:      5,
		Replicas:   5,
		RpcTimeout: 3 * time.Second,
	}
}
