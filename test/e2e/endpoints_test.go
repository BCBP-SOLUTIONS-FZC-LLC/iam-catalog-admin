//go:build e2e

package e2e_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// doJSON issues an HTTP request against env.baseURL with the given
// header-setter applied, an optional JSON body, and returns the decoded
// status code + raw response body.
func doJSON(t *testing.T, env *e2eEnv, method, path string, headers func(*http.Request), body any) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequest(method, env.baseURL+path, reader)
	require.NoError(t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if headers != nil {
		headers(req)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, raw
}

// fetchMetrics returns the Prometheus text exposition served by env's
// dedicated metrics server (mirrors production's separate METRICS_PORT
// listener — router.Handler() has no /metrics route of its own).
func fetchMetrics(t *testing.T, env *e2eEnv) string {
	t.Helper()
	resp, err := http.Get(env.metricsURL + "/metrics")
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	require.Equal(t, http.StatusOK, resp.StatusCode)
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(raw)
}

func decodeMap(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))
	return m
}

// ═════════════════════════════════════════════════════════════════════════
// Health / readiness
// ═════════════════════════════════════════════════════════════════════════

// Test Case ID:      CAT-E2E-001
// Module:            iam-catalog-admin · Infra
// Feature:           /healthz and /readyz respond 200 against a live DB+cache
// API:               GET /healthz, GET /readyz
// Scenario:          Happy path
// Priority: P1 · Severity: Blocker · Automation Status: Automated
func TestE2E001_HealthAndReadyz(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)

	status, _ := doJSON(t, env, http.MethodGet, "/healthz", nil, nil)
	assert.Equal(t, http.StatusOK, status)

	status, raw := doJSON(t, env, http.MethodGet, "/readyz", nil, nil)
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, "ready", decodeMap(t, raw)["status"])
}

// ═════════════════════════════════════════════════════════════════════════
// CAT-1 — POST /operator/departments
// ═════════════════════════════════════════════════════════════════════════

// Test Case ID:      CAT-E2E-010
// Module:            iam-catalog-admin · Departments
// Feature:           CAT-1 · Create a global-catalog department
// API:               POST /api/v1/operator/departments
// Scenario:          Happy path — operator creates a non-system department
// Priority: P1 · Severity: Blocker · Automation Status: Automated
func TestE2E010_CreateDepartment_Happy(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)

	status, raw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "LEGAL_E2E_010", "name": "Legal", "is_system": false})
	require.Equal(t, http.StatusCreated, status, string(raw))

	body := decodeMap(t, raw)
	assert.Equal(t, "LEGAL_E2E_010", body["code"])
	assert.EqualValues(t, 1, body["record_version"])
	assert.NotEmpty(t, body["id"])
}

// Test Case ID:      CAT-E2E-011
// Feature:           CAT-1 · Non-operator caller rejected
// Scenario:          Negative — tenant_admin role lacks platform_operator
// Expected Result:   403 insufficient_role
// Priority: P1 · Severity: Blocker · Automation Status: Automated
func TestE2E011_CreateDepartment_RequiresOperatorRole(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)

	status, raw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", publicHeaders,
		map[string]any{"code": "X_011", "name": "X"})
	require.Equal(t, http.StatusForbidden, status, string(raw))
	assert.Equal(t, "insufficient_role", decodeMap(t, raw)["code"])
}

// Test Case ID:      CAT-E2E-012
// Feature:           CAT-1 · Duplicate code → 409 conflict
// Scenario:          Negative — code collides with a pre-existing (or
//
//	newly created) row.
//
// Priority: P1 · Severity: Major · Automation Status: Automated
func TestE2E012_CreateDepartment_DuplicateCode_Returns409(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)

	status1, _ := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "DUP_012", "name": "First"})
	require.Equal(t, http.StatusCreated, status1)

	status2, raw2 := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "DUP_012", "name": "Second"})
	require.Equal(t, http.StatusConflict, status2, string(raw2))
	assert.Equal(t, "duplicate_code", decodeMap(t, raw2)["code"])
}

// Test Case ID:      CAT-E2E-013
// Feature:           CAT-1 · Missing required field → 400 validation_error
// Priority: P2 · Severity: Major · Automation Status: Automated
func TestE2E013_CreateDepartment_MissingName_Returns400(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)

	status, raw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "NONAME_013"})
	require.Equal(t, http.StatusBadRequest, status, string(raw))
}

