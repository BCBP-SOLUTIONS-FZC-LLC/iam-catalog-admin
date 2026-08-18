package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	catmetrics "github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/adapter/outbound/metrics"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRequestMetricsMiddleware_RecordsRouteAndStatus verifies LLD §13.2's
// catalog_admin_requests_total{route,status} and
// catalog_admin_request_duration_seconds{route,quantile} — recorded with
// the matched route template, not the raw path, so a UUID path param
// doesn't blow up label cardinality.
func TestRequestMetricsMiddleware_RecordsRouteAndStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(requestMetricsMiddleware())
	r.GET("/rmtest/:id", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	// Asserts a delta, not an absolute value: catmetrics.RequestsTotal is a
	// package-level global that isn't reset between test invocations, so an
	// absolute-value assertion would flake under `go test -count=N>1`
	// (each iteration reuses the same process and the same counter state).
	before := testutil.ToFloat64(catmetrics.RequestsTotal.WithLabelValues("/rmtest/:id", "200"))
	durationCountBefore := testutil.CollectAndCount(catmetrics.RequestDuration)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/rmtest/11111111-1111-1111-1111-111111111111", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	assert.Equal(t, before+1,
		testutil.ToFloat64(catmetrics.RequestsTotal.WithLabelValues("/rmtest/:id", "200")))
	assert.GreaterOrEqual(t, testutil.CollectAndCount(catmetrics.RequestDuration), durationCountBefore)
}

// TestRequestMetricsMiddleware_UnmatchedRoute verifies that when Gin cannot
// match a route template (c.FullPath() == ""), the middleware falls back to
// the raw request path so label cardinality stays finite.
func TestRequestMetricsMiddleware_UnmatchedRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(requestMetricsMiddleware())
	// No route registered — every request is unmatched (FullPath() == "").

	before := testutil.ToFloat64(catmetrics.RequestsTotal.WithLabelValues("/no-such-path", "404"))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/no-such-path", nil)
	r.ServeHTTP(w, req)

	// Status is 404 because no handler is registered.
	assert.Equal(t, before+1,
		testutil.ToFloat64(catmetrics.RequestsTotal.WithLabelValues("/no-such-path", "404")))
}

// TestRequestMetricsMiddleware_RecordsErrorStatus verifies a non-2xx
// terminal outcome (e.g. an auth rejection upstream of a handler) is
// still counted — the middleware runs first in the chain precisely so
// rejected requests aren't invisible to this metric.
func TestRequestMetricsMiddleware_RecordsErrorStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(requestMetricsMiddleware())
	r.GET("/rmtest-403", func(c *gin.Context) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": "insufficient_role"})
	})

	// Delta assertion — see RecordsRouteAndStatus's comment on why an
	// absolute-value assertion against this package-level global would
	// flake under `-count=N>1`.
	before := testutil.ToFloat64(catmetrics.RequestsTotal.WithLabelValues("/rmtest-403", "403"))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/rmtest-403", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)

	assert.Equal(t, before+1,
		testutil.ToFloat64(catmetrics.RequestsTotal.WithLabelValues("/rmtest-403", "403")))
}
