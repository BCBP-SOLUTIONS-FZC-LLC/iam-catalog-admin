package port

import (
	"context"
	"time"
)

// Cache is advisory-only (LLD §8, CAT-FAIL-1): every miss, timeout, or
// outage must fall through to Postgres, which stays the sole source of
// truth. A nil Cache is a valid configuration — services treat it the same
// as an always-miss cache.
type Cache interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Delete(ctx context.Context, keys ...string) error
	Health(ctx context.Context) error
	Close() error
}
