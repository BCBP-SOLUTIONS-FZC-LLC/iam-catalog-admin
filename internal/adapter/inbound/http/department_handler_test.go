package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/core/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newDepartmentHandlerForTest() (*DepartmentHandler, *service.DepartmentService) {
	svc := newTestDepartmentService()
	return NewDepartmentHandler(svc), svc
}

func TestDepartmentHandler_Create_RequiresOperatorRole(t *testing.T) {
	h, _ := newDepartmentHandlerForTest()
	body, _ := json.Marshal(DepartmentCreateRequest{Code: "LEGAL", Name: "Legal"})
	c, w := newTestContext(t, http.MethodPost, "/api/v1/operator/departments", body, []string{"tenant_admin"})

	h.Create(c)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestDepartmentHandler_CreateThenGet(t *testing.T) {
	h, _ := newDepartmentHandlerForTest()

	body, _ := json.Marshal(DepartmentCreateRequest{Code: "LEGAL", Name: "Legal", IsSystem: false})
	c, w := newTestContext(t, http.MethodPost, "/api/v1/operator/departments", body, []string{"platform_operator"})
	h.Create(c)
	require.Equal(t, http.StatusCreated, w.Code)

	var created DepartmentResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	assert.Equal(t, "LEGAL", created.Code)

	// CAT-7 — public read, any authenticated caller.
	getC, getW := newTestContext(t, http.MethodGet, "/api/v1/departments/"+created.ID, nil, nil)
	setParams(getC, gin.Params{{Key: "id", Value: created.ID}})
	h.Get(getC)
	require.Equal(t, http.StatusOK, getW.Code)
	var got DepartmentResponse
	require.NoError(t, json.Unmarshal(getW.Body.Bytes(), &got))
	assert.Equal(t, created.ID, got.ID)
}

func TestDepartmentHandler_Patch_RejectsImmutableCodeField(t *testing.T) {
	h, _ := newDepartmentHandlerForTest()

	createBody, _ := json.Marshal(DepartmentCreateRequest{Code: "FINANCE", Name: "Finance"})
	cCreate, wCreate := newTestContext(t, http.MethodPost, "/api/v1/operator/departments", createBody, []string{"platform_operator"})
	h.Create(cCreate)
	require.Equal(t, http.StatusCreated, wCreate.Code)
	var created DepartmentResponse
	require.NoError(t, json.Unmarshal(wCreate.Body.Bytes(), &created))

	patchBody := []byte(`{"code":"NEWCODE","record_version":1}`)
	c, w := newTestContext(t, http.MethodPatch, "/api/v1/operator/departments/"+created.ID, patchBody, []string{"platform_operator"})
	setParams(c, gin.Params{{Key: "id", Value: created.ID}})
	h.Patch(c)

	require.Equal(t, http.StatusUnprocessableEntity, w.Code)
	var er ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &er))
	assert.Equal(t, "field_immutable", er.Code)
}

func TestDepartmentHandler_DeleteBlocked_Always405(t *testing.T) {
	h, _ := newDepartmentHandlerForTest()
	c, w := newTestContext(t, http.MethodDelete, "/api/v1/operator/departments/"+"11111111-1111-1111-1111-111111111111", nil, []string{"platform_operator"})
	setParams(c, gin.Params{{Key: "id", Value: "11111111-1111-1111-1111-111111111111"}})
	h.DeleteBlocked(c)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	// Must go through the standard ErrorResponse envelope like every other
	// error path — not a hand-rolled gin.H missing error/status/trace_id/
	// request_id.
	var er ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &er))
	assert.Equal(t, "method_not_allowed", er.Code)
	assert.Equal(t, "method_not_allowed", er.Error)
	assert.Equal(t, http.StatusMethodNotAllowed, er.Status)
}

