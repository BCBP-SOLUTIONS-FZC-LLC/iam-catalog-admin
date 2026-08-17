// Additional handler-level tests for PlanHandler.
// All tests use hand-wired service+fakes — no Postgres, no HTTP server.
package http

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── CA4A — GET /operator/plans ────────────────────────────────────────

// Scenario CA4A-A-01: no identity → 403 (no roles in test context)
func TestPlanHandler_List_NoRoles_Returns403(t *testing.T) {
	h := NewPlanHandler(newTestPlanService())
	c, w := newTestContext(t, http.MethodGet, "/api/v1/operator/plans", nil, []string{})
	h.List(c)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// Scenario CA4A-A-03: tenant_admin → 403
func TestPlanHandler_List_TenantAdminRole_Returns403(t *testing.T) {
	h := NewPlanHandler(newTestPlanService())
	c, w := newTestContext(t, http.MethodGet, "/api/v1/operator/plans", nil, []string{"tenant_admin"})
	h.List(c)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// Scenario CA4A-H-02: all plan fields present in response
func TestPlanHandler_List_AllPlanFieldsPresent(t *testing.T) {
	h := NewPlanHandler(newTestPlanService())
	c, w := newTestContext(t, http.MethodGet, "/api/v1/operator/plans", nil, []string{"platform_operator"})
	h.List(c)
	require.Equal(t, http.StatusOK, w.Code)
	var resp PlansListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Items)
	for _, plan := range resp.Items {
		assert.NotEmpty(t, plan.Code, "plan must have code")
		assert.NotEmpty(t, plan.DisplayName, "plan must have display_name")
	}
}

// ── CA4B — GET /operator/plans/:code ─────────────────────────────────

// Scenario CA4B-A-01: no roles → 403
func TestPlanHandler_Get_NoRoles_Returns403(t *testing.T) {
	h := NewPlanHandler(newTestPlanService())
	c, w := newTestContext(t, http.MethodGet, "/api/v1/operator/plans/starter", nil, []string{})
	setParams(c, gin.Params{{Key: "code", Value: "starter"}})
	h.Get(c)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// Scenario CA4B-V-02: uppercase code → 404 (case-sensitive lookup)
func TestPlanHandler_Get_UppercaseCode_Returns404(t *testing.T) {
	h := NewPlanHandler(newTestPlanService())
	c, w := newTestContext(t, http.MethodGet, "/api/v1/operator/plans/STARTER", nil, []string{"platform_operator"})
	setParams(c, gin.Params{{Key: "code", Value: "STARTER"}})
	h.Get(c)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// ── CA5 — PATCH /operator/plans/:code ────────────────────────────────

// Scenario CA5-A-02: no roles → 403
func TestPlanHandler_Patch_NoRoles_Returns403(t *testing.T) {
	h := NewPlanHandler(newTestPlanService())
	body := []byte(`{"display_name":"X","record_version":1}`)
	c, w := newTestContext(t, http.MethodPatch, "/api/v1/operator/plans/pro", body, []string{})
	setParams(c, gin.Params{{Key: "code", Value: "pro"}})
	h.Patch(c)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// Scenario CA5-A-03: tenant_admin → 403
func TestPlanHandler_Patch_TenantAdminRole_Returns403(t *testing.T) {
	h := NewPlanHandler(newTestPlanService())
	body := []byte(`{"display_name":"X","record_version":1}`)
	c, w := newTestContext(t, http.MethodPatch, "/api/v1/operator/plans/pro", body, []string{"tenant_admin"})
	setParams(c, gin.Params{{Key: "code", Value: "pro"}})
	h.Patch(c)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// Scenario CA5-V-08: body is JSON array → 400
func TestPlanHandler_Patch_BodyIsArray_Returns400(t *testing.T) {
	h := NewPlanHandler(newTestPlanService())
	c, w := newTestContext(t, http.MethodPatch, "/api/v1/operator/plans/starter",
		[]byte(`[]`), []string{"platform_operator"})
	setParams(c, gin.Params{{Key: "code", Value: "starter"}})
	h.Patch(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// Scenario CA5-M-01: malformed JSON → 400
func TestPlanHandler_Patch_MalformedJSON_Returns400(t *testing.T) {
	h := NewPlanHandler(newTestPlanService())
	c, w := newTestContext(t, http.MethodPatch, "/api/v1/operator/plans/starter",
		[]byte(`{bad json`), []string{"platform_operator"})
	setParams(c, gin.Params{{Key: "code", Value: "starter"}})
	h.Patch(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// Scenario CA5-H-19: code field in request body → ignored (path code wins)
func TestPlanHandler_Patch_CodeInBody_Ignored(t *testing.T) {
	h := NewPlanHandler(newTestPlanService())
	body := []byte(`{"code":"enterprise","display_name":"Code Ignored","record_version":1}`)
	c, w := newTestContext(t, http.MethodPatch, "/api/v1/operator/plans/starter", body, []string{"platform_operator"})
	setParams(c, gin.Params{{Key: "code", Value: "starter"}})
	h.Patch(c)
	require.Equal(t, http.StatusOK, w.Code)
	var resp PlanResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "starter", string(resp.Code), "code in body must be ignored — path code wins")
}

// Scenario CA5-H-06: workflow_template_limit=0 stored as 0, NOT null
func TestPlanHandler_Patch_ZeroWorkflowLimit_NotNull(t *testing.T) {
	h := NewPlanHandler(newTestPlanService())
	body := []byte(`{"workflow_template_limit":0,"record_version":1}`)
	c, w := newTestContext(t, http.MethodPatch, "/api/v1/operator/plans/starter", body, []string{"platform_operator"})
	setParams(c, gin.Params{{Key: "code", Value: "starter"}})
	h.Patch(c)
	require.Equal(t, http.StatusOK, w.Code)
	var resp PlanResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotNil(t, resp.WorkflowTemplateLimit, "0 must be stored as 0, not null")
	assert.EqualValues(t, 0, *resp.WorkflowTemplateLimit)
}

// Scenario CA5-BL-01: field absent from body → unchanged (tri-state: absent ≠ null)
func TestPlanHandler_Patch_AbsentField_LeftUnchanged(t *testing.T) {
	h := NewPlanHandler(newTestPlanService())
	// starter is seeded with workflow_template_limit=5; patch only display_name
	body := []byte(`{"display_name":"Name Only","record_version":1}`)
	c, w := newTestContext(t, http.MethodPatch, "/api/v1/operator/plans/starter", body, []string{"platform_operator"})
	setParams(c, gin.Params{{Key: "code", Value: "starter"}})
	h.Patch(c)
	require.Equal(t, http.StatusOK, w.Code)
	var resp PlanResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "Name Only", resp.DisplayName)
	require.NotNil(t, resp.WorkflowTemplateLimit, "absent field must be left unchanged")
	assert.EqualValues(t, 5, *resp.WorkflowTemplateLimit)
}

// Scenario CA-FMT-03: error response Content-Type is application/json
func TestPlanHandler_ErrorResponse_ContentTypeIsJSON(t *testing.T) {
	h := NewPlanHandler(newTestPlanService())
	c, w := newTestContext(t, http.MethodGet, "/api/v1/operator/plans/bogus", nil, []string{"platform_operator"})
	setParams(c, gin.Params{{Key: "code", Value: "bogus"}})
	h.Get(c)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")
}
