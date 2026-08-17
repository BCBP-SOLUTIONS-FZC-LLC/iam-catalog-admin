//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ═════════════════════════════════════════════════════════════════════════
// CAT-1 — POST /api/v1/operator/departments
// ═════════════════════════════════════════════════════════════════════════

// Scenario CA1-A-01: no identity headers → 401
func TestCreateDepartment_NoIdentityHeaders_Returns401(t *testing.T) {
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", nil,
		map[string]any{"code": "crt_a01", "name": "No Auth"})
	assert.Equal(t, http.StatusUnauthorized, status, string(raw))
}

// Scenario CA1-A-02: identity present but no roles → 403
func TestCreateDepartment_NoRoles_Returns403(t *testing.T) {
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", func(req *http.Request) {
		req.Header.Set("x-user-id", "11111111-1111-1111-1111-111111111111")
		req.Header.Set("x-tenant-id", "22222222-2222-2222-2222-222222222222")
	}, map[string]any{"code": "crt_a02", "name": "No Roles"})
	assert.Equal(t, http.StatusForbidden, status, string(raw))
}

// Scenario CA1-A-04: tenant_owner role is not sufficient → 403
func TestCreateDepartment_TenantOwnerRole_Returns403(t *testing.T) {
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", func(req *http.Request) {
		req.Header.Set("x-user-id", "11111111-1111-1111-1111-111111111111")
		req.Header.Set("x-tenant-id", "22222222-2222-2222-2222-222222222222")
		req.Header.Set("x-tenant-roles", "tenant_owner")
	}, map[string]any{"code": "crt_a04", "name": "Owner Forbidden"})
	require.Equal(t, http.StatusForbidden, status, string(raw))
	assert.Equal(t, "insufficient_role", decodeMap(t, raw)["code"])
}

// Scenario CA1-A-03/A-05 (authorization confirmed): tenant_admin → 403; operator → 201
func TestCreateDepartment_TenantAdminForbidden_OperatorAuthorized(t *testing.T) {
	env := newE2EEnv(t)

	adminStatus, _ := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", publicHeaders,
		map[string]any{"code": "crt_a03_admin", "name": "Admin Forbidden"})
	assert.Equal(t, http.StatusForbidden, adminStatus)

	opStatus, _ := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "crt_a05_op", "name": "Operator Authorized"})
	assert.Equal(t, http.StatusCreated, opStatus)
}

// Scenario CA1-H-02: is_system=true → 201 with is_system=true in response
func TestCreateDepartment_SystemDept_IsSystemTrue(t *testing.T) {
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "crt_h02_sys", "name": "System Dept H02", "is_system": true})
	require.Equal(t, http.StatusCreated, status, string(raw))
	body := decodeMap(t, raw)
	assert.Equal(t, true, body["is_system"])
	assert.Equal(t, "crt_h02_sys", body["code"])
}

// Scenario CA1-H-03: is_system omitted → defaults to false
func TestCreateDepartment_IsSystemOmitted_DefaultsFalse(t *testing.T) {
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "crt_h03", "name": "No IsSystem H03"})
	require.Equal(t, http.StatusCreated, status, string(raw))
	assert.Equal(t, false, decodeMap(t, raw)["is_system"])
}

// Scenario CA1-V-01: code field missing entirely → 400
func TestCreateDepartment_MissingCode_Returns400(t *testing.T) {
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"name": "No Code"})
	assert.Equal(t, http.StatusBadRequest, status, string(raw))
}

// Scenario CA1-V-02: code="" (empty string) → 400
func TestCreateDepartment_EmptyCode_Returns400(t *testing.T) {
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "", "name": "Empty Code"})
	assert.Equal(t, http.StatusBadRequest, status, string(raw))
}

// Scenario CA1-V-04: name="" (empty string) → 400
func TestCreateDepartment_EmptyName_Returns400(t *testing.T) {
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "crt_v04", "name": ""})
	assert.Equal(t, http.StatusBadRequest, status, string(raw))
}

// Scenario CA1-V-05: code="   " (whitespace only) → 400
func TestCreateDepartment_WhitespaceOnlyCode_Returns400(t *testing.T) {
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "   ", "name": "Whitespace Code"})
	assert.Equal(t, http.StatusBadRequest, status, string(raw))
}

// Scenario CA1-V-06: extra unknown fields in body → ignored, 201
func TestCreateDepartment_ExtraUnknownFields_Ignored(t *testing.T) {
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "crt_v06", "name": "Extra Fields", "unknown": "ignored", "another": 99})
	assert.Equal(t, http.StatusCreated, status, string(raw))
}

// Scenario CA1-CON-02: same code, different name → 409 conflict
func TestCreateDepartment_SameCodeDifferentName_Returns409(t *testing.T) {
	env := newE2EEnv(t)
	status1, _ := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "crt_con02", "name": "First"})
	require.Equal(t, http.StatusCreated, status1)

	status2, raw2 := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "crt_con02", "name": "Second"})
	require.Equal(t, http.StatusConflict, status2, string(raw2))
}

