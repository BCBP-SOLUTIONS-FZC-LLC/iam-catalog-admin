package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/core/port"
)

// plansCacheTTL is LLD §8's cat:plans TTL.
const plansCacheTTL = 60 * time.Second

// PlanService implements CAT-4, CAT-5, and the listing half of CAT-I2.
// Every write method assumes the handler-layer platform_operator gate
// (LLD §9) has already run. This service never reads or writes
// tenants.feature_flags (Core's override delta) and computes no
// "effective" value — that merge happens exclusively in Core at I-8 read
// time (LLD §5.3, PLAN-6). This service owns the baseline only.
type PlanService struct {
	repo  port.PlanRepository
	cache port.Cache
}

func NewPlanService(repo port.PlanRepository, cache port.Cache) *PlanService {
	return &PlanService{repo: repo, cache: cache}
}

// List serves CAT-4 (operator list) and CAT-I2 (internal bulk). The full
// three-tier catalog is read-through cached at cat:plans (LLD §8).
func (s *PlanService) List(ctx context.Context) ([]domain.Plan, error) {
	if s.cache != nil {
		if raw, err := s.cache.Get(ctx, "cat:plans"); err == nil && raw != nil {
			var cached []domain.Plan
			if jerr := json.Unmarshal(raw, &cached); jerr == nil {
				return cached, nil
			}
		}
	}
	all, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	if s.cache != nil {
		if raw, jerr := json.Marshal(all); jerr == nil {
			_ = s.cache.Set(ctx, "cat:plans", raw, plansCacheTTL)
		}
	}
	return all, nil
}

// GetByCode serves CAT-4's single-plan read. Not cache-fronted (LLD §8
// only names the whole-catalog key); falls straight through to Postgres.
func (s *PlanService) GetByCode(ctx context.Context, code domain.TenantPlan) (*domain.Plan, error) {
	switch code {
	case domain.PlanStarter, domain.PlanPro, domain.PlanEnterprise:
	default:
		return nil, domain.NewError(domain.ErrPlanNotFound, "plan not found")
	}
	return s.repo.FindByCode(ctx, code)
}

// Patch is CAT-5 — edit plan entitlements (PATCH-only, PLAN-4). Mirrors
// the DB CHECK constraints at the service layer so invalid values surface
// as 400 validation_error rather than a 500 from a raw CHECK violation.
func (s *PlanService) Patch(ctx context.Context, code domain.TenantPlan, patch *domain.PlanPatch) (*domain.Plan, error) {
	switch code {
	case domain.PlanStarter, domain.PlanPro, domain.PlanEnterprise:
	default:
		return nil, domain.NewError(domain.ErrPlanNotFound, "plan not found")
	}
	if patch.DisplayName == nil &&
		patch.WorkflowTemplateLimit == nil &&
		patch.TenderLimit == nil &&
		patch.TrialDurationDays == nil &&
		patch.SSOEnabled == nil &&
		patch.CustomBranding == nil &&
		patch.FeatureSet == nil {
		return nil, domain.NewError(domain.ErrNoMutableField, "at least one field must be provided").
			WithDetails(map[string]any{"code": "no_mutable_field"})
	}
	if patch.WorkflowTemplateLimit != nil && *patch.WorkflowTemplateLimit != nil && **patch.WorkflowTemplateLimit < 0 {
		return nil, domain.NewError(domain.ErrValidation, "workflow_template_limit must be >= 0")
	}
	if patch.TenderLimit != nil && *patch.TenderLimit != nil && **patch.TenderLimit < 0 {
		return nil, domain.NewError(domain.ErrValidation, "tender_limit must be >= 0")
	}
	if patch.TrialDurationDays != nil && *patch.TrialDurationDays < 0 {
		return nil, domain.NewError(domain.ErrValidation, "trial_duration_days must be >= 0")
	}
	if patch.CustomBranding != nil {
		switch *patch.CustomBranding {
		case domain.BrandingNone, domain.BrandingLogo:
		default:
			return nil, domain.NewError(domain.ErrValidation, "custom_branding must be one of: none, logo")
		}
	}
	// PLAN-6(d): feature_set values must be scalars — no nested
	// objects/arrays, the same defense O-4's allow-list check applies to
	// Core's tenants.feature_flags override delta (LLD §5.3).
	for k, v := range patch.FeatureSet {
		switch v.(type) {
		case string, bool, float64, int, int64, nil:
		default:
			return nil, domain.NewError(domain.ErrValidation, "feature_set values must be scalars").
				WithDetails(map[string]any{"code": "invalid_feature_value", "key": k})
		}
	}
	p, err := s.repo.Update(ctx, code, patch)
	if err != nil {
		return nil, err
	}
	if s.cache != nil {
		_ = s.cache.Delete(ctx, "cat:plans")
	}
	return p, nil
}
