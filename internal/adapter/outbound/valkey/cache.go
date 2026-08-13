// Package valkey implements port.Cache backed by Valkey (Redis-compatible).
// Cache is advisory-only (LLD §8, CAT-FAIL-1) — every miss, timeout, or
// outage must fall through to Postgres; /readyz stays ready while
// Postgres is healthy even if the cache is down.
package valkey

import (
	"context"
	"errors"
	"time"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/adapter/outbound/metrics"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/core/port"
	"github.com/redis/go-redis/v9"
)

// Cache implements port.Cache.
type Cache struct {
	client *redis.Client
}

var _ port.Cache = (*Cache)(nil)

// New creates a Cache from addr. addr may be plain host:port or a full URL
// (redis://user:pass@host or rediss://... for TLS — required in
// production). Timeouts are tight by default (50 ms read/write, 100 ms
// dial): a slow Valkey must degrade to a fast miss rather than stall the
// request until the DB-backed fallback is no longer within SLO.
func New(addr string) *Cache {
	opts, err := redis.ParseURL(addr)
	if err != nil {
		opts = &redis.Options{Addr: addr}
	}
	if opts.DialTimeout == 0 {
		opts.DialTimeout = 100 * time.Millisecond
	}
	if opts.ReadTimeout == 0 {
		opts.ReadTimeout = 50 * time.Millisecond
	}
	if opts.WriteTimeout == 0 {
		opts.WriteTimeout = 50 * time.Millisecond
	}
	return &Cache{client: redis.NewClient(opts)}
}

// Get returns (nil, nil) on cache miss. Records a hit/miss metric per key
// (LLD §13 — "cat:departments/cat:plans cache hit ratio" is one of this
// service's own dashboard's tracked signals) so the adapter, not the core
// service layer, owns this cross-cutting concern.
func (c *Cache) Get(ctx context.Context, key string) ([]byte, error) {
	val, err := c.client.Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			metrics.CacheMisses.WithLabelValues(key).Inc()
			return nil, nil
		}
		// A real outage/timeout, not a miss — left out of the hit-ratio
		// metric; Health() is the signal for Valkey being down.
		return nil, err
	}
	metrics.CacheHits.WithLabelValues(key).Inc()
	return val, nil
}

func (c *Cache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return c.client.Set(ctx, key, value, ttl).Err()
}

func (c *Cache) Delete(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	return c.client.Del(ctx, keys...).Err()
}

func (c *Cache) Health(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

func (c *Cache) Close() error {
	return c.client.Close()
}

// ─── Key builders (LLD §8) ───────────────────────────────────────────────

// DepartmentsKey builds cat:departments — the full department catalog,
// 60s TTL, invalidated on any CAT-1/CAT-2 write.
const DepartmentsKey = "cat:departments"

// PlansKey builds cat:plans — the full plan catalog, 60s TTL, invalidated
// on any CAT-5 write.
const PlansKey = "cat:plans"
