// Package valkey implements port.Cache backed by Valkey (Redis-compatible).
// Cache is advisory-only (LLD §6, CAT-FAIL-1) — every miss, timeout, or
// outage must fall through to Postgres; /readyz stays ready while
// Postgres is healthy even if the cache is down.
package valkey

import (
	"context"
	"errors"
	"log"
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

// Logger is the minimal structured-logging capability this package needs —
// satisfied structurally by platform-gincommon/pkg/logger's port.Logger
// (an internal type, so this package declares its own duck-typed interface
// rather than importing it directly).
type Logger interface {
	Error(msg string, fields map[string]interface{})
}

// pkgLogger is nil until SetLogger is called (e.g. from main.go); nil means
// "fall back to stdlib log" so package tests that never call SetLogger
// still see output somewhere instead of silently discarding it.
var pkgLogger Logger

// SetLogger installs the structured logger used by this package's own
// error logging (currently just Delete's invalidation-failure line, LLD
// §9.3). Call once at startup, mirroring metrics.Register()'s idiom.
func SetLogger(l Logger) { pkgLogger = l }

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
// (LLD §11 — "cat:departments/cat:plans cache hit ratio" is one of this
// service's own dashboard's tracked signals) so the adapter, not the core
// service layer, owns this cross-cutting concern.
func (c *Cache) Get(ctx context.Context, key string) ([]byte, error) {
	val, err := c.client.Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			metrics.CacheMisses.WithLabelValues(key).Inc()
			metrics.DeprecatedCacheMisses.WithLabelValues(key).Inc()
			return nil, nil
		}
		// A real outage/timeout, not a miss — left out of the hit-ratio
		// metric; Health() is the signal for Valkey being down.
		return nil, err
	}
	metrics.CacheHits.WithLabelValues(key).Inc()
	metrics.DeprecatedCacheHits.WithLabelValues(key).Inc()
	return val, nil
}

func (c *Cache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return c.client.Set(ctx, key, value, ttl).Err()
}

// Delete invalidates keys (called on every CAT-1/CAT-2/CAT-5 write, post-
// commit — LLD §6). A failure here is advisory, same as everywhere else in
// this cache (CAT-FAIL-1): the stale entry self-heals within its own TTL,
// so this never fails the write. It is logged, per §9.3's failure matrix,
// so an operator can tell a Valkey write-path problem from silence.
//
// No trace_id/request_id here: platform-gincommon's TraceIDFromContext/
// RequestIDFromContext both require a *gin.Context, which this adapter
// layer never has (only the plain context.Context that flows down from
// it) — reaching for the OTel API directly to reconstruct one just for
// this single advisory log line isn't worth a new dependency on this
// package. This is a package-level cross-cutting failure ("Valkey's
// write path is unhealthy"), not usually one specific request's problem,
// so the structured message plus the failing keys is enough to act on.
func (c *Cache) Delete(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	err := c.client.Del(ctx, keys...).Err()
	if err != nil {
		if pkgLogger != nil {
			pkgLogger.Error("cache invalidation failed", map[string]interface{}{"keys": keys, "error": err.Error()})
		} else {
			log.Printf("[ERROR] cache invalidation failed keys=%v error=%v", keys, err)
		}
	}
	return err
}

func (c *Cache) Health(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

func (c *Cache) Close() error {
	return c.client.Close()
}

// ─── Key builders (LLD §6) ───────────────────────────────────────────────

// DepartmentsKey builds cat:departments — the full department catalog,
// 60s TTL, invalidated on any CAT-1/CAT-2 write.
const DepartmentsKey = "cat:departments"

// PlansKey builds cat:plans — the full plan catalog, 60s TTL, invalidated
// on any CAT-5 write.
const PlansKey = "cat:plans"