// ═════════════════════════════════════════════════════════════════════════
// CAT-2 — PATCH /operator/departments/:id
// ═════════════════════════════════════════════════════════════════════════

// Test Case ID:      CAT-E2E-020
// Feature:           CAT-2 · Rename + optimistic-lock success path
// Priority: P1 · Severity: Blocker · Automation Status: Automated
func TestE2E020_PatchDepartment_Happy(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)

	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "RENAME_020", "name": "Old Name"})
	id := decodeMap(t, createRaw)["id"].(string)

	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/departments/"+id, operatorHeaders,
		map[string]any{"name": "New Name", "record_version": 1})
	require.Equal(t, http.StatusOK, status, string(raw))
	body := decodeMap(t, raw)
	assert.Equal(t, "New Name", body["name"])
	assert.EqualValues(t, 2, body["record_version"])
}

// Test Case ID:      CAT-E2E-021
// Feature:           CAT-2 · code is immutable → 422 field_immutable
// Priority: P1 · Severity: Major · Automation Status: Automated
func TestE2E021_PatchDepartment_ImmutableCode_Returns422(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "IMM_021", "name": "Immutable"})
	id := decodeMap(t, createRaw)["id"].(string)

	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/departments/"+id, operatorHeaders,
		map[string]any{"code": "CHANGED", "record_version": 1})
	require.Equal(t, http.StatusUnprocessableEntity, status, string(raw))
	assert.Equal(t, "field_immutable", decodeMap(t, raw)["code"])
}

// Test Case ID:      CAT-E2E-022
// Feature:           CAT-2 · is_system is immutable → 422 field_immutable
// Priority: P1 · Severity: Major · Automation Status: Automated
func TestE2E022_PatchDepartment_ImmutableIsSystem_Returns422(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "IMM_022", "name": "Immutable", "is_system": false})
	id := decodeMap(t, createRaw)["id"].(string)

	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/departments/"+id, operatorHeaders,
		map[string]any{"is_system": true, "record_version": 1})
	require.Equal(t, http.StatusUnprocessableEntity, status, string(raw))
	assert.Equal(t, "field_immutable", decodeMap(t, raw)["code"])
}

// Test Case ID:      CAT-E2E-023
// Feature:           CAT-2 · retiring a system department → 422 (D-7/OP invariant)
// Priority: P1 · Severity: Blocker · Automation Status: Automated
func TestE2E023_PatchDepartment_SystemDeptCannotBeRetired(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "SYS_023", "name": "System Dept", "is_system": true})
	id := decodeMap(t, createRaw)["id"].(string)

	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/departments/"+id, operatorHeaders,
		map[string]any{"is_active": false, "record_version": 1})
	require.Equal(t, http.StatusUnprocessableEntity, status, string(raw))
	assert.Equal(t, "system_department_cannot_be_retired", decodeMap(t, raw)["code"])
}

// Test Case ID:      CAT-E2E-024
// Feature:           CAT-2 · stale record_version → 409 optimistic_lock_conflict
// Priority: P1 · Severity: Major · Automation Status: Automated
func TestE2E024_PatchDepartment_StaleVersion_Returns409(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "STALE_024", "name": "Stale"})
	id := decodeMap(t, createRaw)["id"].(string)

	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/departments/"+id, operatorHeaders,
		map[string]any{"name": "X", "record_version": 99})
	require.Equal(t, http.StatusConflict, status, string(raw))
	assert.Equal(t, "optimistic_lock_conflict", decodeMap(t, raw)["code"])
}

// Test Case ID:      CAT-E2E-025
// Feature:           CAT-2 · unknown department id → 404
// Priority: P2 · Severity: Major · Automation Status: Automated
func TestE2E025_PatchDepartment_NotFound(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodPatch,
		"/api/v1/operator/departments/11111111-1111-1111-1111-111111111111", operatorHeaders,
		map[string]any{"name": "X", "record_version": 1})
	require.Equal(t, http.StatusNotFound, status, string(raw))
}

// ═════════════════════════════════════════════════════════════════════════
// CAT-3 — DELETE /operator/departments/:id → always 405
// ═════════════════════════════════════════════════════════════════════════

