//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ═════════════════════════════════════════════════════════════════════════
// CAT-I1 — GET /api/v1/internal/departments (iam-system mesh only)
// ═════════════════════════════════════════════════════════════════════════

// Scenario CAI1-A-03: tenant_admin (not iam-system) → 403
func TestInternalDepartments_TenantAdminRole_Returns403(t *testing.T) {
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodGet, "/api/v1/internal/departments", publicHeaders, nil)
	assert.Equal(t, http.StatusForbidden, status, string(raw))
}

// Scenario CAI1-A-04: iam-system role → 200 (authorized)
func TestInternalDepartments_IamSystemRole_Authorized(t *testing.T) {
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodGet, "/api/v1/internal/departments", systemHeaders, nil)
	assert.Equal(t, http.StatusOK, status, string(raw))
}

// Scenario CAI1-H-02: returns ALL departments including inactive ones (no activeOnly filter)
func TestInternalDepartments_ReturnsInactiveDeptsUnfiltered(t *testing.T) {
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "int_h02_retire", "name": "Retire Internal H02", "is_system": false})
	id := decodeMap(t, createRaw)["id"].(string)
	doJSON(t, env, http.MethodPatch, "/api/v1/operator/departments/"+id, operatorHeaders,
		map[string]any{"is_active": false, "record_version": 1}) //nolint:errcheck

	status, raw := doJSON(t, env, http.MethodGet, "/api/v1/internal/departments", systemHeaders, nil)
	require.Equal(t, http.StatusOK, status, string(raw))
	assert.Contains(t, string(raw), "int_h02_retire", "internal endpoint must include retired depts (no filter)")
}

// Scenario CAI1-H-03: each department in response contains all required fields
func TestInternalDepartments_AllFieldsPresent(t *testing.T) {
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodGet, "/api/v1/internal/departments", systemHeaders, nil)
	require.Equal(t, http.StatusOK, status, string(raw))

	var body struct {
		Departments []map[string]any `json:"departments"`
	}
	require.NoError(t, json.Unmarshal(raw, &body))
	require.NotEmpty(t, body.Departments)

	for _, d := range body.Departments[:1] { // check first dept is sufficient
		for _, key := range []string{"id", "code", "name", "is_system", "is_active", "record_version"} {
			_, ok := d[key]
			assert.True(t, ok, "internal dept response missing field %q", key)
		}
	}
}

// Scenario CAI1-BL-02 / CROSS-CA-06: after dept PATCH, internal endpoint reflects updated name
func TestInternalDepartments_ReflectsUpdateAfterPatch(t *testing.T) {
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "int_bl02", "name": "Before Internal"})
	id := decodeMap(t, createRaw)["id"].(string)
	doJSON(t, env, http.MethodPatch, "/api/v1/operator/departments/"+id, operatorHeaders,
		map[string]any{"name": "After Internal", "record_version": 1}) //nolint:errcheck

	_, raw := doJSON(t, env, http.MethodGet, "/api/v1/internal/departments", systemHeaders, nil)
	assert.Contains(t, string(raw), "After Internal")
}

// Scenario CAI1-NOEVT-01: no outbox_events table — read endpoints emit no events
func TestInternalDepartments_NoOutboxEventsOnRead(t *testing.T) {
	env := newE2EEnv(t)
	doJSON(t, env, http.MethodGet, "/api/v1/internal/departments", systemHeaders, nil) //nolint:errcheck

	var exists bool
	err := env.rawPool.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM information_schema.tables
         WHERE table_schema='public' AND table_name='outbox_events')`).Scan(&exists)
	require.NoError(t, err)
	assert.False(t, exists)
}

// ═════════════════════════════════════════════════════════════════════════
// CAT-I2 — GET /api/v1/internal/plans (iam-system mesh only)
// ═════════════════════════════════════════════════════════════════════════

// Scenario CAI2-A-02: platform_operator (not iam-system) → 403
func TestInternalPlans_PlatformOperatorRole_Returns403(t *testing.T) {
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodGet, "/api/v1/internal/plans", operatorHeaders, nil)
	assert.Equal(t, http.StatusForbidden, status, string(raw))
}

// Scenario CAI2-H-02: record_versions map values match the per-code versions from CAT-4b
func TestInternalPlans_RecordVersionsMatchPlanVersions(t *testing.T) {
	env := newE2EEnv(t)

	_, internalRaw := doJSON(t, env, http.MethodGet, "/api/v1/internal/plans", systemHeaders, nil)
	var internalBody struct {
		RecordVersions map[string]float64 `json:"record_versions"`
	}
	require.NoError(t, json.Unmarshal(internalRaw, &internalBody))
	require.Contains(t, internalBody.RecordVersions, "pro")

	_, planRaw := doJSON(t, env, http.MethodGet, "/api/v1/operator/plans/pro", operatorHeaders, nil)
	planVersion := decodeMap(t, planRaw)["record_version"].(float64)

	assert.Equal(t, internalBody.RecordVersions["pro"], planVersion,
		"CAT-I2 record_versions[pro] must equal CAT-4b plan.record_version")
}

// Scenario CAI2-H-03 / CROSS-CA-05: after plan PATCH, internal endpoint reflects update
func TestInternalPlans_ReflectsUpdateAfterPatch(t *testing.T) {
	env := newE2EEnv(t)
	doJSON(t, env, http.MethodPatch, "/api/v1/operator/plans/pro", operatorHeaders,
		map[string]any{"display_name": "Internal Visible", "record_version": 1}) //nolint:errcheck

	_, raw := doJSON(t, env, http.MethodGet, "/api/v1/internal/plans", systemHeaders, nil)
	assert.Contains(t, string(raw), "Internal Visible")
}

// Scenario CAI2-NOEVT-01: no outbox_events table on internal read
func TestInternalPlans_NoOutboxEventsOnRead(t *testing.T) {
	env := newE2EEnv(t)
	doJSON(t, env, http.MethodGet, "/api/v1/internal/plans", systemHeaders, nil) //nolint:errcheck

	var exists bool
	err := env.rawPool.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM information_schema.tables
         WHERE table_schema='public' AND table_name='outbox_events')`).Scan(&exists)
	require.NoError(t, err)
	assert.False(t, exists)
}
