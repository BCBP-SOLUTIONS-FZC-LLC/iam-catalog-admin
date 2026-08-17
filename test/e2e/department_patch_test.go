//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ═════════════════════════════════════════════════════════════════════════
// CAT-2 — PATCH /api/v1/operator/departments/:id
// ═════════════════════════════════════════════════════════════════════════

// Scenario CA2-A-01: no identity headers → 401
func TestPatchDepartment_NoIdentityHeaders_Returns401(t *testing.T) {
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "ptch_a01", "name": "No Auth"})
	id := decodeMap(t, createRaw)["id"].(string)

	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/departments/"+id, nil,
		map[string]any{"name": "Updated", "record_version": 1})
	assert.Equal(t, http.StatusUnauthorized, status, string(raw))
}

// Scenario CA2-A-02: identity present, no roles → 403
func TestPatchDepartment_NoRoles_Returns403(t *testing.T) {
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "ptch_a02", "name": "No Roles"})
	id := decodeMap(t, createRaw)["id"].(string)

	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/departments/"+id, func(req *http.Request) {
		req.Header.Set("x-user-id", "11111111-1111-1111-1111-111111111111")
		req.Header.Set("x-tenant-id", "22222222-2222-2222-2222-222222222222")
	}, map[string]any{"name": "Updated", "record_version": 1})
	assert.Equal(t, http.StatusForbidden, status, string(raw))
}

// Scenario CA2-A-03: tenant_admin role not sufficient → 403
func TestPatchDepartment_TenantAdminRole_Returns403(t *testing.T) {
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "ptch_a03", "name": "Admin Forbidden"})
	id := decodeMap(t, createRaw)["id"].(string)

	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/departments/"+id, publicHeaders,
		map[string]any{"name": "Updated", "record_version": 1})
	require.Equal(t, http.StatusForbidden, status, string(raw))
	assert.Equal(t, "insufficient_role", decodeMap(t, raw)["code"])
}

// Scenario CA2-A-04: platform_operator → 200 authorized
func TestPatchDepartment_OperatorRole_Authorized(t *testing.T) {
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "ptch_a04", "name": "Authorized"})
	id := decodeMap(t, createRaw)["id"].(string)

	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/departments/"+id, operatorHeaders,
		map[string]any{"name": "Updated A04", "record_version": 1})
	require.Equal(t, http.StatusOK, status, string(raw))
}

// Scenario CA2-H-02: retire non-system department (is_active=false) → 200
func TestPatchDepartment_RetireNonSystem_Returns200(t *testing.T) {
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "ptch_h02", "name": "Retire H02", "is_system": false})
	id := decodeMap(t, createRaw)["id"].(string)

	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/departments/"+id, operatorHeaders,
		map[string]any{"is_active": false, "record_version": 1})
	require.Equal(t, http.StatusOK, status, string(raw))
	assert.Equal(t, false, decodeMap(t, raw)["is_active"])
}

// Scenario CA2-H-03: re-activate a retired department → 200
func TestPatchDepartment_ReactivateRetiredDept_Returns200(t *testing.T) {
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "ptch_h03", "name": "Reactivate H03", "is_system": false})
	id := decodeMap(t, createRaw)["id"].(string)

	doJSON(t, env, http.MethodPatch, "/api/v1/operator/departments/"+id, operatorHeaders,
		map[string]any{"is_active": false, "record_version": 1}) //nolint:errcheck

	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/departments/"+id, operatorHeaders,
		map[string]any{"is_active": true, "record_version": 2})
	require.Equal(t, http.StatusOK, status, string(raw))
	assert.Equal(t, true, decodeMap(t, raw)["is_active"])
}