// Test Case ID:      CAT-E2E-030
// Feature:           CAT-3 · hard delete always blocked (D-4/OP-3)
// Priority: P1 · Severity: Blocker · Automation Status: Automated
func TestE2E030_DeleteDepartment_Always405(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "DEL_030", "name": "Del"})
	id := decodeMap(t, createRaw)["id"].(string)

	status, raw := doJSON(t, env, http.MethodDelete, "/api/v1/operator/departments/"+id, operatorHeaders, nil)
	require.Equal(t, http.StatusMethodNotAllowed, status, string(raw))
	body := decodeMap(t, raw)
	assert.Equal(t, "method_not_allowed", body["code"])
	assert.Equal(t, "method_not_allowed", body["error"])
	assert.EqualValues(t, http.StatusMethodNotAllowed, body["status"])
}

// ═════════════════════════════════════════════════════════════════════════
// CAT-6/CAT-7 — public department reads
// ═════════════════════════════════════════════════════════════════════════

// Test Case ID:      CAT-E2E-040
// Feature:           CAT-6 · public list, any authenticated caller, includes seeded system depts
// Priority: P1 · Severity: Blocker · Automation Status: Automated
func TestE2E040_ListDepartments_PublicIncludesSeeded(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodGet, "/api/v1/departments", publicHeaders, nil)
	require.Equal(t, http.StatusOK, status, string(raw))
	var body struct {
		Items []map[string]any `json:"items"`
	}
	require.NoError(t, json.Unmarshal(raw, &body))
	assert.GreaterOrEqual(t, len(body.Items), 5, "5 system departments are seeded by migration 000001_init_schema")
}

// Test Case ID:      CAT-E2E-041
// Feature:           CAT-6 · active_only=true filters out retired departments
// Priority: P2 · Severity: Major · Automation Status: Automated
func TestE2E041_ListDepartments_ActiveOnlyFilter(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "RETIRED_041", "name": "Retired"})
	id := decodeMap(t, createRaw)["id"].(string)
	patchStatus, _ := doJSON(t, env, http.MethodPatch, "/api/v1/operator/departments/"+id, operatorHeaders,
		map[string]any{"is_active": false, "record_version": 1})
	require.Equal(t, http.StatusOK, patchStatus)

	status, raw := doJSON(t, env, http.MethodGet, "/api/v1/departments?active_only=true", publicHeaders, nil)
	require.Equal(t, http.StatusOK, status)
	var body struct {
		Items []map[string]any `json:"items"`
	}
	require.NoError(t, json.Unmarshal(raw, &body))
	for _, item := range body.Items {
		assert.NotEqual(t, "RETIRED_041", item["code"], "retired department must not appear under active_only=true")
	}
}

// Test Case ID:      CAT-E2E-042
// Feature:           CAT-7 · single department read
// Priority: P1 · Severity: Blocker · Automation Status: Automated
func TestE2E042_GetDepartment_Happy(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "GET_042", "name": "Gettable"})
	id := decodeMap(t, createRaw)["id"].(string)

	status, raw := doJSON(t, env, http.MethodGet, "/api/v1/departments/"+id, publicHeaders, nil)
	require.Equal(t, http.StatusOK, status, string(raw))
	assert.Equal(t, "GET_042", decodeMap(t, raw)["code"])
}

// Test Case ID:      CAT-E2E-043
// Feature:           CAT-7 · unknown id → 404
// Priority: P2 · Severity: Major · Automation Status: Automated
func TestE2E043_GetDepartment_NotFound(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, _ := doJSON(t, env, http.MethodGet,
		"/api/v1/departments/11111111-1111-1111-1111-111111111111", publicHeaders, nil)
	assert.Equal(t, http.StatusNotFound, status)
}

// ═════════════════════════════════════════════════════════════════════════
// CAT-4 — GET /operator/plans[/:code]
// ═════════════════════════════════════════════════════════════════════════

// Test Case ID:      CAT-E2E-050
// Feature:           CAT-4 · list returns exactly the 3 seeded tiers
// Priority: P1 · Severity: Blocker · Automation Status: Automated
func TestE2E050_ListPlans_SeededTiers(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodGet, "/api/v1/operator/plans", operatorHeaders, nil)
	require.Equal(t, http.StatusOK, status, string(raw))
	var body struct {
		Items []map[string]any `json:"items"`
	}
	require.NoError(t, json.Unmarshal(raw, &body))
	require.Len(t, body.Items, 3)
	codes := map[string]bool{}
	for _, p := range body.Items {
		codes[p["code"].(string)] = true
	}
	assert.True(t, codes["starter"])
	assert.True(t, codes["pro"])
	assert.True(t, codes["enterprise"])
}

