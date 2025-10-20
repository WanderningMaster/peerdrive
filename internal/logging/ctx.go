package logging

import (
	"context"
	"log"
	"os"
)

type logKeyType int

const (
	ClientPrefix      = "client"
	ServerPrefix      = "server"
	RelayClientPrefix = "relay_client"
	Maintainance      = "maintainance"
	Reprovider        = "reprovider"
)

const envPrefixKey logKeyType = iota

func WithPrefix(ctx context.Context, prefix string) context.Context {
	return context.WithValue(ctx, envPrefixKey, prefix)
}

func PrefixFrom(ctx context.Context) string {
	if v := ctx.Value(envPrefixKey); v != nil {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func Logf(ctx context.Context, format string, args ...any) {
	arg := os.Getenv("VERBOSE")
	if arg == "1" {
		p := PrefixFrom(ctx)
		log.Printf("[%s] "+format, append([]any{p}, args...)...)
	}
}