// Scenario CA2-H-04: update name and is_active in one request → both fields changed
func TestPatchDepartment_UpdateNameAndIsActive_BothChanged(t *testing.T) {
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "ptch_h04", "name": "Old Name H04", "is_system": false})
	id := decodeMap(t, createRaw)["id"].(string)

	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/departments/"+id, operatorHeaders,
		map[string]any{"name": "New Name H04", "is_active": false, "record_version": 1})
	require.Equal(t, http.StatusOK, status, string(raw))
	body := decodeMap(t, raw)
	assert.Equal(t, "New Name H04", body["name"])
	assert.Equal(t, false, body["is_active"])
}

// Scenario CA2-H-05: retire already-retired dept → 200 (idempotent)
func TestPatchDepartment_RetireAlreadyRetired_Idempotent(t *testing.T) {
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "ptch_h05", "name": "Retire Idempotent H05", "is_system": false})
	id := decodeMap(t, createRaw)["id"].(string)

	doJSON(t, env, http.MethodPatch, "/api/v1/operator/departments/"+id, operatorHeaders,
		map[string]any{"is_active": false, "record_version": 1}) //nolint:errcheck

	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/departments/"+id, operatorHeaders,
		map[string]any{"is_active": false, "record_version": 2})
	require.Equal(t, http.StatusOK, status, string(raw))
	assert.Equal(t, false, decodeMap(t, raw)["is_active"])
}

// Scenario CA2-H-06: reactivate already-active dept → 200 (idempotent)
func TestPatchDepartment_ReactivateAlreadyActive_Idempotent(t *testing.T) {
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "ptch_h06", "name": "Active Idempotent H06", "is_system": false})
	id := decodeMap(t, createRaw)["id"].(string)

	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/departments/"+id, operatorHeaders,
		map[string]any{"is_active": true, "record_version": 1})
	require.Equal(t, http.StatusOK, status, string(raw))
	assert.Equal(t, true, decodeMap(t, raw)["is_active"])
}

// Scenario CA2-V-01: non-UUID :id path param → 400
func TestPatchDepartment_NonUUIDPathParam_Returns400(t *testing.T) {
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/departments/not-a-uuid", operatorHeaders,
		map[string]any{"name": "X", "record_version": 1})
	assert.Equal(t, http.StatusBadRequest, status, string(raw))
}

// Scenario CA2-V-02: body has only record_version, no mutable fields → 400 no_mutable_field
func TestPatchDepartment_NoMutableFields_Returns400(t *testing.T) {
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "ptch_v02", "name": "No Mutable"})
	id := decodeMap(t, createRaw)["id"].(string)

	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/departments/"+id, operatorHeaders,
		map[string]any{"record_version": 1})
	require.Equal(t, http.StatusBadRequest, status, string(raw))
	assert.Equal(t, "no_mutable_field", decodeMap(t, raw)["code"])
}

// Scenario CA2-V-03: name=null explicitly → 400 no_mutable_field (JSON null → nil pointer)
func TestPatchDepartment_NameNullExplicit_Returns400(t *testing.T) {
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "ptch_v03", "name": "Name Null"})
	id := decodeMap(t, createRaw)["id"].(string)

	req, err := http.NewRequest(http.MethodPatch, env.baseURL+"/api/v1/operator/departments/"+id,
		bytes.NewBufferString(`{"name":null,"record_version":1}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	operatorHeaders(req)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	raw, _ := io.ReadAll(resp.Body)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode, string(raw))
	assert.Equal(t, "no_mutable_field", decodeMap(t, raw)["code"])
}

// Scenario CA2-V-04: is_active=null explicitly → 400
func TestPatchDepartment_IsActiveNullExplicit_Returns400(t *testing.T) {
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "ptch_v04", "name": "IsActive Null"})
	id := decodeMap(t, createRaw)["id"].(string)

	req, err := http.NewRequest(http.MethodPatch, env.baseURL+"/api/v1/operator/departments/"+id,
		bytes.NewBufferString(`{"is_active":null,"record_version":1}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	operatorHeaders(req)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	raw, _ := io.ReadAll(resp.Body)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode, string(raw))
}