// Test Case ID:      CAT-E2E-051
// Feature:           CAT-4 · enterprise ships NULL limits (CAT-D6, unlimited)
// Priority: P1 · Severity: Major · Automation Status: Automated
func TestE2E051_GetPlan_EnterpriseUnlimited(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodGet, "/api/v1/operator/plans/enterprise", operatorHeaders, nil)
	require.Equal(t, http.StatusOK, status, string(raw))
	body := decodeMap(t, raw)
	assert.Nil(t, body["workflow_template_limit"])
	assert.Nil(t, body["tender_limit"])
}

// Test Case ID:      CAT-E2E-052
// Feature:           CAT-4 · unknown plan code → 404
// Priority: P2 · Severity: Major · Automation Status: Automated
func TestE2E052_GetPlan_UnknownCode_Returns404(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, _ := doJSON(t, env, http.MethodGet, "/api/v1/operator/plans/bogus", operatorHeaders, nil)
	assert.Equal(t, http.StatusNotFound, status)
}

// Test Case ID:      CAT-E2E-053
// Feature:           CAT-4 · non-operator caller rejected
// Priority: P1 · Severity: Blocker · Automation Status: Automated
func TestE2E053_ListPlans_RequiresOperatorRole(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, _ := doJSON(t, env, http.MethodGet, "/api/v1/operator/plans", publicHeaders, nil)
	assert.Equal(t, http.StatusForbidden, status)
}

// ═════════════════════════════════════════════════════════════════════════
// CAT-5 — PATCH /operator/plans/:code
// ═════════════════════════════════════════════════════════════════════════

// Test Case ID:      CAT-E2E-060
// Feature:           CAT-5 · explicit limit value + branding update
// Priority: P1 · Severity: Blocker · Automation Status: Automated
func TestE2E060_PatchPlan_Happy(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/plans/starter", operatorHeaders,
		map[string]any{"workflow_template_limit": 7, "custom_branding": "logo", "record_version": 1})
	require.Equal(t, http.StatusOK, status, string(raw))
	body := decodeMap(t, raw)
	assert.EqualValues(t, 7, body["workflow_template_limit"])
	assert.Equal(t, "logo", body["custom_branding"])
	assert.EqualValues(t, 2, body["record_version"])
}

// Test Case ID:      CAT-E2E-061
// Feature:           CAT-5 · explicit null → unlimited (CAT-D6 tri-state parsing)
// Priority: P1 · Severity: Major · Automation Status: Automated
func TestE2E061_PatchPlan_NullLimitMeansUnlimited(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/plans/pro", operatorHeaders,
		map[string]any{"tender_limit": nil, "record_version": 1})
	require.Equal(t, http.StatusOK, status, string(raw))
	assert.Nil(t, decodeMap(t, raw)["tender_limit"])
}

// Test Case ID:      CAT-E2E-062
// Feature:           CAT-5 · absent field is left untouched (tri-state: absent ≠ null)
// Priority: P1 · Severity: Major · Automation Status: Automated
func TestE2E062_PatchPlan_AbsentFieldUnchanged(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	// starter seeds workflow_template_limit=5; patch only display_name.
	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/plans/starter", operatorHeaders,
		map[string]any{"display_name": "Starter Plus", "record_version": 1})
	require.Equal(t, http.StatusOK, status, string(raw))
	body := decodeMap(t, raw)
	assert.Equal(t, "Starter Plus", body["display_name"])
	assert.EqualValues(t, 5, body["workflow_template_limit"], "field absent from the patch body must be left untouched")
}

// Test Case ID:      CAT-E2E-063
// Feature:           CAT-5 · stale record_version → 409
// Priority: P1 · Severity: Major · Automation Status: Automated
func TestE2E063_PatchPlan_StaleVersion_Returns409(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/plans/starter", operatorHeaders,
		map[string]any{"display_name": "X", "record_version": 99})
	require.Equal(t, http.StatusConflict, status, string(raw))
	assert.Equal(t, "optimistic_lock_conflict", decodeMap(t, raw)["code"])
}

// Test Case ID:      CAT-E2E-064
// Feature:           CAT-5 · unknown plan code → 404
// Priority: P2 · Severity: Major · Automation Status: Automated
func TestE2E064_PatchPlan_UnknownCode_Returns404(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, _ := doJSON(t, env, http.MethodPatch, "/api/v1/operator/plans/bogus", operatorHeaders,
		map[string]any{"display_name": "X", "record_version": 1})
	assert.Equal(t, http.StatusNotFound, status)
}

