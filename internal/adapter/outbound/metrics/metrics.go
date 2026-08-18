// Package metrics registers this service's own business metrics, prefixed
// catalog_admin_ (LLD §13.2) — distinct from, and in addition to, the
// generic HTTP request metrics platform-gincommon's ObservabilityMiddlewares
// already registers automatically (http_requests_total/
// http_request_duration_seconds — shared, service-agnostic names/labels
// (method, route, status_class, error_class), identical across the whole
// IAM fleet; see LLD §13.2's own bullet 6). RequestsTotal/RequestDuration
// below are this service's own literal §13.2 metrics — exact status code
// and quantile, not status_class/le buckets — so a reader following
// §13.2's PromQL examples finds exactly the names/labels documented there,
// not just an equivalent under a different name.
package metrics

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	// CacheHits counts cat:departments/cat:plans cache hits, labelled by
	// key. LLD §13.2: "cat:departments/cat:plans cache hit ratio" is
	// derived from this and CacheMisses via PromQL
	// (rate(catalog_admin_cache_hits_total)/(rate(hits)+rate(misses))) —
	// a raw ratio gauge would be redundant with these two counters and is
	// not itself instrumented.
	CacheHits *prometheus.CounterVec
	// CacheMisses counts cat:departments/cat:plans cache misses (including
	// Valkey being down — CAT-FAIL-1 treats both as a fall-through to
	// Postgres, not an error).
	CacheMisses *prometheus.CounterVec
	// WritesTotal counts departments/plans inserts and updates, labelled by
	// table and op (LLD §13.2's catalog_admin_writes_total{table,op}).
	WritesTotal *prometheus.CounterVec
	// OptimisticLockConflicts counts CAT-2/CAT-5's 409 optimistic_lock_conflict
	// rate, labelled by table (LLD §13.2's
	// catalog_admin_optimistic_lock_conflicts_total{table}). A sustained
	// nonzero rate feeds the §13.5 alert ("sustained > 0 for > 15 minutes")
	// — writes are rare enough that any sustained rate is itself
	// diagnostic of a caller retry-storm or tooling bug.
	OptimisticLockConflicts *prometheus.CounterVec
	// RequestsTotal is LLD §13.2's catalog_admin_requests_total{route,status}
	// — every CAT-1 through CAT-I2 call's terminal outcome, labelled by the
	// matched route template and the exact HTTP status code. Recorded by
	// the inbound HTTP adapter's request-metrics middleware, scoped to the
	// /api/v1 group (not the unauthenticated infra probes).
	RequestsTotal *prometheus.CounterVec
	// RequestDuration is LLD §13.2's
	// catalog_admin_request_duration_seconds{route,quantile} — feeds the
	// §13.1 SLOs directly. A Summary (not a Histogram), because §13.2 names
	// the label "quantile", which is a Summary's exposition shape (a
	// Histogram would expose "le" instead).
	RequestDuration *prometheus.SummaryVec
)

// build (re)creates every collector above with constLabels applied and
// assigns them to the package vars. Called at package init with no labels
// (so code/tests that reference these vars work before Register runs) and
// again from Register with gincommon's {service, version} labels — the
// second build wins because Register always runs before the metrics are
// scraped or incremented in the real binary.
func build(constLabels prometheus.Labels) {
	CacheHits = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:        "catalog_admin_cache_hits_total",
			Help:        "Cache hits against this service's own cat:* keys, labelled by key.",
			ConstLabels: constLabels,
		},
		[]string{"key"},
	)
	CacheMisses = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:        "catalog_admin_cache_misses_total",
			Help:        "Cache misses against this service's own cat:* keys, labelled by key.",
			ConstLabels: constLabels,
		},
		[]string{"key"},
	)
	WritesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:        "catalog_admin_writes_total",
			Help:        "Departments/plans inserts and updates, labelled by table and op.",
			ConstLabels: constLabels,
		},
		[]string{"table", "op"},
	)
	OptimisticLockConflicts = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:        "catalog_admin_optimistic_lock_conflicts_total",
			Help:        "CAT-2/CAT-5 409 optimistic_lock_conflict occurrences, labelled by table.",
			ConstLabels: constLabels,
		},
		[]string{"table"},
	)
	RequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:        "catalog_admin_requests_total",
			Help:        "CAT-1 through CAT-I2 terminal outcomes, labelled by route and exact HTTP status code.",
			ConstLabels: constLabels,
		},
		[]string{"route", "status"},
	)
	RequestDuration = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name:        "catalog_admin_request_duration_seconds",
			Help:        "CAT-1 through CAT-I2 request latency in seconds, labelled by route. Feeds the §13.1 SLOs.",
			Objectives:  map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
			ConstLabels: constLabels,
		},
		[]string{"route"},
	)
}

func init() {
	build(nil)
}

var registerOnce sync.Once

// Register (re)builds this package's collectors with constLabels and installs
// them on reg, after gincommon.ObservabilityMiddlewares/DefaultMiddlewares has
// run — pass gincommon.MetricsRegisterer() and gincommon.MetricsConstLabels()
// so these catalog_admin_* collectors are registered the same way, and carry
// the same {service, version} labels, as gincommon's own http_requests_total/
// etc. Mirrors iam-org-membership's internal/adapter/outbound/metrics/
// business.go.
//
// Idempotent via sync.Once (mirrors gincommon's own metrics.Init): the first
// caller wins. Needed because the e2e test suite builds a fresh RouterConfig
// per test via the same composition-root call path — without this, the
// second test's call would panic with "duplicate metrics collector
// registration attempted" against the shared prometheus.DefaultRegisterer.
func Register(reg prometheus.Registerer, constLabels prometheus.Labels) {
	registerOnce.Do(func() {
		registerOnto(reg, constLabels)
	})
}

func registerOnto(reg prometheus.Registerer, constLabels prometheus.Labels) {
	build(constLabels)
	reg.MustRegister(CacheHits, CacheMisses, WritesTotal, OptimisticLockConflicts,
		RequestsTotal, RequestDuration)
	// Pre-initialise known label values so dashboards show 0 rather than
	// "no data" before the first request (same rationale as O&M's
	// business.go).
	for _, key := range []string{"cat:departments", "cat:plans"} {
		CacheHits.WithLabelValues(key)
		CacheMisses.WithLabelValues(key)
	}
	for _, table := range []string{"departments", "plans"} {
		OptimisticLockConflicts.WithLabelValues(table)
	}
	WritesTotal.WithLabelValues("departments", "insert")
	WritesTotal.WithLabelValues("departments", "update")
	WritesTotal.WithLabelValues("plans", "update")
}