// Scenario CA2-V-05: name="" (empty string) → 400
func TestPatchDepartment_EmptyName_Returns400(t *testing.T) {
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "ptch_v05", "name": "Empty Name"})
	id := decodeMap(t, createRaw)["id"].(string)

	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/departments/"+id, operatorHeaders,
		map[string]any{"name": "", "record_version": 1})
	assert.Equal(t, http.StatusBadRequest, status, string(raw))
}

// Scenario CA2-V-06: record_version as JSON string "1" (type mismatch) → 400
func TestPatchDepartment_RecordVersionAsString_Returns400(t *testing.T) {
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "ptch_v06", "name": "Version String"})
	id := decodeMap(t, createRaw)["id"].(string)

	req, err := http.NewRequest(http.MethodPatch, env.baseURL+"/api/v1/operator/departments/"+id,
		bytes.NewBufferString(`{"name":"X","record_version":"1"}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	operatorHeaders(req)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	raw, _ := io.ReadAll(resp.Body)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode, string(raw))
}

// Scenario CA2-V-07: both code and is_system in body → 422 field_immutable (code checked first)
func TestPatchDepartment_BothImmutableFields_Returns422(t *testing.T) {
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "ptch_v07", "name": "Both Immutable", "is_system": false})
	id := decodeMap(t, createRaw)["id"].(string)

	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/departments/"+id, operatorHeaders,
		map[string]any{"code": "changed", "is_system": true, "record_version": 1})
	require.Equal(t, http.StatusUnprocessableEntity, status, string(raw))
	assert.Equal(t, "field_immutable", decodeMap(t, raw)["code"])
}

// Scenario CA2-NF-02: well-formed UUID that does not exist → 404
func TestPatchDepartment_NonExistentUUID_Returns404(t *testing.T) {
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodPatch,
		"/api/v1/operator/departments/00000000-0000-0000-0000-000000000001", operatorHeaders,
		map[string]any{"name": "X", "record_version": 1})
	assert.Equal(t, http.StatusNotFound, status, string(raw))
}

// Scenario CA2-BL-05: cache invalidated after patch; next GET reflects updated name
func TestPatchDepartment_CacheInvalidated_NextGetReflectsUpdate(t *testing.T) {
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "ptch_bl05", "name": "Cache Before"})
	id := decodeMap(t, createRaw)["id"].(string)

	doJSON(t, env, http.MethodGet, "/api/v1/departments/"+id, publicHeaders, nil) //nolint:errcheck

	doJSON(t, env, http.MethodPatch, "/api/v1/operator/departments/"+id, operatorHeaders,
		map[string]any{"name": "Cache After", "record_version": 1}) //nolint:errcheck

	_, raw := doJSON(t, env, http.MethodGet, "/api/v1/departments/"+id, publicHeaders, nil)
	assert.Equal(t, "Cache After", decodeMap(t, raw)["name"])
}

// Scenario CA2-BL-06 / CA-DI-02 / CROSS-CA-02: retired dept excluded from active_only=true list
func TestPatchDepartment_RetiredDept_ExcludedFromActiveFilter(t *testing.T) {
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "ptch_bl06", "name": "Retire Filter", "is_system": false})
	id := decodeMap(t, createRaw)["id"].(string)

	doJSON(t, env, http.MethodPatch, "/api/v1/operator/departments/"+id, operatorHeaders,
		map[string]any{"is_active": false, "record_version": 1}) //nolint:errcheck

	_, raw := doJSON(t, env, http.MethodGet, "/api/v1/departments?active_only=true", publicHeaders, nil)
	var body struct {
		Items []map[string]any `json:"items"`
	}
	require.NoError(t, json.Unmarshal(raw, &body))
	for _, item := range body.Items {
		assert.NotEqual(t, "ptch_bl06", item["code"], "retired dept must not appear under active_only=true")
	}
}

// Scenario CA2-OL-02: record_version=0 when actual is ≥1 → 409 optimistic_lock_conflict
func TestPatchDepartment_VersionZero_Returns409(t *testing.T) {
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "ptch_ol02", "name": "Version Zero"})
	id := decodeMap(t, createRaw)["id"].(string)

	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/departments/"+id, operatorHeaders,
		map[string]any{"name": "Updated", "record_version": 0})
	require.Equal(t, http.StatusConflict, status, string(raw))
	assert.Equal(t, "optimistic_lock_conflict", decodeMap(t, raw)["code"])
}

// Scenario CA2-OL-03 / CA2-FMT-06: correct record_version → 200 with version incremented
func TestPatchDepartment_CorrectVersion_Returns200_VersionIncremented(t *testing.T) {
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "ptch_ol03", "name": "Correct Version"})
	id := decodeMap(t, createRaw)["id"].(string)

	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/departments/"+id, operatorHeaders,
		map[string]any{"name": "Updated OL03", "record_version": 1})
	require.Equal(t, http.StatusOK, status, string(raw))
	assert.EqualValues(t, 2, decodeMap(t, raw)["record_version"])
}

// Scenario CA2-M-01: malformed JSON → 400
func TestPatchDepartment_MalformedJSON_Returns400(t *testing.T) {
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "ptch_m01", "name": "Malformed"})
	id := decodeMap(t, createRaw)["id"].(string)

	req, err := http.NewRequest(http.MethodPatch, env.baseURL+"/api/v1/operator/departments/"+id,
		bytes.NewBufferString(`{"name": "bad json`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	operatorHeaders(req)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	raw, _ := io.ReadAll(resp.Body)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode, string(raw))
}

// Scenario CA2-M-02: empty body {} → 400 no_mutable_field
func TestPatchDepartment_EmptyBody_Returns400NoMutableField(t *testing.T) {
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "ptch_m02", "name": "Empty Body"})
	id := decodeMap(t, createRaw)["id"].(string)

	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/departments/"+id, operatorHeaders,
		map[string]any{})
	require.Equal(t, http.StatusBadRequest, status, string(raw))
	assert.Equal(t, "no_mutable_field", decodeMap(t, raw)["code"])
}

