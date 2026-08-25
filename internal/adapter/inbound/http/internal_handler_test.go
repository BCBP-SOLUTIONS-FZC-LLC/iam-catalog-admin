package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/core/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInternalHandler_Departments_RequiresSystemRole(t *testing.T) {
	h := NewInternalHandler(newTestDepartmentService(), newTestPlanService())
	c, w := newTestContext(t, http.MethodGet, "/api/v1/internal/departments", nil, []string{"platform_operator"})
	h.Departments(c)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestInternalHandler_Departments_CAT_I1(t *testing.T) {
	h := NewInternalHandler(newTestDepartmentService(), newTestPlanService())
	c, w := newTestContext(t, http.MethodGet, "/api/v1/internal/departments", nil, []string{"iam-system"})
	h.Departments(c)
	require.Equal(t, http.StatusOK, w.Code)
	var resp InternalDepartmentsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp.AsOf)
}

func TestInternalHandler_Plans_CAT_I2(t *testing.T) {
	h := NewInternalHandler(newTestDepartmentService(), newTestPlanService())
	c, w := newTestContext(t, http.MethodGet, "/api/v1/internal/plans", nil, []string{"iam-system"})
	h.Plans(c)
	require.Equal(t, http.StatusOK, w.Code)
	var resp InternalPlansResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Plans, 1)
	assert.Equal(t, int64(1), resp.RecordVersions["starter"])
}

func TestInternalHandler_Plans_RequiresSystemRole(t *testing.T) {
	h := NewInternalHandler(newTestDepartmentService(), newTestPlanService())
	c, w := newTestContext(t, http.MethodGet, "/api/v1/internal/plans", nil, []string{"platform_operator"})
	h.Plans(c)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestInternalHandler_Departments_RepoError(t *testing.T) {
	repo := newFakeDepartmentRepo()
	repo.listErr = errors.New("boom")
	h := NewInternalHandler(newTestDepartmentServiceWithRepo(repo), newTestPlanService())
	c, w := newTestContext(t, http.MethodGet, "/api/v1/internal/departments", nil, []string{"iam-system"})
	h.Departments(c)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestInternalHandler_Plans_RepoError(t *testing.T) {
	repo := newFakePlanRepo()
	repo.listErr = errors.New("boom")
	h := NewInternalHandler(newTestDepartmentService(), newTestPlanServiceWithRepo(repo))
	c, w := newTestContext(t, http.MethodGet, "/api/v1/internal/plans", nil, []string{"iam-system"})
	h.Plans(c)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// TestInternalHandler_Departments_WithItems exercises the for-range loop body
// (lines 44-46) by pre-populating the repo with one department.
func TestInternalHandler_Departments_WithItems(t *testing.T) {
	repo := newFakeDepartmentRepo()
	id := uuid.New()
	repo.rows[id] = domain.Department{
		ID: id, Code: "GLOBAL", Name: "Global",
		IsSystem: true, IsActive: true, RecordVersion: 1,
	}
	h := NewInternalHandler(newTestDepartmentServiceWithRepo(repo), newTestPlanService())
	c, w := newTestContext(t, http.MethodGet, "/api/v1/internal/departments", nil, []string{"iam-system"})
	h.Departments(c)
	require.Equal(t, http.StatusOK, w.Code)
	var resp InternalDepartmentsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Departments, 1)
	assert.Equal(t, "GLOBAL", resp.Departments[0].Code)
}
