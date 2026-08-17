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

	// Writes counts successful DB writes (post-commit), labelled by table
	// (departments/plans) and op (insert/update). LLD §13.2
	// catalog_admin_writes_total{table,op}.
	Writes = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "catalog_admin_writes_total",
			Help: "Successful DB writes, labelled by table (departments|plans) and op (insert|update).",
		},
		[]string{"table", "op"},
	)

	// OptimisticLockConflicts counts 409 optimistic_lock_conflict responses
	// from CAT-2/CAT-5, labelled by table. LLD §13.2
	// catalog_admin_optimistic_lock_conflicts_total{table}.
	OptimisticLockConflicts = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "catalog_admin_optimistic_lock_conflicts_total",
			Help: "Optimistic-lock conflict (409) count from CAT-2/CAT-5, labelled by table.",
		},
		[]string{"table"},
	)
)

// Register installs this package's metrics on the default Prometheus
// registry. Call once at startup, before /metrics is served — mirrors
// iam-org-membership's internal/adapter/outbound/metrics/business.go.
func Register() {
	prometheus.MustRegister(CacheHits, CacheMisses, Writes, OptimisticLockConflicts)
	// Pre-initialise known label combinations so dashboards show 0 rather
	// than "no data" before the first request.
	for _, key := range []string{"cat:departments", "cat:plans"} {
		CacheHits.WithLabelValues(key)
		CacheMisses.WithLabelValues(key)
	}
	for _, tbl := range []string{"departments", "plans"} {
		OptimisticLockConflicts.WithLabelValues(tbl)
	}
	Writes.WithLabelValues("departments", "insert")
	Writes.WithLabelValues("departments", "update")
	Writes.WithLabelValues("plans", "update")
}