func TestDepartmentHandler_Get_InvalidUUID(t *testing.T) {
	h, _ := newDepartmentHandlerForTest()
	c, w := newTestContext(t, http.MethodGet, "/api/v1/departments/not-a-uuid", nil, nil)
	setParams(c, gin.Params{{Key: "id", Value: "not-a-uuid"}})
	h.Get(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDepartmentHandler_Get_NotFound(t *testing.T) {
	h, _ := newDepartmentHandlerForTest()
	id := "11111111-1111-1111-1111-111111111111"
	c, w := newTestContext(t, http.MethodGet, "/api/v1/departments/"+id, nil, nil)
	setParams(c, gin.Params{{Key: "id", Value: id}})
	h.Get(c)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestDepartmentHandler_Create_InvalidBody(t *testing.T) {
	h, _ := newDepartmentHandlerForTest()
	c, w := newTestContext(t, http.MethodPost, "/api/v1/operator/departments", []byte(`{"code":`), []string{"platform_operator"})
	h.Create(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDepartmentHandler_Create_DuplicateCodeConflict(t *testing.T) {
	h, _ := newDepartmentHandlerForTest()
	body, _ := json.Marshal(DepartmentCreateRequest{Code: "LEGAL", Name: "Legal"})
	c1, w1 := newTestContext(t, http.MethodPost, "/api/v1/operator/departments", body, []string{"platform_operator"})
	h.Create(c1)
	require.Equal(t, http.StatusCreated, w1.Code)

	c2, w2 := newTestContext(t, http.MethodPost, "/api/v1/operator/departments", body, []string{"platform_operator"})
	h.Create(c2)
	assert.Equal(t, http.StatusConflict, w2.Code)
}

func TestDepartmentHandler_Patch_RequiresOperatorRole(t *testing.T) {
	h, _ := newDepartmentHandlerForTest()
	id := "11111111-1111-1111-1111-111111111111"
	c, w := newTestContext(t, http.MethodPatch, "/api/v1/operator/departments/"+id, []byte(`{"record_version":1}`), []string{"tenant_admin"})
	setParams(c, gin.Params{{Key: "id", Value: id}})
	h.Patch(c)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestDepartmentHandler_Patch_InvalidUUID(t *testing.T) {
	h, _ := newDepartmentHandlerForTest()
	c, w := newTestContext(t, http.MethodPatch, "/api/v1/operator/departments/not-a-uuid", []byte(`{"record_version":1}`), []string{"platform_operator"})
	setParams(c, gin.Params{{Key: "id", Value: "not-a-uuid"}})
	h.Patch(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDepartmentHandler_Patch_RejectsImmutableIsSystemField(t *testing.T) {
	h, _ := newDepartmentHandlerForTest()
	id := "11111111-1111-1111-1111-111111111111"
	patchBody := []byte(`{"is_system":true,"record_version":1}`)
	c, w := newTestContext(t, http.MethodPatch, "/api/v1/operator/departments/"+id, patchBody, []string{"platform_operator"})
	setParams(c, gin.Params{{Key: "id", Value: id}})
	h.Patch(c)
	require.Equal(t, http.StatusUnprocessableEntity, w.Code)
	var er ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &er))
	assert.Equal(t, "field_immutable", er.Code)
}

func TestDepartmentHandler_Patch_InvalidBody(t *testing.T) {
	h, _ := newDepartmentHandlerForTest()
	id := "11111111-1111-1111-1111-111111111111"
	c, w := newTestContext(t, http.MethodPatch, "/api/v1/operator/departments/"+id, []byte(`{"record_version": "not-a-number"}`), []string{"platform_operator"})
	setParams(c, gin.Params{{Key: "id", Value: id}})
	h.Patch(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDepartmentHandler_Patch_NotFound(t *testing.T) {
	h, _ := newDepartmentHandlerForTest()
	id := "11111111-1111-1111-1111-111111111111"
	c, w := newTestContext(t, http.MethodPatch, "/api/v1/operator/departments/"+id, []byte(`{"is_active":false,"record_version":1}`), []string{"platform_operator"})
	setParams(c, gin.Params{{Key: "id", Value: id}})
	h.Patch(c)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestDepartmentHandler_DeleteBlocked_RequiresOperatorRole(t *testing.T) {
	h, _ := newDepartmentHandlerForTest()
	id := "11111111-1111-1111-1111-111111111111"
	c, w := newTestContext(t, http.MethodDelete, "/api/v1/operator/departments/"+id, nil, []string{"tenant_admin"})
	setParams(c, gin.Params{{Key: "id", Value: id}})
	h.DeleteBlocked(c)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestDepartmentHandler_List_RequiresNoRole(t *testing.T) {
	h, _ := newDepartmentHandlerForTest()
	c, w := newTestContext(t, http.MethodGet, "/api/v1/departments", nil, nil)
	h.List(c)
	require.Equal(t, http.StatusOK, w.Code)
	var resp DepartmentListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Empty(t, resp.Items)
}

func TestDepartmentHandler_List_RepoError(t *testing.T) {
	repo := newFakeDepartmentRepo()
	repo.listErr = errors.New("boom")
	h := NewDepartmentHandler(newTestDepartmentServiceWithRepo(repo))
	c, w := newTestContext(t, http.MethodGet, "/api/v1/departments", nil, nil)
	h.List(c)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDepartmentHandler_Patch_MalformedJSONBody(t *testing.T) {
	h, _ := newDepartmentHandlerForTest()
	id := "11111111-1111-1111-1111-111111111111"
	c, w := newTestContext(t, http.MethodPatch, "/api/v1/operator/departments/"+id, []byte(`{not-json`), []string{"platform_operator"})
	setParams(c, gin.Params{{Key: "id", Value: id}})
	h.Patch(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDepartmentHandler_List_ActiveOnlyQueryParam(t *testing.T) {
	h, svc := newDepartmentHandlerForTest()

	body, _ := json.Marshal(DepartmentCreateRequest{Code: "LEGAL", Name: "Legal"})
	c, _ := newTestContext(t, http.MethodPost, "/api/v1/operator/departments", body, []string{"platform_operator"})
	h.Create(c)

	// Retire it via the service directly to avoid another round trip.
	all, err := svc.List(c.Request.Context(), false)
	require.NoError(t, err)
	require.Len(t, all, 1)
	inactive := false
	_, err = svc.Patch(c.Request.Context(), all[0].ID, nil, &inactive, all[0].RecordVersion)
	require.NoError(t, err)

	listC, listW := newTestContext(t, http.MethodGet, "/api/v1/departments?active_only=true", nil, nil)
	listC.Request.URL.RawQuery = "active_only=true"
	h.List(listC)
	require.Equal(t, http.StatusOK, listW.Code)
	var resp DepartmentListResponse
	require.NoError(t, json.Unmarshal(listW.Body.Bytes(), &resp))
	assert.Empty(t, resp.Items)
}
