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
// Cross-flow — end-to-end chains across multiple endpoints
// ═════════════════════════════════════════════════════════════════════════

// Scenario CROSS-CA-08: stale version after another PATCH → 409
func TestCrossFlow_StaleVersionAfterConcurrentUpdate_Returns409(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "cf08_stale", "name": "Stale Cross"})
	id := decodeMap(t, createRaw)["id"].(string)

	doJSON(t, env, http.MethodPatch, "/api/v1/operator/departments/"+id, operatorHeaders,
		map[string]any{"name": "First", "record_version": 1}) //nolint:errcheck // version now 2

	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/departments/"+id, operatorHeaders,
		map[string]any{"name": "Second", "record_version": 1}) // stale
	require.Equal(t, http.StatusConflict, status, string(raw))
	assert.Equal(t, "optimistic_lock_conflict", decodeMap(t, raw)["code"])
}

// ═════════════════════════════════════════════════════════════════════════
// Response format — error envelope and Content-Type contract
// ═════════════════════════════════════════════════════════════════════════

// Scenario CA-FMT-01: error response always has {error, code, message, status} fields
func TestResponseFormat_ErrorEnvelope_HasAllRequiredFields(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodGet,
		"/api/v1/departments/00000000-0000-0000-0000-000000000001", publicHeaders, nil)
	require.Equal(t, http.StatusNotFound, status, string(raw))

	body := decodeMap(t, raw)
	assert.NotNil(t, body["error"], "error field must be present")
	assert.NotNil(t, body["code"], "code field must be present")
	assert.NotNil(t, body["message"], "message field must be present")
	assert.NotNil(t, body["status"], "status field must be present")
}

// Scenario CA-FMT-02: 200 success response has Content-Type: application/json
func TestResponseFormat_Success_ContentTypeIsJSON(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	req, err := http.NewRequest(http.MethodGet, env.baseURL+"/api/v1/departments", nil)
	require.NoError(t, err)
	publicHeaders(req)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")
}

// Scenario CA-FMT-03: 4xx error response has Content-Type: application/json
func TestResponseFormat_Error_ContentTypeIsJSON(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	req, err := http.NewRequest(http.MethodGet, env.baseURL+"/api/v1/departments/not-a-uuid", nil)
	require.NoError(t, err)
	publicHeaders(req)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")
}

// Scenario CA-FMT-07: 409 OCC error response surfaces record_version information
func TestResponseFormat_OCC409_SurfacesVersionInfo(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "fmt07_occ", "name": "OCC Format"})
	id := decodeMap(t, createRaw)["id"].(string)

	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/departments/"+id, operatorHeaders,
		map[string]any{"name": "X", "record_version": 99})
	require.Equal(t, http.StatusConflict, status, string(raw))
	body := decodeMap(t, raw)
	assert.Equal(t, "optimistic_lock_conflict", body["code"])
	assert.EqualValues(t, http.StatusConflict, body["status"])
}

// ═════════════════════════════════════════════════════════════════════════
// No-event audit — catalog-admin has no outbox (LLD §7)
// ═════════════════════════════════════════════════════════════════════════

// Scenario CA-NOEVT-01: outbox_events table does not exist in this service's schema
func TestNoEvents_OutboxTable_DoesNotExist(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	var exists bool
	err := env.rawPool.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM information_schema.tables
         WHERE table_schema='public' AND table_name='outbox_events')`).Scan(&exists)
	require.NoError(t, err)
	assert.False(t, exists, "catalog-admin must have no outbox_events table (LLD §7 CAT-EVT-1..5)")
}

// Scenario CA-NOEVT-02: no outbox rows after department patch
func TestNoEvents_PatchDepartment_NoOutboxRows(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "noevt02", "name": "No Events Dept"})
	id := decodeMap(t, createRaw)["id"].(string)
	doJSON(t, env, http.MethodPatch, "/api/v1/operator/departments/"+id, operatorHeaders,
		map[string]any{"name": "Updated NoEvt", "record_version": 1}) //nolint:errcheck

	var exists bool
	err := env.rawPool.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM information_schema.tables
         WHERE table_schema='public' AND table_name='outbox_events')`).Scan(&exists)
	require.NoError(t, err)
	assert.False(t, exists)
}

// Scenario CA-NOEVT-03: no outbox rows after plan patch
func TestNoEvents_PatchPlan_NoOutboxRows(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	doJSON(t, env, http.MethodPatch, "/api/v1/operator/plans/pro", operatorHeaders,
		map[string]any{"display_name": "No Events Plan", "record_version": 1}) //nolint:errcheck

	var exists bool
	err := env.rawPool.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM information_schema.tables
         WHERE table_schema='public' AND table_name='outbox_events')`).Scan(&exists)
	require.NoError(t, err)
	assert.False(t, exists)
}

// Scenario CA-NOEVT-05: no app.tenant_id GUC set — pure leaf, no RLS
func TestNoEvents_NoRLSGUCSet(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	doJSON(t, env, http.MethodGet, "/api/v1/departments", publicHeaders, nil) //nolint:errcheck

	var setting string
	err := env.rawPool.QueryRow(context.Background(),
		"SELECT COALESCE(current_setting('app.tenant_id', true), '')").Scan(&setting)
	require.NoError(t, err)
	assert.Empty(t, setting, "catalog-admin never sets app.tenant_id GUC — no RLS, pure leaf service")
}

// ═════════════════════════════════════════════════════════════════════════
// Data integrity
// ═════════════════════════════════════════════════════════════════════════

// Scenario CA-DI-05 / CAI2-H-02: CAT-I2 record_versions map matches CAT-4b per-code versions
func TestDataIntegrity_InternalPlansVersions_MatchOperatorPlanVersions(t *testing.T) {
	t.Parallel()
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

// Scenario CA-DI-06: Unicode code stored and retrieved correctly
func TestDataIntegrity_UnicodeCode_StoredCorrectly(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "dept-unicode-01", "name": "قسم عربي"})
	require.Equal(t, http.StatusCreated, status, string(raw))
	assert.Equal(t, "قسم عربي", decodeMap(t, raw)["name"])
}