// Scenario CA2-CT-01: Content-Type: text/plain → 415
func TestPatchDepartment_WrongContentType_Returns415(t *testing.T) {
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "ptch_ct01", "name": "CT Test"})
	id := decodeMap(t, createRaw)["id"].(string)

	req, err := http.NewRequest(http.MethodPatch, env.baseURL+"/api/v1/operator/departments/"+id,
		bytes.NewBufferString(`{"name":"Updated","record_version":1}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "text/plain")
	operatorHeaders(req)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	raw, _ := io.ReadAll(resp.Body)
	assert.Equal(t, http.StatusUnsupportedMediaType, resp.StatusCode, string(raw))
}

// Scenario CA2-NOEVT-01: no outbox_events table after patch
func TestPatchDepartment_NoOutboxEventsEmitted(t *testing.T) {
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "ptch_noevt01", "name": "No Events"})
	id := decodeMap(t, createRaw)["id"].(string)
	doJSON(t, env, http.MethodPatch, "/api/v1/operator/departments/"+id, operatorHeaders,
		map[string]any{"name": "Updated", "record_version": 1}) //nolint:errcheck

	var exists bool
	err := env.rawPool.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM information_schema.tables
         WHERE table_schema='public' AND table_name='outbox_events')`).Scan(&exists)
	require.NoError(t, err)
	assert.False(t, exists, "outbox_events must not exist — no outbox runner in this service (LLD §10)")
}

