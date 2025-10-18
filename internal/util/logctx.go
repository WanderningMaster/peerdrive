package util

import (
	"context"
	"log"
)

type logKeyType int

const logPrefixKey logKeyType = iota

// WithLogPrefix returns a child context carrying a log prefix.
func WithLogPrefix(ctx context.Context, prefix string) context.Context {
	return context.WithValue(ctx, logPrefixKey, prefix)
}

// PrefixFrom extracts the log prefix from context or returns the default.
func PrefixFrom(ctx context.Context) string {
	if v := ctx.Value(logPrefixKey); v != nil {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// Logf prints a log line prefixed with the context-derived prefix.
func Logf(ctx context.Context, format string, args ...any) {
	p := PrefixFrom(ctx)
	log.Printf("[%s] "+format, append([]any{p}, args...)...)
}