// Test Case ID:      CAT-E2E-065
// Feature:           CAT-5 · negative limit rejected → 400
// Priority: P2 · Severity: Major · Automation Status: Automated
func TestE2E065_PatchPlan_NegativeLimit_Returns400(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, _ := doJSON(t, env, http.MethodPatch, "/api/v1/operator/plans/starter", operatorHeaders,
		map[string]any{"workflow_template_limit": -1, "record_version": 1})
	assert.Equal(t, http.StatusBadRequest, status)
}

// Test Case ID:      CAT-E2E-066
// Feature:           CAT-5 · non-scalar feature_set value → 400 invalid_feature_value (PLAN-6(d))
// Priority: P1 · Severity: Major · Automation Status: Automated
func TestE2E066_PatchPlan_NonScalarFeatureSetValue_Returns400InvalidFeatureValue(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/plans/starter", operatorHeaders,
		map[string]any{"feature_set": map[string]any{"nested": map[string]any{"a": 1}}, "record_version": 1})
	require.Equal(t, http.StatusBadRequest, status, string(raw))
	body := decodeMap(t, raw)
	assert.Equal(t, "invalid_feature_value", body["code"])
	assert.Equal(t, "invalid_feature_value", body["error"])
}

// ═════════════════════════════════════════════════════════════════════════
// CAT-I1/CAT-I2 — internal mesh-only bulk reads
// ═════════════════════════════════════════════════════════════════════════

// Test Case ID:      CAT-E2E-070
// Feature:           CAT-I1 · bulk department read, mesh-only (iam-system)
// Priority: P1 · Severity: Blocker · Automation Status: Automated
func TestE2E070_InternalDepartments_Happy(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodGet, "/api/v1/internal/departments", systemHeaders, nil)
	require.Equal(t, http.StatusOK, status, string(raw))
	var body struct {
		Departments []map[string]any `json:"departments"`
		AsOf        string           `json:"as_of"`
	}
	require.NoError(t, json.Unmarshal(raw, &body))
	assert.GreaterOrEqual(t, len(body.Departments), 5)
	assert.NotEmpty(t, body.AsOf)
}

// Test Case ID:      CAT-E2E-071
// Feature:           CAT-I1 · non-system caller rejected
// Priority: P1 · Severity: Blocker · Automation Status: Automated
func TestE2E071_InternalDepartments_RequiresSystemRole(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, _ := doJSON(t, env, http.MethodGet, "/api/v1/internal/departments", operatorHeaders, nil)
	assert.Equal(t, http.StatusForbidden, status, "platform_operator is not iam-system — CAT-I1 is mesh-only")
}

// Test Case ID:      CAT-E2E-072
// Feature:           CAT-I2 · bulk plan read includes record_versions map
// Priority: P1 · Severity: Blocker · Automation Status: Automated
func TestE2E072_InternalPlans_Happy(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodGet, "/api/v1/internal/plans", systemHeaders, nil)
	require.Equal(t, http.StatusOK, status, string(raw))
	var body struct {
		Plans          []map[string]any `json:"plans"`
		RecordVersions map[string]int64 `json:"record_versions"`
	}
	require.NoError(t, json.Unmarshal(raw, &body))
	require.Len(t, body.Plans, 3)
	assert.Contains(t, body.RecordVersions, "starter")
	assert.Contains(t, body.RecordVersions, "pro")
	assert.Contains(t, body.RecordVersions, "enterprise")
}

// Test Case ID:      CAT-E2E-073
// Feature:           CAT-I2 · non-system caller rejected
// Priority: P1 · Severity: Blocker · Automation Status: Automated
func TestE2E073_InternalPlans_RequiresSystemRole(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, _ := doJSON(t, env, http.MethodGet, "/api/v1/internal/plans", publicHeaders, nil)
	assert.Equal(t, http.StatusForbidden, status)
}

// ═════════════════════════════════════════════════════════════════════════
// Cross-cutting — identity headers
// ═════════════════════════════════════════════════════════════════════════

// Test Case ID:      CAT-E2E-080
// Feature:           Missing identity headers entirely → 401
// Priority: P1 · Severity: Blocker · Automation Status: Automated
func TestE2E080_MissingIdentityHeaders_Returns401(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, _ := doJSON(t, env, http.MethodGet, "/api/v1/departments", nil, nil)
	assert.Equal(t, http.StatusUnauthorized, status)
}

