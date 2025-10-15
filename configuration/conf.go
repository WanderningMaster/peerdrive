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
    // Maintenance/GC
    BucketRefresh      time.Duration
    RecordTTL          time.Duration
    RepublishInterval  time.Duration
    GCInterval         time.Duration
    RevalidateInterval time.Duration
    // Limits and health
    MaxValueSize     int
    FailureThreshold int
}

func Default() Config {
    return Config{
        IdBits:     256,
        KBucketK:   20,
        Alpha:      5,
        Replicas:   5,
        RpcTimeout: 3 * time.Second,
        // Reasonable defaults
        BucketRefresh:      1 * time.Hour,
        RecordTTL:          24 * time.Hour,
        RepublishInterval:  12 * time.Hour,
        GCInterval:         1 * time.Minute,
        RevalidateInterval: 10 * time.Minute,
        MaxValueSize:       1 << 20, // 1 MiB
        FailureThreshold:   3,
    }
}
