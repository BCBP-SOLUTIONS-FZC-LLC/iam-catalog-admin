package http

import (
	"net/http"
	"time"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/core/service"
	"github.com/gin-gonic/gin"
)

// InternalHandler serves CAT-I1/CAT-I2 — mesh-only bulk reads consumed by
// Core Org & Membership (om:departments/om:plans) and the Group Mapping
// Service (gm:departments) to populate their own local caches, replacing
// the DB FKs each loses on the physical split (LLD §7).
type InternalHandler struct {
	depts *service.DepartmentService
	plans *service.PlanService
}

func NewInternalHandler(depts *service.DepartmentService, plans *service.PlanService) *InternalHandler {
	return &InternalHandler{depts: depts, plans: plans}
}

// Departments is CAT-I1 — GET /internal/departments (LLD §7.1).
//
// @Summary      CAT-I1 — Bulk department catalog read (mesh-only)
// @Tags         internal
// @Produce      json
// @Success      200  {object}  InternalDepartmentsResponse
// @Failure      403  {object}  ErrorResponse
// @Router       /internal/departments [get]
func (h *InternalHandler) Departments(c *gin.Context) {
	if err := requireSystem(c); err != nil {
		HandleError(c, err)
		return
	}
	depts, err := h.depts.List(c.Request.Context(), false)
	if err != nil {
		HandleError(c, err)
		return
	}
	items := make([]DepartmentResponse, len(depts))
	for i := range depts {
		items[i] = departmentToResponse(&depts[i])
	}
	c.JSON(http.StatusOK, InternalDepartmentsResponse{
		Departments: items,
		AsOf:        time.Now().UTC().Format(time.RFC3339),
	})
}

// Plans is CAT-I2 — GET /internal/plans (LLD §7.2).
//
// @Summary      CAT-I2 — Bulk plan catalog read (mesh-only)
// @Tags         internal
// @Produce      json
// @Success      200  {object}  InternalPlansResponse
// @Failure      403  {object}  ErrorResponse
// @Router       /internal/plans [get]
func (h *InternalHandler) Plans(c *gin.Context) {
	if err := requireSystem(c); err != nil {
		HandleError(c, err)
		return
	}
	plans, err := h.plans.List(c.Request.Context())
	if err != nil {
		HandleError(c, err)
		return
	}
	items := make([]PlanResponse, len(plans))
	versions := make(map[string]int64, len(plans))
	for i := range plans {
		items[i] = planToResponse(&plans[i])
		versions[string(plans[i].Code)] = plans[i].RecordVersion
	}
	c.JSON(http.StatusOK, InternalPlansResponse{Plans: items, RecordVersions: versions})
}
