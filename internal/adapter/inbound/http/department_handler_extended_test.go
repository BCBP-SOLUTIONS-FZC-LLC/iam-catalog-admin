// Additional handler-level tests for DepartmentHandler.
// All tests use hand-wired service+fakes — no Postgres, no HTTP server.
// Patterns mirror department_handler_test.go.
package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── CA1 — POST /operator/departments ─────────────────────────────────

// Scenario CA1-A-02: no roles in context → 403
func TestDepartmentHandler_Create_NoRoles_Returns403(t *testing.T) {
	h, _ := newDepartmentHandlerForTest()
	body, _ := json.Marshal(DepartmentCreateRequest{Code: "X", Name: "X"})
	c, w := newTestContext(t, http.MethodPost, "/api/v1/operator/departments", body, []string{})
	h.Create(c)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// Scenario CA1-A-04: tenant_owner role → 403
func TestDepartmentHandler_Create_TenantOwnerRole_Returns403(t *testing.T) {
	h, _ := newDepartmentHandlerForTest()
	body, _ := json.Marshal(DepartmentCreateRequest{Code: "X", Name: "X"})
	c, w := newTestContext(t, http.MethodPost, "/api/v1/operator/departments", body, []string{"tenant_owner"})
	h.Create(c)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// Scenario CA1-V-06: extra unknown fields in body → ignored by binding → 201
func TestDepartmentHandler_Create_ExtraFieldsInBody_Ignored(t *testing.T) {
	h, _ := newDepartmentHandlerForTest()
	rawBody := []byte(`{"code":"ext-f01","name":"Extra Fields","unknown":"ignored","another":99}`)
	c, w := newTestContext(t, http.MethodPost, "/api/v1/operator/departments", rawBody, []string{"platform_operator"})
	h.Create(c)
	assert.Equal(t, http.StatusCreated, w.Code)
}

// Scenario CA1-M-01: malformed JSON → 400
func TestDepartmentHandler_Create_MalformedJSON_Returns400(t *testing.T) {
	h, _ := newDepartmentHandlerForTest()
	c, w := newTestContext(t, http.MethodPost, "/api/v1/operator/departments",
		[]byte(`{"code": bad json`), []string{"platform_operator"})
	h.Create(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// Scenario CA1-M-02: empty body {} → 400 (code and name required)
func TestDepartmentHandler_Create_EmptyBody_Returns400(t *testing.T) {
	h, _ := newDepartmentHandlerForTest()
	c, w := newTestContext(t, http.MethodPost, "/api/v1/operator/departments",
		[]byte(`{}`), []string{"platform_operator"})
	h.Create(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ── CA2 — PATCH /operator/departments/:id ────────────────────────────

// Scenario CA2-A-02: no roles → 403
func TestDepartmentHandler_Patch_NoRoles_Returns403(t *testing.T) {
	h, _ := newDepartmentHandlerForTest()
	body, _ := json.Marshal(DepartmentPatchRequest{RecordVersion: 1})
	c, w := newTestContext(t, http.MethodPatch, "/api/v1/operator/departments/abc", body, []string{})
	setParams(c, gin.Params{{Key: "id", Value: "11111111-1111-1111-1111-111111111111"}})
	h.Patch(c)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// Scenario CA2-A-03: tenant_admin → 403
func TestDepartmentHandler_Patch_TenantAdminRole_Returns403(t *testing.T) {
	h, _ := newDepartmentHandlerForTest()
	body, _ := json.Marshal(DepartmentPatchRequest{RecordVersion: 1})
	c, w := newTestContext(t, http.MethodPatch, "/api/v1/operator/departments/abc", body, []string{"tenant_admin"})
	setParams(c, gin.Params{{Key: "id", Value: "11111111-1111-1111-1111-111111111111"}})
	h.Patch(c)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// Scenario CA2-V-06: record_version as string "1" → 400 (type mismatch)
func TestDepartmentHandler_Patch_RecordVersionAsString_Returns400(t *testing.T) {
	h, _ := newDepartmentHandlerForTest()
	c, w := newTestContext(t, http.MethodPatch, "/api/v1/operator/departments/abc",
		[]byte(`{"name":"X","record_version":"1"}`), []string{"platform_operator"})
	setParams(c, gin.Params{{Key: "id", Value: "11111111-1111-1111-1111-111111111111"}})
	h.Patch(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// Scenario CA2-V-07: both code and is_system in body → 422 field_immutable (code checked first)
func TestDepartmentHandler_Patch_BothImmutableFields_CodeCheckedFirst_Returns422(t *testing.T) {
	h, svc := newDepartmentHandlerForTest()
	// Create a dept first
	createBody, _ := json.Marshal(DepartmentCreateRequest{Code: "IMM-BOTH", Name: "Both"})
	cCreate, wCreate := newTestContext(t, http.MethodPost, "/api/v1/operator/departments", createBody, []string{"platform_operator"})
	h.Create(cCreate)
	require.Equal(t, http.StatusCreated, wCreate.Code)
	_ = svc

	var created DepartmentResponse
	require.NoError(t, json.Unmarshal(wCreate.Body.Bytes(), &created))

	patchBody := []byte(`{"code":"CHANGED","is_system":true,"record_version":1}`)
	c, w := newTestContext(t, http.MethodPatch, "/api/v1/operator/departments/"+created.ID, patchBody, []string{"platform_operator"})
	setParams(c, gin.Params{{Key: "id", Value: created.ID}})
	h.Patch(c)
	require.Equal(t, http.StatusUnprocessableEntity, w.Code)
	var resp ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "field_immutable", resp.Code)
}

// Scenario CA2-M-01: malformed JSON body → 400
func TestDepartmentHandler_Patch_MalformedJSONBody_Returns400(t *testing.T) {
	h, _ := newDepartmentHandlerForTest()
	c, w := newTestContext(t, http.MethodPatch, "/api/v1/operator/departments/abc",
		[]byte(`{"name": bad`), []string{"platform_operator"})
	setParams(c, gin.Params{{Key: "id", Value: "11111111-1111-1111-1111-111111111111"}})
	h.Patch(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ── CA3 — DELETE /operator/departments/:id ────────────────────────────

// Scenario CA3-A-02: no roles → 403
func TestDepartmentHandler_Delete_NoRoles_Returns403(t *testing.T) {
	h, _ := newDepartmentHandlerForTest()
	c, w := newTestContext(t, http.MethodDelete, "/api/v1/operator/departments/abc", nil, []string{})
	setParams(c, gin.Params{{Key: "id", Value: "11111111-1111-1111-1111-111111111111"}})
	h.DeleteBlocked(c)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// Scenario CA3-A-03: tenant_admin → 403
func TestDepartmentHandler_Delete_TenantAdminRole_Returns403(t *testing.T) {
	h, _ := newDepartmentHandlerForTest()
	c, w := newTestContext(t, http.MethodDelete, "/api/v1/operator/departments/abc", nil, []string{"tenant_admin"})
	setParams(c, gin.Params{{Key: "id", Value: "11111111-1111-1111-1111-111111111111"}})
	h.DeleteBlocked(c)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// Scenario CA3-BL-03: operator + non-existent UUID → 405 (handler returns 405 before any DB lookup)
func TestDepartmentHandler_Delete_OperatorNonExistentUUID_Returns405(t *testing.T) {
	h, _ := newDepartmentHandlerForTest()
	c, w := newTestContext(t, http.MethodDelete, "/api/v1/operator/departments/abc", nil, []string{"platform_operator"})
	setParams(c, gin.Params{{Key: "id", Value: "00000000-0000-0000-0000-000000000099"}})
	h.DeleteBlocked(c)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// Scenario CA3-BL-04: operator + invalid UUID → 405 (405 before UUID parse)
func TestDepartmentHandler_Delete_OperatorInvalidUUID_Returns405(t *testing.T) {
	h, _ := newDepartmentHandlerForTest()
	c, w := newTestContext(t, http.MethodDelete, "/api/v1/operator/departments/not-uuid", nil, []string{"platform_operator"})
	setParams(c, gin.Params{{Key: "id", Value: "not-a-uuid"}})
	h.DeleteBlocked(c)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// ── CA6 — GET /departments (public) ──────────────────────────────────

// Scenario CA6-A-04: no roles still gets 200 (public endpoint)
func TestDepartmentHandler_List_NoRoles_Returns200(t *testing.T) {
	h, _ := newDepartmentHandlerForTest()
	c, w := newTestContext(t, http.MethodGet, "/api/v1/departments", nil, []string{})
	h.List(c)
	assert.Equal(t, http.StatusOK, w.Code)
}

// ── Response format ───────────────────────────────────────────────────

// Scenario CA-FMT-01: error response has {error, code, message, status}
func TestDepartmentHandler_ErrorResponse_HasRequiredFields(t *testing.T) {
	h, _ := newDepartmentHandlerForTest()
	c, w := newTestContext(t, http.MethodGet, "/api/v1/departments/not-a-uuid", nil, []string{})
	setParams(c, gin.Params{{Key: "id", Value: "not-a-uuid"}})
	h.Get(c)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	for _, field := range []string{"error", "code", "message", "status"} {
		assert.Contains(t, resp, field, "error response must have field %q", field)
	}
}

// Scenario CA-FMT-02: success response Content-Type is application/json
func TestDepartmentHandler_SuccessResponse_ContentTypeIsJSON(t *testing.T) {
	h, _ := newDepartmentHandlerForTest()
	c, w := newTestContext(t, http.MethodGet, "/api/v1/departments", nil, []string{})
	h.List(c)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")
}

// ── CA6 — List with non-empty result (for loop body coverage) ────────

// TestDepartmentHandler_List_WithNonEmptyResult exercises the for-range body
// inside List (line 52-54) that populates DepartmentResponse items.
func TestDepartmentHandler_List_WithNonEmptyResult(t *testing.T) {
	h, _ := newDepartmentHandlerForTest()

	createBody, _ := json.Marshal(DepartmentCreateRequest{Code: "SALES", Name: "Sales"})
	c1, w1 := newTestContext(t, http.MethodPost, "/api/v1/operator/departments", createBody, []string{"platform_operator"})
	h.Create(c1)
	require.Equal(t, http.StatusCreated, w1.Code)

	c2, w2 := newTestContext(t, http.MethodGet, "/api/v1/departments", nil, nil)
	h.List(c2)
	require.Equal(t, http.StatusOK, w2.Code)
	var resp DepartmentListResponse
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &resp))
	require.Len(t, resp.Items, 1)
	assert.Equal(t, "SALES", resp.Items[0].Code)
}

// ── CA2 — Patch success and optimistic-lock conflict paths ───────────

// TestDepartmentHandler_Patch_Success covers the happy-path lines inside Patch
// (metrics increment + 200 response, lines 182-183).
func TestDepartmentHandler_Patch_Success(t *testing.T) {
	h, _ := newDepartmentHandlerForTest()

	createBody, _ := json.Marshal(DepartmentCreateRequest{Code: "HR", Name: "Human Resources"})
	c1, w1 := newTestContext(t, http.MethodPost, "/api/v1/operator/departments", createBody, []string{"platform_operator"})
	h.Create(c1)
	require.Equal(t, http.StatusCreated, w1.Code)
	var created DepartmentResponse
	require.NoError(t, json.Unmarshal(w1.Body.Bytes(), &created))

	patchBody := []byte(`{"name":"HR Dept","record_version":1}`)
	c2, w2 := newTestContext(t, http.MethodPatch, "/api/v1/operator/departments/"+created.ID, patchBody, []string{"platform_operator"})
	setParams(c2, gin.Params{{Key: "id", Value: created.ID}})
	h.Patch(c2)
	require.Equal(t, http.StatusOK, w2.Code)
	var updated DepartmentResponse
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &updated))
	assert.Equal(t, "HR Dept", updated.Name)
}

// TestDepartmentHandler_Patch_OptimisticLockConflict covers the OLC metric
// increment (lines 176-178) triggered when record_version doesn't match.
func TestDepartmentHandler_Patch_OptimisticLockConflict(t *testing.T) {
	h, _ := newDepartmentHandlerForTest()

	createBody, _ := json.Marshal(DepartmentCreateRequest{Code: "MKTING", Name: "Marketing"})
	c1, w1 := newTestContext(t, http.MethodPost, "/api/v1/operator/departments", createBody, []string{"platform_operator"})
	h.Create(c1)
	require.Equal(t, http.StatusCreated, w1.Code)
	var created DepartmentResponse
	require.NoError(t, json.Unmarshal(w1.Body.Bytes(), &created))

	patchBody := []byte(`{"name":"Marketing Dept","record_version":999}`)
	c2, w2 := newTestContext(t, http.MethodPatch, "/api/v1/operator/departments/"+created.ID, patchBody, []string{"platform_operator"})
	setParams(c2, gin.Params{{Key: "id", Value: created.ID}})
	h.Patch(c2)
	assert.Equal(t, http.StatusConflict, w2.Code)
}

// ── helper: bytes.NewBufferString used in raw body tests ──────────────
var _ = bytes.NewBufferString
