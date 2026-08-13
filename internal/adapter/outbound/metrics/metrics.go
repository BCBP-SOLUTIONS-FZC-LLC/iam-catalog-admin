// Package metrics registers this service's own business metrics on top of
// the generic HTTP request metrics platform-gincommon's
// ObservabilityMiddlewares already registers automatically. Metric names
// are prefixed catadmin_ (LLD §13 dashboard requirement), not iam_ — this
// is a separate service from iam-org-membership and must not collide with
// its metric namespace if both are ever scraped by the same Prometheus.
package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
	// CacheHits counts cat:departments/cat:plans cache hits, labelled by
	// key. LLD §13: "cat:departments/cat:plans cache hit ratio" is one of
	// this service's own dashboard's three tracked signals.
	CacheHits = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "catadmin_cache_hits_total",
			Help: "Cache hits against this service's own cat:* keys, labelled by key.",
		},
		[]string{"key"},
	)
	// CacheMisses counts cat:departments/cat:plans cache misses (including
	// Valkey being down — CAT-FAIL-1 treats both as a fall-through to
	// Postgres, not an error).
	CacheMisses = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "catadmin_cache_misses_total",
			Help: "Cache misses against this service's own cat:* keys, labelled by key.",
		},
		[]string{"key"},
	)
)

// Register installs this package's metrics on the default Prometheus
// registry. Call once at startup, before /metrics is served — mirrors
// iam-org-membership's internal/adapter/outbound/metrics/business.go.
func Register() {
	prometheus.MustRegister(CacheHits, CacheMisses)
	// Pre-initialise the two known label values so dashboards show 0
	// rather than "no data" before the first request (same rationale as
	// O&M's business.go).
	for _, key := range []string{"cat:departments", "cat:plans"} {
		CacheHits.WithLabelValues(key)
		CacheMisses.WithLabelValues(key)
	}
}
