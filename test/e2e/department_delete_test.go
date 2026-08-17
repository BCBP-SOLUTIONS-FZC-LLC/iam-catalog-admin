//go:build e2e

package e2e_test

import (
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ═════════════════════════════════════════════════════════════════════════
// CAT-3 — DELETE /api/v1/operator/departments/:id (always blocked)
// ═════════════════════════════════════════════════════════════════════════

// Scenario CA3-A-01: no identity headers → 401 (auth fires before 405)
func TestDeleteDepartment_NoIdentityHeaders_Returns401(t *testing.T) {
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "del_a01", "name": "No Auth"})
	id := decodeMap(t, createRaw)["id"].(string)

	req, err := http.NewRequest(http.MethodDelete, env.baseURL+"/api/v1/operator/departments/"+id, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "auth must fire before 405")
}

// Scenario CA3-A-02: identity present but no roles → 403
func TestDeleteDepartment_NoRoles_Returns403(t *testing.T) {
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "del_a02", "name": "No Roles"})
	id := decodeMap(t, createRaw)["id"].(string)

	req, err := http.NewRequest(http.MethodDelete, env.baseURL+"/api/v1/operator/departments/"+id, nil)
	require.NoError(t, err)
	req.Header.Set("x-user-id", "11111111-1111-1111-1111-111111111111")
	req.Header.Set("x-tenant-id", "22222222-2222-2222-2222-222222222222")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

// Scenario CA3-A-03: tenant_admin role → 403
func TestDeleteDepartment_TenantAdminRole_Returns403(t *testing.T) {
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "del_a03", "name": "Admin Forbidden"})
	id := decodeMap(t, createRaw)["id"].(string)

	req, err := http.NewRequest(http.MethodDelete, env.baseURL+"/api/v1/operator/departments/"+id, nil)
	require.NoError(t, err)
	req.Header.Set("x-user-id", "11111111-1111-1111-1111-111111111111")
	req.Header.Set("x-tenant-id", "22222222-2222-2222-2222-222222222222")
	req.Header.Set("x-tenant-roles", "tenant_admin")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

// Scenario CA3-BL-02: DELETE system dept → 405 unconditional
func TestDeleteDepartment_SystemDept_Returns405(t *testing.T) {
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "del_bl02_sys", "name": "System Delete", "is_system": true})
	id := decodeMap(t, createRaw)["id"].(string)

	req, err := http.NewRequest(http.MethodDelete, env.baseURL+"/api/v1/operator/departments/"+id, nil)
	require.NoError(t, err)
	operatorHeaders(req)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode, "DELETE system dept must still return 405")
}

// Scenario CA3-BL-03: DELETE non-existent UUID → 405 (handler returns before any DB lookup)
func TestDeleteDepartment_NonExistentUUID_Returns405(t *testing.T) {
	env := newE2EEnv(t)
	req, err := http.NewRequest(http.MethodDelete,
		env.baseURL+"/api/v1/operator/departments/00000000-0000-0000-0000-000000000099", nil)
	require.NoError(t, err)
	operatorHeaders(req)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode, "405 returned before any DB lookup")
}

// Scenario CA3-BL-04: invalid UUID path param + operator role → 405
func TestDeleteDepartment_InvalidUUIDParam_Returns405(t *testing.T) {
	env := newE2EEnv(t)
	req, err := http.NewRequest(http.MethodDelete,
		env.baseURL+"/api/v1/operator/departments/not-a-uuid", nil)
	require.NoError(t, err)
	operatorHeaders(req)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

// Scenario CA3-CT-01: no Content-Type header → still 405 (not 415); DELETE skips content-type check
func TestDeleteDepartment_NoContentType_Returns405NotUnsupportedMediaType(t *testing.T) {
	env := newE2EEnv(t)
	_, createRaw := doJSON(t, env, http.MethodPost, "/api/v1/operator/departments", operatorHeaders,
		map[string]any{"code": "del_ct01", "name": "CT Delete"})
	id := decodeMap(t, createRaw)["id"].(string)

	req, err := http.NewRequest(http.MethodDelete, env.baseURL+"/api/v1/operator/departments/"+id, nil)
	require.NoError(t, err)
	operatorHeaders(req)
	// No Content-Type set — DELETE has no body, RequireJSONContentType is skipped.
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	raw, _ := io.ReadAll(resp.Body)
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode,
		"DELETE returns 405, never 415: %s", string(raw))
}
