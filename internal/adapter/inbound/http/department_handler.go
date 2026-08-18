package http

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/adapter/outbound/metrics"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/core/service"
	"github.com/gin-gonic/gin"
)

type DepartmentHandler struct {
	svc *service.DepartmentService
}

func NewDepartmentHandler(svc *service.DepartmentService) *DepartmentHandler {
	return &DepartmentHandler{svc: svc}
}

func departmentToResponse(d *domain.Department) DepartmentResponse {
	return DepartmentResponse{
		ID: d.ID.String(), Code: d.Code, Name: d.Name,
		IsSystem: d.IsSystem, IsActive: d.IsActive, RecordVersion: d.RecordVersion,
	}
}

// ── CAT-6 GET /api/v1/departments ─────────────────────────────────────

// List is CAT-6 — the previously-undocumented global department catalog
// read, now catalogued (ADR-0007 Action Item 1). Any authenticated caller
// (no platform_operator requirement).
//
// @Summary      CAT-6 — Global department catalog listing
// @Tags         departments
// @Produce      json
// @Param        active_only  query     bool  false  "Filter to is_active=true only"
// @Success      200          {object}  DepartmentListResponse
// @Failure      401          {object}  ErrorResponse
// @Router       /departments [get]
func (h *DepartmentHandler) List(c *gin.Context) {
	activeOnly := c.Query("active_only") == "true"
	depts, err := h.svc.List(c.Request.Context(), activeOnly)
	if err != nil {
		HandleError(c, err)
		return
	}
	items := make([]DepartmentResponse, len(depts))
	for i := range depts {
		items[i] = departmentToResponse(&depts[i])
	}
	c.JSON(http.StatusOK, DepartmentListResponse{Items: items})
}

// ── CAT-7 GET /api/v1/departments/:id ─────────────────────────────────

// Get is CAT-7 — single department read.
//
// @Summary      CAT-7 — Single department read
// @Tags         departments
// @Produce      json
// @Param        id   path      string  true  "Department UUID"  format(uuid)
// @Success      200  {object}  DepartmentResponse
// @Failure      404  {object}  ErrorResponse
// @Router       /departments/{id} [get]
func (h *DepartmentHandler) Get(c *gin.Context) {
	id, err := parseUUIDParam(c, "id")
	if err != nil {
		HandleError(c, err)
		return
	}
	d, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, departmentToResponse(d))
}

// ── CAT-1 POST /api/v1/operator/departments ───────────────────────────

// Create is CAT-1 — add a global-catalog department.
//
// @Summary      CAT-1 — Add global-catalog department
// @Description  is_system and code are immutable after creation.
// @Tags         operator
// @Accept       json
// @Produce      json
// @Param        request  body      DepartmentCreateRequest  true  "Department payload"
// @Success      201      {object}  DepartmentResponse
// @Failure      400      {object}  ErrorResponse
// @Failure      403      {object}  ErrorResponse
// @Failure      409      {object}  ErrorResponse  "duplicate_code"
// @Router       /operator/departments [post]
func (h *DepartmentHandler) Create(c *gin.Context) {
	if err := requireOperator(c); err != nil {
		HandleError(c, err)
		return
	}
	var req DepartmentCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		HandleError(c, domain.NewError(domain.ErrValidation, "invalid request body"))
		return
	}
	d, err := h.svc.Create(c.Request.Context(), req.Code, req.Name, req.IsSystem)
	if err != nil {
		HandleError(c, err)
		return
	}
	metrics.WritesTotal.WithLabelValues("departments", "insert").Inc()
	c.JSON(http.StatusCreated, departmentToResponse(d))
}

// ── CAT-2 PATCH /api/v1/operator/departments/:id ──────────────────────

// Patch is CAT-2 — update name or is_active (code + is_system immutable).
//
// @Summary      CAT-2 — Update name or is_active
// @Description  System-department deactivation is blocked (422 system_department_cannot_be_retired). Optimistic-locked on record_version.
// @Tags         operator
// @Accept       json
// @Produce      json
// @Param        id       path      string                    true  "Department UUID"  format(uuid)
// @Param        request  body      DepartmentPatchRequest    true  "Patch payload"
// @Success      200      {object}  DepartmentResponse
// @Failure      400      {object}  ErrorResponse
// @Failure      403      {object}  ErrorResponse
// @Failure      404      {object}  ErrorResponse
// @Failure      409      {object}  ErrorResponse  "optimistic_lock_conflict"
// @Failure      422      {object}  ErrorResponse  "field_immutable | system_name_immutable | system_department_cannot_be_retired"
// @Router       /operator/departments/{id} [patch]
func (h *DepartmentHandler) Patch(c *gin.Context) {
	if err := requireOperator(c); err != nil {
		HandleError(c, err)
		return
	}
	id, err := parseUUIDParam(c, "id")
	if err != nil {
		HandleError(c, err)
		return
	}
	// CAT-2 (LLD §6): reject `code`/`is_system` in the body with
	// 422 field_immutable — the DTO struct would silently drop them.
	body, _ := io.ReadAll(c.Request.Body)
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	var raw map[string]json.RawMessage
	if len(body) > 0 {
		if jerr := json.Unmarshal(body, &raw); jerr != nil {
			HandleError(c, domain.NewError(domain.ErrValidation, "invalid request body"))
			return
		}
	}
	if _, present := raw["code"]; present {
		HandleError(c, domain.NewError(domain.ErrFieldImmutable, "code is immutable").
			WithDetails(map[string]any{"code": "field_immutable", "field": "code"}))
		return
	}
	if _, present := raw["is_system"]; present {
		HandleError(c, domain.NewError(domain.ErrFieldImmutable, "is_system is immutable").
			WithDetails(map[string]any{"code": "field_immutable", "field": "is_system"}))
		return
	}
	var req DepartmentPatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		HandleError(c, domain.NewError(domain.ErrValidation, "invalid request body"))
		return
	}
	d, err := h.svc.Patch(c.Request.Context(), id, req.Name, req.IsActive, req.RecordVersion)
	if err != nil {
		if errors.Is(err, domain.ErrOptimisticLockConflict) {
			metrics.OptimisticLockConflicts.WithLabelValues("departments").Inc()
		}
		HandleError(c, err)
		return
	}
	metrics.WritesTotal.WithLabelValues("departments", "update").Inc()
	c.JSON(http.StatusOK, departmentToResponse(d))
}

// ── CAT-3 DELETE /api/v1/operator/departments/:id → 405 ───────────────

// DeleteBlocked is CAT-3 — always returns 405 (retire via CAT-2 is_active=false).
//
// @Summary      CAT-3 — Blocked (405); retire via CAT-2 is_active=false
// @Tags         operator
// @Produce      json
// @Param        id   path  string  true  "Department UUID"  format(uuid)
// @Failure      403  {object}  ErrorResponse
// @Failure      405  {object}  ErrorResponse
// @Router       /operator/departments/{id} [delete]
func (h *DepartmentHandler) DeleteBlocked(c *gin.Context) {
	if err := requireOperator(c); err != nil {
		HandleError(c, err)
		return
	}
	HandleError(c, h.svc.DeleteBlocked())
}