// Test Case ID:      CAT-E2E-081
// Feature:           Malformed x-user-id header → 401 missing_identity_headers
// Priority: P2 · Severity: Major · Automation Status: Automated
func TestE2E081_MalformedUserIDHeader_Returns401(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodGet, "/api/v1/departments", func(req *http.Request) {
		req.Header.Set("x-user-id", "not-a-uuid")
		req.Header.Set("x-tenant-id", "22222222-2222-2222-2222-222222222222")
	}, nil)
	require.Equal(t, http.StatusUnauthorized, status, string(raw))
	assert.Equal(t, "missing_identity_headers", decodeMap(t, raw)["code"])
}

// ═════════════════════════════════════════════════════════════════════════
// Full read-cutover flow — the exact shape Core Org & Membership / Group
// Mapping Service will depend on once wired (LLD §7, §8, §12 Phase 2).
// ═════════════════════════════════════════════════════════════════════════

// Test Case ID:      CAT-E2E-090
// Feature:           A CAT-1 write is immediately visible via CAT-6, CAT-7,
//
//	and CAT-I1 — proves the write path and every read path share
//	one Postgres source of truth (CAT-FAIL-1), independent of
//	the 60s cat:* cache TTL.
//
// Priority: P1 · Severity: Blocker · Automation Status: Automated
func TestE2E090_CreateThenVisibleAcrossAllReadPaths(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	code := "FLOW_090"

	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": code, "name": "Flow Dept"})
	id := decodeMap(t, createRaw)["id"].(string)

	// CAT-7 direct get.
	status, raw := doJSON(t, env, http.MethodGet, "/api/v1/departments/"+id, publicHeaders, nil)
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, code, decodeMap(t, raw)["code"])

	// CAT-6 public list.
	status, raw = doJSON(t, env, http.MethodGet, "/api/v1/departments", publicHeaders, nil)
	require.Equal(t, http.StatusOK, status)
	assert.Contains(t, string(raw), code)

	// CAT-I1 internal bulk (the exact seam Core/Group-Mapping consume).
	status, raw = doJSON(t, env, http.MethodGet, "/api/v1/internal/departments", systemHeaders, nil)
	require.Equal(t, http.StatusOK, status)
	assert.Contains(t, string(raw), code)
}

// Test Case ID:      CAT-E2E-091
// Feature:           A CAT-5 plan patch is immediately visible via CAT-4
//
//	and CAT-I2 (the om:plans population seam).
//
// Priority: P1 · Severity: Blocker · Automation Status: Automated
func TestE2E091_PatchPlanThenVisibleAcrossAllReadPaths(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)

	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/plans/enterprise", operatorHeaders,
		map[string]any{"sso_enabled": false, "record_version": 1})
	require.Equal(t, http.StatusOK, status, string(raw))

	status, raw = doJSON(t, env, http.MethodGet, "/api/v1/operator/plans/enterprise", operatorHeaders, nil)
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, false, decodeMap(t, raw)["sso_enabled"])

	status, raw = doJSON(t, env, http.MethodGet, "/api/v1/internal/plans", systemHeaders, nil)
	require.Equal(t, http.StatusOK, status)
	var body struct {
		Plans []map[string]any `json:"plans"`
	}
	require.NoError(t, json.Unmarshal(raw, &body))
	found := false
	for _, p := range body.Plans {
		if p["code"] == "enterprise" {
			found = true
			assert.Equal(t, false, p["sso_enabled"])
		}
	}
	assert.True(t, found, fmt.Sprintf("enterprise plan missing from CAT-I2 response: %s", raw))
}

// Test Case ID:      CAT-E2E-092
// Feature:           CAT-2 · renaming a system department → 422 system_name_immutable (D-11)
// Priority: P1 · Severity: Major · Automation Status: Automated
func TestE2E092_PatchDepartment_SystemDeptRename_Returns422SystemNameImmutable(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "SYS_092", "name": "System Dept", "is_system": true})
	id := decodeMap(t, createRaw)["id"].(string)

	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/departments/"+id, operatorHeaders,
		map[string]any{"name": "Renamed", "record_version": 1})
	require.Equal(t, http.StatusUnprocessableEntity, status, string(raw))
	assert.Equal(t, "system_name_immutable", decodeMap(t, raw)["code"])
}
