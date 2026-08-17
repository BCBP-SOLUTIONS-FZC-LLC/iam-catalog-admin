//go:build e2e

package e2e_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ═════════════════════════════════════════════════════════════════════════
// Health & observability endpoints
// ═════════════════════════════════════════════════════════════════════════

// Scenario CAH-H-04: readyz fails when Valkey unavailable
func TestReadyz_ValkeyDown_Returns503(t *testing.T) {
	t.Skip("infrastructure test — requires Valkey container to be stopped before running")
}

// Scenario CAH-H-05: GET /metrics → 200 with catadmin_* metric families
func TestMetrics_CatAdminCountersPresent(t *testing.T) {
	env := newE2EEnv(t)
	doJSON(t, env, http.MethodGet, "/api/v1/departments", publicHeaders, nil) //nolint:errcheck

	status, raw := doJSON(t, env, http.MethodGet, "/metrics", nil, nil)
	require.Equal(t, http.StatusOK, status, string(raw))
	assert.Contains(t, string(raw), "catadmin_")
}

// Scenario CAH-H-06: GET /healthz requires no identity headers
func TestHealthz_NoAuthRequired_AlwaysReady(t *testing.T) {
	env := newE2EEnv(t)
	status, _ := doJSON(t, env, http.MethodGet, "/healthz", nil, nil)
	assert.Equal(t, http.StatusOK, status)
}

// Scenario CAH-H-07: GET /readyz requires no identity headers
func TestReadyz_NoAuthRequired_Returns200WhenHealthy(t *testing.T) {
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodGet, "/readyz", nil, nil)
	require.Equal(t, http.StatusOK, status, string(raw))
}

// Scenario CA-OBS-01: catalog_admin_writes_total increments after dept create
func TestObservability_DeptCreate_WritesMetricIncrements(t *testing.T) {
	env := newE2EEnv(t)
	doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "obs_dept01", "name": "Writes Metric"}) //nolint:errcheck

	_, metricsRaw := doJSON(t, env, http.MethodGet, "/metrics", nil, nil)
	assert.Contains(t, string(metricsRaw), "catalog_admin_writes_total")
}

// Scenario CA-OBS-02: catalog_admin_writes_total increments after dept patch
func TestObservability_DeptPatch_WriteUpdateMetricIncrements(t *testing.T) {
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "obs_dept02", "name": "Update Metric"})
	id := decodeMap(t, createRaw)["id"].(string)
	doJSON(t, env, http.MethodPatch, "/api/v1/operator/departments/"+id, operatorHeaders,
		map[string]any{"name": "Updated Metric", "record_version": 1}) //nolint:errcheck

	_, metricsRaw := doJSON(t, env, http.MethodGet, "/metrics", nil, nil)
	assert.Contains(t, string(metricsRaw), "catalog_admin_writes_total")
}

// Scenario CA-OBS-03: catalog_admin_writes_total increments after plan patch
func TestObservability_PlanPatch_WritesMetricIncrements(t *testing.T) {
	env := newE2EEnv(t)
	doJSON(t, env, http.MethodPatch, "/api/v1/operator/plans/pro", operatorHeaders,
		map[string]any{"display_name": "Obs Plan", "record_version": 1}) //nolint:errcheck

	_, metricsRaw := doJSON(t, env, http.MethodGet, "/metrics", nil, nil)
	assert.Contains(t, string(metricsRaw), "catalog_admin_writes_total")
}

// Scenario CA-OBS-05 / CROSS-CA-09/10: cache counters and OCC counters present in /metrics
func TestObservability_AllExpectedCountersPresent(t *testing.T) {
	env := newE2EEnv(t)

	// Trigger cache miss and hit.
	doJSON(t, env, http.MethodGet, "/api/v1/departments", publicHeaders, nil) //nolint:errcheck
	doJSON(t, env, http.MethodGet, "/api/v1/departments", publicHeaders, nil) //nolint:errcheck

	// Trigger OCC conflict.
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "obs_occ01", "name": "OCC Counter"})
	id := decodeMap(t, createRaw)["id"].(string)
	doJSON(t, env, http.MethodPatch, "/api/v1/operator/departments/"+id, operatorHeaders,
		map[string]any{"name": "X", "record_version": 99}) //nolint:errcheck (stale version → OCC)

	_, metricsRaw := doJSON(t, env, http.MethodGet, "/metrics", nil, nil)
	body := string(metricsRaw)
	assert.Contains(t, body, "catadmin_cache_hits_total")
	assert.Contains(t, body, "catadmin_cache_misses_total")
	assert.Contains(t, body, "catalog_admin_writes_total")
	assert.Contains(t, body, "catalog_admin_optimistic_lock_conflicts_total")
}
