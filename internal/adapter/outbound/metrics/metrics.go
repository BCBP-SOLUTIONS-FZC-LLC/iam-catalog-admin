// Package metrics registers this service's own business metrics, following the
// Enterprise Platform Observability Standard (Tier 3 service-specific metrics):
// primary names use the iam_catalog_admin_* prefix (<domain>_<service>_<metric>),
// distinct from the generic HTTP request metrics platform-gincommon's
// ObservabilityMiddlewares already registers automatically (http_requests_total/
// http_request_duration_seconds — shared, service-agnostic names/labels across
// the whole IAM fleet). RequestsTotal/RequestDuration are this service's own
// LLD §11.2 metrics — exact status code and a real bucketed histogram, not
// status_class/generic le buckets.
//
// Required labels (service, environment) are injected as ConstLabels via
// Register so instrumentation code cannot omit or misspell them.
//
// Backward compatibility: the deprecated catalog_admin_* series are emitted in
// parallel during the sunset period. Remove the Deprecated* vars and their
// registration once all dashboards, alerts, and SLO rules have migrated to the
// iam_catalog_admin_* names (deploy/monitoring/ files in this repo are already
// migrated — only external consumers need a sunset window).
package metrics

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	// ── Primary metrics — Tier 3: iam_catalog_admin_* ─────────────────────
	//
	// CacheHits counts cat:departments/cat:plans cache hits, labelled by key.
	// LLD §11.2: "cat:departments/cat:plans cache hit ratio" is derived from
	// this and CacheMisses via PromQL
	// (rate(iam_catalog_admin_cache_hits_total)/(rate(hits)+rate(misses))).
	CacheHits *prometheus.CounterVec
	// CacheMisses counts cat:departments/cat:plans cache misses (including
	// Valkey being down — CAT-FAIL-1 treats both as a fall-through to
	// Postgres, not an error).
	CacheMisses *prometheus.CounterVec
	// WritesTotal counts departments/plans inserts and updates, labelled by
	// table and op (LLD §11.2's iam_catalog_admin_writes_total{table,op}).
	WritesTotal *prometheus.CounterVec
	// OptimisticLockConflicts counts CAT-2/CAT-5's 409 optimistic_lock_conflict
	// rate, labelled by table. A sustained nonzero rate feeds the §11.5 alert
	// ("sustained > 0 for > 15 minutes") — writes are rare enough that any
	// sustained rate is itself diagnostic of a caller retry-storm or tooling bug.
	OptimisticLockConflicts *prometheus.CounterVec
	// RequestsTotal is LLD §11.2's iam_catalog_admin_requests_total{route,status}
	// — every CAT-1 through CAT-I2 call's terminal outcome, labelled by the
	// matched route template and the exact HTTP status code.
	RequestsTotal *prometheus.CounterVec
	// RequestDuration is LLD §11.2's
	// iam_catalog_admin_request_duration_seconds{route} — feeds the §11.1 SLOs
	// directly. A Histogram (not a Summary, CAT-D13): Summary quantiles can't be
	// aggregated across pods and expose no per-request good/bad ratio; the _bucket
	// series a Histogram provides is what slo-rules.yml's burn-rate alerts need.
	// Bucket boundaries land exactly on §11.1's SLO thresholds (15ms/30ms/40ms/
	// 100ms) so burn-rate `le` selectors are exact, not interpolated.
	RequestDuration *prometheus.HistogramVec

	// ── Deprecated legacy metrics — backward-compat sunset period ──────────
	//
	// These catalog_admin_* series are emitted in parallel with the
	// iam_catalog_admin_* primaries above per the Enterprise Platform
	// Observability Standard's backward-compatibility requirement. They carry
	// identical values. Remove after the approved sunset date.
	DeprecatedCacheHits               *prometheus.CounterVec
	DeprecatedCacheMisses             *prometheus.CounterVec
	DeprecatedWritesTotal             *prometheus.CounterVec
	DeprecatedOptimisticLockConflicts *prometheus.CounterVec
	DeprecatedRequestsTotal           *prometheus.CounterVec
	DeprecatedRequestDuration         *prometheus.HistogramVec
)