// Scenario CA1-CON-03 / CROSS-CA-03: retired dept code is still unique — cannot reuse
func TestCreateDepartment_RetiredCode_StillBlocked(t *testing.T) {
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "crt_con03", "name": "To Retire", "is_system": false})
	id := decodeMap(t, createRaw)["id"].(string)

	retireStatus, _ := doJSON(t, env, http.MethodPatch, "/api/v1/operator/departments/"+id, operatorHeaders,
		map[string]any{"is_active": false, "record_version": 1})
	require.Equal(t, http.StatusOK, retireStatus)

	status, raw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "crt_con03", "name": "Reuse Attempt"})
	require.Equal(t, http.StatusConflict, status, string(raw))
}

// Scenario CA1-M-01: malformed JSON body → 400
func TestCreateDepartment_MalformedJSON_Returns400(t *testing.T) {
	env := newE2EEnv(t)
	req, err := http.NewRequest(http.MethodPost, env.baseURL+"/api/v1/operator/departments",
		bytes.NewBufferString(`{"code": bad json`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	operatorHeaders(req)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	raw, _ := io.ReadAll(resp.Body)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode, string(raw))
}

// Scenario CA1-M-02: empty body {} → 400
func TestCreateDepartment_EmptyBody_Returns400(t *testing.T) {
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{})
	assert.Equal(t, http.StatusBadRequest, status, string(raw))
}

// Scenario CA1-CT-01: Content-Type: text/plain → 415
func TestCreateDepartment_WrongContentType_Returns415(t *testing.T) {
	env := newE2EEnv(t)
	req, err := http.NewRequest(http.MethodPost, env.baseURL+"/api/v1/operator/departments",
		bytes.NewBufferString(`{"code":"crt_ct01","name":"CT Test"}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "text/plain")
	operatorHeaders(req)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	raw, _ := io.ReadAll(resp.Body)
	assert.Equal(t, http.StatusUnsupportedMediaType, resp.StatusCode, string(raw))
}

// Scenario CA1-NOEVT-01: catalog-admin has no outbox — outbox_events table must not exist
func TestCreateDepartment_NoOutboxEventsEmitted(t *testing.T) {
	env := newE2EEnv(t)
	status, _ := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "crt_noevt01", "name": "No Events"})
	require.Equal(t, http.StatusCreated, status)

	var exists bool
	err := env.rawPool.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM information_schema.tables
         WHERE table_schema='public' AND table_name='outbox_events')`).Scan(&exists)
	require.NoError(t, err)
	assert.False(t, exists, "outbox_events table must not exist — this service has no outbox runner (LLD §10)")
}

// Scenario CA1-CON-04: concurrent POSTs with same code → exactly one 201, one 409
func TestCreateDepartment_ConcurrentSameCode_ExactlyOneWins(t *testing.T) {
	env := newE2EEnv(t)
	results := make([]int, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			status, _ := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
				map[string]any{"code": "crt_con04_race", "name": "Concurrent"})
			results[idx] = status
		}(i)
	}
	wg.Wait()

	okCount, conflictCount := 0, 0
	for _, s := range results {
		switch s {
		case http.StatusCreated:
			okCount++
		case http.StatusConflict:
			conflictCount++
		}
	}
	assert.Equal(t, 1, okCount, "exactly one goroutine must succeed with 201")
	assert.Equal(t, 1, conflictCount, "exactly one goroutine must receive 409 conflict")
}

// Scenario CA1-FMT-04/05: POST response always has record_version=1 and is_active=true
func TestCreateDepartment_Response_RecordVersionOneAndIsActiveTrue(t *testing.T) {
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "crt_fmt04", "name": "Record Version"})
	require.Equal(t, http.StatusCreated, status, string(raw))
	body := decodeMap(t, raw)
	assert.EqualValues(t, 1, body["record_version"], "record_version must be 1 on creation")
	assert.Equal(t, true, body["is_active"], "is_active must be true on creation")
}

// Scenario CA1-FMT-08: round-trip data integrity — sent values match returned values
func TestCreateDepartment_RoundTrip_DataMatchesRequest(t *testing.T) {
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "crt_fmt08_code", "name": "FMT08 Round Trip"})
	require.Equal(t, http.StatusCreated, status, string(raw))
	body := decodeMap(t, raw)
	assert.Equal(t, "crt_fmt08_code", body["code"])
	assert.Equal(t, "FMT08 Round Trip", body["name"])
}

// Scenario CA1-FMT-09: special characters in name stored and returned correctly
func TestCreateDepartment_SpecialCharsInName_StoredCorrectly(t *testing.T) {
	env := newE2EEnv(t)
	name := "Finance & Accounting (O'Brien)"
	status, raw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "crt_fmt09", "name": name})
	require.Equal(t, http.StatusCreated, status, string(raw))
	assert.Equal(t, name, decodeMap(t, raw)["name"])
}
