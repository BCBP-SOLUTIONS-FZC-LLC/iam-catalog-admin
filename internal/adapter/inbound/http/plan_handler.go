package http

import (
	"encoding/json"
	"net/http"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/core/service"
	"github.com/gin-gonic/gin"
)

type PlanHandler struct {
	svc *service.PlanService
}

func NewPlanHandler(svc *service.PlanService) *PlanHandler {
	return &PlanHandler{svc: svc}
}

func planToResponse(p *domain.Plan) PlanResponse {
	return PlanResponse{
		Code: string(p.Code), DisplayName: p.DisplayName,
		WorkflowTemplateLimit: p.WorkflowTemplateLimit, TenderLimit: p.TenderLimit,
		TrialDurationDays: p.TrialDurationDays, SSOEnabled: p.SSOEnabled,
		CustomBranding: string(p.CustomBranding), FeatureSet: p.FeatureSet,
		RecordVersion: p.RecordVersion,
	}
}

// ── CAT-4 GET /api/v1/operator/plans ───────────────────────────────────

// List is CAT-4 — list plan entitlement catalog.
//
// @Summary      CAT-4 — List plan entitlement catalog
// @Tags         operator
// @Produce      json
// @Success      200  {object}  PlansListResponse
// @Failure      403  {object}  ErrorResponse
// @Router       /operator/plans [get]
func (h *PlanHandler) List(c *gin.Context) {
	if err := requireOperator(c); err != nil {
		HandleError(c, err)
		return
	}
	plans, err := h.svc.List(c.Request.Context())
	if err != nil {
		HandleError(c, err)
		return
	}
	items := make([]PlanResponse, len(plans))
	for i := range plans {
		items[i] = planToResponse(&plans[i])
	}
	c.JSON(http.StatusOK, PlansListResponse{Items: items})
}

// ── CAT-4 GET /api/v1/operator/plans/:code ─────────────────────────────

// Get is CAT-4 — single plan read by code.
//
// @Summary      CAT-4 — Single plan read by code
// @Tags         operator
// @Produce      json
// @Param        code  path      string  true  "Plan code"  Enums(starter, pro, enterprise)
// @Success      200   {object}  PlanResponse
// @Failure      403   {object}  ErrorResponse
// @Failure      404   {object}  ErrorResponse
// @Router       /operator/plans/{code} [get]
func (h *PlanHandler) Get(c *gin.Context) {
	if err := requireOperator(c); err != nil {
		HandleError(c, err)
		return
	}
	code := domain.TenantPlan(c.Param("code"))
	p, err := h.svc.GetByCode(c.Request.Context(), code)
	if err != nil {
		HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, planToResponse(p))
}

// ── CAT-5 PATCH /api/v1/operator/plans/:code ───────────────────────────

// Patch is CAT-5 — edit plan entitlements (PATCH-only, PLAN-4).
//
// @Summary      CAT-5 — Edit plan entitlements (PATCH-only)
// @Description  No create/delete API — the tier set is fixed to the tenant_plan ENUM. Optimistic-locked on record_version.
// @Tags         operator
// @Accept       json
// @Produce      json
// @Param        code     path      string             true  "Plan code"  Enums(starter, pro, enterprise)
// @Param        request  body      PlanPatchRequest   true  "Plan patch"
// @Success      200      {object}  PlanResponse
// @Failure      400      {object}  ErrorResponse  "no_mutable_field | invalid_feature_value | validation_error"
// @Failure      403      {object}  ErrorResponse
// @Failure      404      {object}  ErrorResponse
// @Failure      409      {object}  ErrorResponse  "optimistic_lock_conflict"
// @Router       /operator/plans/{code} [patch]
func (h *PlanHandler) Patch(c *gin.Context) {
	if err := requireOperator(c); err != nil {
		HandleError(c, err)
		return
	}
	code := domain.TenantPlan(c.Param("code"))
	var body struct {
		DisplayName           *string         `json:"display_name,omitempty"`
		WorkflowTemplateLimit json.RawMessage `json:"workflow_template_limit"`
		TenderLimit           json.RawMessage `json:"tender_limit"`
		TrialDurationDays     *int            `json:"trial_duration_days,omitempty"`
		SSOEnabled            *bool           `json:"sso_enabled,omitempty"`
		CustomBranding        *string         `json:"custom_branding,omitempty"`
		FeatureSet            map[string]any  `json:"feature_set,omitempty"`
		RecordVersion         int64           `json:"record_version"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		HandleError(c, domain.NewError(domain.ErrValidation, "invalid request body"))
		return
	}
	// parseLimit distinguishes absent (nil RawMessage) from explicit null
	// from a value — drives the **int double-pointer in PlanPatch (LLD
	// §5.2/CAT-D6):
	//   nil **int        = field absent → no SET clause in UPDATE
	//   **int → nil *int = explicit null → NULL in DB (unlimited)
	//   **int → *int → n = explicit value → cap n in DB
	parseLimit := func(raw json.RawMessage, field string) (**int, error) {
		if len(raw) == 0 {
			return nil, nil
		}
		if string(raw) == "null" {
			return new(*int), nil
		}
		var v int
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, domain.NewError(domain.ErrValidation, field+" must be an integer or null")
		}
		vp := &v
		return &vp, nil
	}
	patch := &domain.PlanPatch{
		DisplayName:       body.DisplayName,
		TrialDurationDays: body.TrialDurationDays,
		SSOEnabled:        body.SSOEnabled,
		FeatureSet:        body.FeatureSet,
		RecordVersion:     body.RecordVersion,
	}
	var parseErr error
	patch.WorkflowTemplateLimit, parseErr = parseLimit(body.WorkflowTemplateLimit, "workflow_template_limit")
	if parseErr != nil {
		HandleError(c, parseErr)
		return
	}
	patch.TenderLimit, parseErr = parseLimit(body.TenderLimit, "tender_limit")
	if parseErr != nil {
		HandleError(c, parseErr)
		return
	}
	if body.CustomBranding != nil {
		v := domain.BrandingLevel(*body.CustomBranding)
		patch.CustomBranding = &v
	}
	p, err := h.svc.Patch(c.Request.Context(), code, patch)
	if err != nil {
		HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, planToResponse(p))
}