// build (re)creates every collector above with constLabels applied and assigns
// them to the package vars. Called at package init with no labels (so code and
// tests that reference these vars work before Register runs) and again from
// Register with the production {service, version, environment} labels — the
// second build wins because Register always runs before the metrics are scraped
// or incremented in the real binary.
func build(constLabels prometheus.Labels) {
	CacheHits = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:        "iam_catalog_admin_cache_hits_total",
			Help:        "Cache hits against this service's own cat:* keys, labelled by key.",
			ConstLabels: constLabels,
		},
		[]string{"key"},
	)
	CacheMisses = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:        "iam_catalog_admin_cache_misses_total",
			Help:        "Cache misses against this service's own cat:* keys, labelled by key.",
			ConstLabels: constLabels,
		},
		[]string{"key"},
	)
	WritesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:        "iam_catalog_admin_writes_total",
			Help:        "Departments/plans inserts and updates, labelled by table and op.",
			ConstLabels: constLabels,
		},
		[]string{"table", "op"},
	)
	OptimisticLockConflicts = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:        "iam_catalog_admin_optimistic_lock_conflicts_total",
			Help:        "CAT-2/CAT-5 409 optimistic_lock_conflict occurrences, labelled by table.",
			ConstLabels: constLabels,
		},
		[]string{"table"},
	)
	RequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:        "iam_catalog_admin_requests_total",
			Help:        "CAT-1 through CAT-I2 terminal outcomes, labelled by route and exact HTTP status code.",
			ConstLabels: constLabels,
		},
		[]string{"route", "status"},
	)
	RequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:        "iam_catalog_admin_request_duration_seconds",
			Help:        "CAT-1 through CAT-I2 request latency in seconds, labelled by route. Feeds the §11.1 SLOs.",
			Buckets:     []float64{0.005, 0.01, 0.015, 0.02, 0.03, 0.04, 0.05, 0.075, 0.1, 0.25, 0.5, 1, 2.5},
			ConstLabels: constLabels,
		},
		[]string{"route"},
	)

	// Deprecated legacy series — catalog_admin_* without the environment label
	// (matching their original label set so existing Prometheus rules that don't
	// select on environment continue to work unmodified during the sunset window).
	legacyLabels := legacyConstLabels(constLabels)
	DeprecatedCacheHits = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:        "catalog_admin_cache_hits_total",
			Help:        "DEPRECATED: use iam_catalog_admin_cache_hits_total. Emitted in parallel during the backward-compat sunset period.",
			ConstLabels: legacyLabels,
		},
		[]string{"key"},
	)
	DeprecatedCacheMisses = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:        "catalog_admin_cache_misses_total",
			Help:        "DEPRECATED: use iam_catalog_admin_cache_misses_total.",
			ConstLabels: legacyLabels,
		},
		[]string{"key"},
	)
	DeprecatedWritesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:        "catalog_admin_writes_total",
			Help:        "DEPRECATED: use iam_catalog_admin_writes_total.",
			ConstLabels: legacyLabels,
		},
		[]string{"table", "op"},
	)
	DeprecatedOptimisticLockConflicts = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:        "catalog_admin_optimistic_lock_conflicts_total",
			Help:        "DEPRECATED: use iam_catalog_admin_optimistic_lock_conflicts_total.",
			ConstLabels: legacyLabels,
		},
		[]string{"table"},
	)
	DeprecatedRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:        "catalog_admin_requests_total",
			Help:        "DEPRECATED: use iam_catalog_admin_requests_total.",
			ConstLabels: legacyLabels,
		},
		[]string{"route", "status"},
	)
	DeprecatedRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:        "catalog_admin_request_duration_seconds",
			Help:        "DEPRECATED: use iam_catalog_admin_request_duration_seconds.",
			Buckets:     []float64{0.005, 0.01, 0.015, 0.02, 0.03, 0.04, 0.05, 0.075, 0.1, 0.25, 0.5, 1, 2.5},
			ConstLabels: legacyLabels,
		},
		[]string{"route"},
	)
}

// legacyConstLabels returns a copy of constLabels with the "environment" key
// stripped. The deprecated catalog_admin_* series preserve their original label
// set so existing Prometheus queries without an environment selector continue to
// work during the sunset window without a label cardinality change.
func legacyConstLabels(constLabels prometheus.Labels) prometheus.Labels {
	if len(constLabels) == 0 {
		return constLabels
	}
	out := make(prometheus.Labels, len(constLabels))
	for k, v := range constLabels {
		if k != "environment" {
			out[k] = v
		}
	}
	return out
}

func init() {
	build(nil)
}

var registerOnce sync.Once

// Register (re)builds this package's collectors with constLabels and installs
// them on reg. constLabels must include at minimum "service" and "environment"
// (Enterprise Platform Observability Standard Tier 3 required labels) so that
// these iam_catalog_admin_* collectors carry the same base labels as
// gincommon's own http_requests_total/etc. Pass gincommon.MetricsRegisterer()
// and a merged map of gincommon.MetricsConstLabels() + {"environment": appEnv}.
//
// Idempotent via sync.Once: the first caller wins. Needed because the e2e test
// suite builds a fresh RouterConfig per test via the same composition-root call
// path — without this, the second test's call would panic with "duplicate
// metrics collector registration attempted".
func Register(reg prometheus.Registerer, constLabels prometheus.Labels) {
	registerOnce.Do(func() {
		registerOnto(reg, constLabels)
	})
}

func registerOnto(reg prometheus.Registerer, constLabels prometheus.Labels) {
	build(constLabels)
	reg.MustRegister(
		CacheHits, CacheMisses, WritesTotal, OptimisticLockConflicts,
		RequestsTotal, RequestDuration,
		DeprecatedCacheHits, DeprecatedCacheMisses, DeprecatedWritesTotal,
		DeprecatedOptimisticLockConflicts, DeprecatedRequestsTotal, DeprecatedRequestDuration,
	)
	// Pre-initialise known label values so dashboards show 0 rather than
	// "no data" before the first request.
	for _, key := range []string{"cat:departments", "cat:plans"} {
		CacheHits.WithLabelValues(key)
		CacheMisses.WithLabelValues(key)
		DeprecatedCacheHits.WithLabelValues(key)
		DeprecatedCacheMisses.WithLabelValues(key)
	}
	for _, table := range []string{"departments", "plans"} {
		OptimisticLockConflicts.WithLabelValues(table)
		DeprecatedOptimisticLockConflicts.WithLabelValues(table)
	}
	WritesTotal.WithLabelValues("departments", "insert")
	WritesTotal.WithLabelValues("departments", "update")
	WritesTotal.WithLabelValues("plans", "update")
	DeprecatedWritesTotal.WithLabelValues("departments", "insert")
	DeprecatedWritesTotal.WithLabelValues("departments", "update")
	DeprecatedWritesTotal.WithLabelValues("plans", "update")
}
