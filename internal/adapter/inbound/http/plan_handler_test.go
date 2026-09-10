package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlanHandler_List_RepoError(t *testing.T) {
	repo := newFakePlanRepo()
	repo.listErr = errors.New("boom")
	h := NewPlanHandler(newTestPlanServiceWithRepo(repo))
	c, w := newTestContext(t, http.MethodGet, "/api/v1/operator/plans", nil, []string{"platform_operator"})
	h.List(c)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestPlanHandler_List_RequiresOperatorRole(t *testing.T) {
	h := NewPlanHandler(newTestPlanService())
	c, w := newTestContext(t, http.MethodGet, "/api/v1/operator/plans", nil, nil)
	h.List(c)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestPlanHandler_List(t *testing.T) {
	h := NewPlanHandler(newTestPlanService())
	c, w := newTestContext(t, http.MethodGet, "/api/v1/operator/plans", nil, []string{"platform_operator"})
	h.List(c)
	require.Equal(t, http.StatusOK, w.Code)
	var resp PlansListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Len(t, resp.Items, 1)
}

func TestPlanHandler_Get_UnknownCode404(t *testing.T) {
	h := NewPlanHandler(newTestPlanService())
	c, w := newTestContext(t, http.MethodGet, "/api/v1/operator/plans/bogus", nil, []string{"platform_operator"})
	setParams(c, gin.Params{{Key: "code", Value: "bogus"}})
	h.Get(c)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestPlanHandler_Patch_NullLimitMeansUnlimited(t *testing.T) {
	h := NewPlanHandler(newTestPlanService())
	body := []byte(`{"workflow_template_limit":null,"record_version":1}`)
	c, w := newTestContext(t, http.MethodPatch, "/api/v1/operator/plans/starter", body, []string{"platform_operator"})
	setParams(c, gin.Params{{Key: "code", Value: "starter"}})
	h.Patch(c)
	require.Equal(t, http.StatusOK, w.Code)
	var resp PlanResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Nil(t, resp.WorkflowTemplateLimit)
}

func TestPlanHandler_Get_RequiresOperatorRole(t *testing.T) {
	h := NewPlanHandler(newTestPlanService())
	c, w := newTestContext(t, http.MethodGet, "/api/v1/operator/plans/starter", nil, nil)
	setParams(c, gin.Params{{Key: "code", Value: "starter"}})
	h.Get(c)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestPlanHandler_Get(t *testing.T) {
	h := NewPlanHandler(newTestPlanService())
	c, w := newTestContext(t, http.MethodGet, "/api/v1/operator/plans/starter", nil, []string{"platform_operator"})
	setParams(c, gin.Params{{Key: "code", Value: "starter"}})
	h.Get(c)
	require.Equal(t, http.StatusOK, w.Code)
	var resp PlanResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "starter", resp.Code)
}

func TestPlanHandler_Patch_RequiresOperatorRole(t *testing.T) {
	h := NewPlanHandler(newTestPlanService())
	c, w := newTestContext(t, http.MethodPatch, "/api/v1/operator/plans/starter", []byte(`{"record_version":1}`), nil)
	setParams(c, gin.Params{{Key: "code", Value: "starter"}})
	h.Patch(c)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestPlanHandler_Patch_InvalidBody(t *testing.T) {
	h := NewPlanHandler(newTestPlanService())
	c, w := newTestContext(t, http.MethodPatch, "/api/v1/operator/plans/starter", []byte(`{"record_version":`), []string{"platform_operator"})
	setParams(c, gin.Params{{Key: "code", Value: "starter"}})
	h.Patch(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestPlanHandler_Patch_InvalidLimitType(t *testing.T) {
	h := NewPlanHandler(newTestPlanService())
	body := []byte(`{"workflow_template_limit":"not-a-number","record_version":1}`)
	c, w := newTestContext(t, http.MethodPatch, "/api/v1/operator/plans/starter", body, []string{"platform_operator"})
	setParams(c, gin.Params{{Key: "code", Value: "starter"}})
	h.Patch(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestPlanHandler_Patch_InvalidTenderLimitType(t *testing.T) {
	h := NewPlanHandler(newTestPlanService())
	body := []byte(`{"tender_limit":"not-a-number","record_version":1}`)
	c, w := newTestContext(t, http.MethodPatch, "/api/v1/operator/plans/starter", body, []string{"platform_operator"})
	setParams(c, gin.Params{{Key: "code", Value: "starter"}})
	h.Patch(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestPlanHandler_Patch_ExplicitLimitValue(t *testing.T) {
	h := NewPlanHandler(newTestPlanService())
	body := []byte(`{"workflow_template_limit":7,"custom_branding":"logo","record_version":1}`)
	c, w := newTestContext(t, http.MethodPatch, "/api/v1/operator/plans/starter", body, []string{"platform_operator"})
	setParams(c, gin.Params{{Key: "code", Value: "starter"}})
	h.Patch(c)
	require.Equal(t, http.StatusOK, w.Code)
	var resp PlanResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotNil(t, resp.WorkflowTemplateLimit)
	assert.Equal(t, 7, *resp.WorkflowTemplateLimit)
	assert.Equal(t, "logo", resp.CustomBranding)
}

func TestPlanHandler_Patch_OptimisticLockConflict(t *testing.T) {
	h := NewPlanHandler(newTestPlanService())
	body := []byte(`{"display_name":"Starter v2","record_version":99}`)
	c, w := newTestContext(t, http.MethodPatch, "/api/v1/operator/plans/starter", body, []string{"platform_operator"})
	setParams(c, gin.Params{{Key: "code", Value: "starter"}})
	h.Patch(c)
	require.Equal(t, http.StatusConflict, w.Code)
	var er ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &er))
	assert.Equal(t, "optimistic_lock_conflict", er.Code)
}

func TestPlanHandler_Patch_NonScalarFeatureSetValue_Returns400InvalidFeatureValue(t *testing.T) {
	h := NewPlanHandler(newTestPlanService())
	body := []byte(`{"feature_set":{"nested":{"a":1}},"record_version":1}`)
	c, w := newTestContext(t, http.MethodPatch, "/api/v1/operator/plans/starter", body, []string{"platform_operator"})
	setParams(c, gin.Params{{Key: "code", Value: "starter"}})
	h.Patch(c)
	require.Equal(t, http.StatusBadRequest, w.Code)
	var er ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &er))
	// LLD §17: a distinct invalid_feature_value code, not the
	// generic validation_error — error and code must agree, like every
	// other error this service returns.
	assert.Equal(t, "invalid_feature_value", er.Code)
	assert.Equal(t, "invalid_feature_value", er.Error)
}