// Scenario CA2-CON-01: concurrent PATCHes with same version → one 200, one 409
func TestPatchDepartment_ConcurrentSameVersion_ExactlyOneWins(t *testing.T) {
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "ptch_con01", "name": "Concurrent"})
	id := decodeMap(t, createRaw)["id"].(string)

	results := make([]int, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			status, _ := doJSON(t, env, http.MethodPatch, "/api/v1/operator/departments/"+id, operatorHeaders,
				map[string]any{"name": "Winner", "record_version": 1})
			results[idx] = status
		}(i)
	}
	wg.Wait()

	okCount, conflictCount := 0, 0
	for _, s := range results {
		switch s {
		case http.StatusOK:
			okCount++
		case http.StatusConflict:
			conflictCount++
		}
	}
	assert.Equal(t, 1, okCount, "exactly one goroutine must win the optimistic lock")
	assert.Equal(t, 1, conflictCount, "exactly one goroutine must receive 409 optimistic_lock_conflict")
}

// Scenario CA2-FMT-07: 409 OCC response includes record_version info
func TestPatchDepartment_OCCResponse_IncludesVersionInfo(t *testing.T) {
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "ptch_fmt07", "name": "OCC Format"})
	id := decodeMap(t, createRaw)["id"].(string)

	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/departments/"+id, operatorHeaders,
		map[string]any{"name": "X", "record_version": 99})
	require.Equal(t, http.StatusConflict, status, string(raw))

	body := decodeMap(t, raw)
	assert.Equal(t, "optimistic_lock_conflict", body["code"])
	hasVersion := body["record_version"] != nil
	if details, ok := body["details"].(map[string]any); ok {
		hasVersion = hasVersion || details["record_version"] != nil || details["current_version"] != nil
	}
	assert.True(t, hasVersion, "409 OCC response must surface current record_version: %s", string(raw))
}

// Scenario CA-BL-01: record_version missing from body → decoded as 0 → 409 (NOT 400)
func TestPatchDepartment_MissingRecordVersion_Returns409NotBadRequest(t *testing.T) {
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "ptch_bl01x", "name": "Missing Version"})
	id := decodeMap(t, createRaw)["id"].(string)

	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/departments/"+id, operatorHeaders,
		map[string]any{"name": "Updated"}) // no record_version → decoded as 0
	require.Equal(t, http.StatusConflict, status, string(raw))
	assert.Equal(t, "optimistic_lock_conflict", decodeMap(t, raw)["code"])
}

// Scenario CA-DI-01: retired dept is still readable by ID via CAT-7
func TestPatchDepartment_RetiredDept_StillReadableByID(t *testing.T) {
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "ptch_di01", "name": "Retire Readable", "is_system": false})
	id := decodeMap(t, createRaw)["id"].(string)

	doJSON(t, env, http.MethodPatch, "/api/v1/operator/departments/"+id, operatorHeaders,
		map[string]any{"is_active": false, "record_version": 1}) //nolint:errcheck

	status, raw := doJSON(t, env, http.MethodGet, "/api/v1/departments/"+id, publicHeaders, nil)
	require.Equal(t, http.StatusOK, status, string(raw))
	body := decodeMap(t, raw)
	assert.Equal(t, "ptch_di01", body["code"])
	assert.Equal(t, false, body["is_active"], "retired dept must still be readable by ID")
}

// Scenario CA-DI-03: patching with new data increments record_version.
// The DB trigger fires WHEN (OLD.* IS DISTINCT FROM NEW.*), so the name
// must differ from the current value to trigger the version bump.
func TestPatchDepartment_DataChange_RecordVersionIncrements(t *testing.T) {
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "ptch_di03", "name": "Original Name"})
	id := decodeMap(t, createRaw)["id"].(string)

	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/departments/"+id, operatorHeaders,
		map[string]any{"name": "Updated Name", "record_version": 1})
	require.Equal(t, http.StatusOK, status, string(raw))
	assert.EqualValues(t, 2, decodeMap(t, raw)["record_version"],
		"record_version must increment when data changes")
}
