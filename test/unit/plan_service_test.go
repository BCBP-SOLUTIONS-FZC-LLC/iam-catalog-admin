// Unit tests for internal/core/service/plan_service.go.
// Black-box, package unit_test — see department_service_test.go's header
// for the shared convention (and fakeCache, defined there).
package unit_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/core/port"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/core/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakePlanRepo is an in-memory port.PlanRepository.
type fakePlanRepo struct {
	rows map[domain.TenantPlan]domain.Plan
}

func newFakePlanRepo() *fakePlanRepo {
	limit := func(n int) *int { return &n }
	return &fakePlanRepo{rows: map[domain.TenantPlan]domain.Plan{
		domain.PlanStarter: {
			Code: domain.PlanStarter, DisplayName: "Starter",
			WorkflowTemplateLimit: limit(5), TenderLimit: limit(10),
			TrialDurationDays: 30, CustomBranding: domain.BrandingNone,
			FeatureSet: map[string]any{}, RecordVersion: 1,
		},
		domain.PlanEnterprise: {
			Code: domain.PlanEnterprise, DisplayName: "Enterprise",
			WorkflowTemplateLimit: nil, TenderLimit: nil,
			TrialDurationDays: 30, SSOEnabled: true, CustomBranding: domain.BrandingLogo,
			FeatureSet: map[string]any{}, RecordVersion: 1,
		},
	}}
}

func (f *fakePlanRepo) List(_ context.Context) ([]domain.Plan, error) {
	var out []domain.Plan
	for _, p := range f.rows {
		out = append(out, p)
	}
	return out, nil
}

func (f *fakePlanRepo) FindByCode(_ context.Context, code domain.TenantPlan) (*domain.Plan, error) {
	p, ok := f.rows[code]
	if !ok {
		return nil, domain.NewError(domain.ErrPlanNotFound, "plan not found")
	}
	return &p, nil
}

func (f *fakePlanRepo) Update(_ context.Context, code domain.TenantPlan, patch *domain.PlanPatch) (*domain.Plan, error) {
	p, ok := f.rows[code]
	if !ok {
		return nil, domain.NewError(domain.ErrPlanNotFound, "plan not found")
	}
	if p.RecordVersion != patch.RecordVersion {
		return nil, domain.NewError(domain.ErrOptimisticLockConflict, "record version conflict").
			WithDetails(map[string]any{"record_version": p.RecordVersion})
	}
	if patch.DisplayName != nil {
		p.DisplayName = *patch.DisplayName
	}
	if patch.WorkflowTemplateLimit != nil {
		p.WorkflowTemplateLimit = *patch.WorkflowTemplateLimit
	}
	if patch.TenderLimit != nil {
		p.TenderLimit = *patch.TenderLimit
	}
	if patch.TrialDurationDays != nil {
		p.TrialDurationDays = *patch.TrialDurationDays
	}
	if patch.SSOEnabled != nil {
		p.SSOEnabled = *patch.SSOEnabled
	}
	if patch.CustomBranding != nil {
		p.CustomBranding = *patch.CustomBranding
	}
	if patch.FeatureSet != nil {
		p.FeatureSet = patch.FeatureSet
	}
	p.RecordVersion++
	f.rows[code] = p
	out := p
	return &out, nil
}

var _ port.PlanRepository = (*fakePlanRepo)(nil)

func TestPlanService_GetByCode(t *testing.T) {
	svc := service.NewPlanService(newFakePlanRepo(), newFakeCache())
	p, err := svc.GetByCode(context.Background(), domain.PlanStarter)
	require.NoError(t, err)
	assert.Equal(t, "Starter", p.DisplayName)
}

func TestPlanService_GetByCode_UnknownCode(t *testing.T) {
	svc := service.NewPlanService(newFakePlanRepo(), newFakeCache())
	_, err := svc.GetByCode(context.Background(), domain.TenantPlan("bogus"))
	require.Error(t, err)
	var de *domain.DomainError
	require.ErrorAs(t, err, &de)
	assert.Equal(t, domain.ErrPlanNotFound.Error(), de.Code)
}

func TestPlanService_Patch_NoMutableField(t *testing.T) {
	svc := service.NewPlanService(newFakePlanRepo(), newFakeCache())
	_, err := svc.Patch(context.Background(), domain.PlanStarter, &domain.PlanPatch{RecordVersion: 1})
	require.Error(t, err)
	var de *domain.DomainError
	require.ErrorAs(t, err, &de)
	assert.Equal(t, domain.ErrNoMutableField.Error(), de.Code)
}

func TestPlanService_Patch_NegativeLimitRejected(t *testing.T) {
	svc := service.NewPlanService(newFakePlanRepo(), newFakeCache())
	bad := -1
	badp := &bad
	_, err := svc.Patch(context.Background(), domain.PlanStarter, &domain.PlanPatch{
		WorkflowTemplateLimit: &badp, RecordVersion: 1,
	})
	require.Error(t, err)
	var de *domain.DomainError
	require.ErrorAs(t, err, &de)
	assert.Equal(t, domain.ErrValidation.Error(), de.Code)
}

func TestPlanService_Patch_NullLimitMeansUnlimited(t *testing.T) {
	repo := newFakePlanRepo()
	svc := service.NewPlanService(repo, newFakeCache())
	var nilLimit *int
	p, err := svc.Patch(context.Background(), domain.PlanStarter, &domain.PlanPatch{
		WorkflowTemplateLimit: &nilLimit, RecordVersion: 1,
	})
	require.NoError(t, err)
	assert.Nil(t, p.WorkflowTemplateLimit)
}

func TestPlanService_Patch_InvalidBrandingRejected(t *testing.T) {
	svc := service.NewPlanService(newFakePlanRepo(), newFakeCache())
	bogus := domain.BrandingLevel("full")
	_, err := svc.Patch(context.Background(), domain.PlanStarter, &domain.PlanPatch{
		CustomBranding: &bogus, RecordVersion: 1,
	})
	require.Error(t, err)
	var de *domain.DomainError
	require.ErrorAs(t, err, &de)
	assert.Equal(t, domain.ErrValidation.Error(), de.Code)
}

func TestPlanService_Patch_NonScalarFeatureSetRejected(t *testing.T) {
	svc := service.NewPlanService(newFakePlanRepo(), newFakeCache())
	_, err := svc.Patch(context.Background(), domain.PlanStarter, &domain.PlanPatch{
		FeatureSet:    map[string]any{"nested": map[string]any{"a": 1}},
		RecordVersion: 1,
	})
	require.Error(t, err)
	var de *domain.DomainError
	require.ErrorAs(t, err, &de)
	assert.Equal(t, domain.ErrValidation.Error(), de.Code)
	assert.Equal(t, "invalid_feature_value", de.Details["code"])
}

func TestPlanService_Patch_OptimisticLockConflict(t *testing.T) {
	svc := service.NewPlanService(newFakePlanRepo(), newFakeCache())
	name := "Starter Plus"
	_, err := svc.Patch(context.Background(), domain.PlanStarter, &domain.PlanPatch{
		DisplayName: &name, RecordVersion: 99,
	})
	require.Error(t, err)
	var de *domain.DomainError
	require.ErrorAs(t, err, &de)
	assert.Equal(t, domain.ErrOptimisticLockConflict.Error(), de.Code)
}

func TestPlanService_Patch_UnknownPlanCode(t *testing.T) {
	svc := service.NewPlanService(newFakePlanRepo(), newFakeCache())
	name := "x"
	_, err := svc.Patch(context.Background(), domain.TenantPlan("bogus"), &domain.PlanPatch{
		DisplayName: &name, RecordVersion: 1,
	})
	require.Error(t, err)
	var de *domain.DomainError
	require.ErrorAs(t, err, &de)
	assert.Equal(t, domain.ErrPlanNotFound.Error(), de.Code)
}

func TestPlanService_Patch_NegativeTenderLimitRejected(t *testing.T) {
	svc := service.NewPlanService(newFakePlanRepo(), newFakeCache())
	bad := -1
	badp := &bad
	_, err := svc.Patch(context.Background(), domain.PlanStarter, &domain.PlanPatch{
		TenderLimit: &badp, RecordVersion: 1,
	})
	require.Error(t, err)
	var de *domain.DomainError
	require.ErrorAs(t, err, &de)
	assert.Equal(t, domain.ErrValidation.Error(), de.Code)
}

func TestPlanService_Patch_NegativeTrialDurationRejected(t *testing.T) {
	svc := service.NewPlanService(newFakePlanRepo(), newFakeCache())
	bad := -1
	_, err := svc.Patch(context.Background(), domain.PlanStarter, &domain.PlanPatch{
		TrialDurationDays: &bad, RecordVersion: 1,
	})
	require.Error(t, err)
	var de *domain.DomainError
	require.ErrorAs(t, err, &de)
	assert.Equal(t, domain.ErrValidation.Error(), de.Code)
}

func TestPlanService_List_CacheHitSkipsRepo(t *testing.T) {
	repo := newFakePlanRepo()
	cache := newFakeCache()
	svc := service.NewPlanService(repo, cache)

	cached := []domain.Plan{{Code: domain.PlanStarter, DisplayName: "Cached Starter"}}
	raw, err := json.Marshal(cached)
	require.NoError(t, err)
	require.NoError(t, cache.Set(context.Background(), "cat:plans", raw, time.Minute))

	all, err := svc.List(context.Background())
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, "Cached Starter", all[0].DisplayName, "List must return the cached snapshot, not the repo's")
}

func TestPlanService_List_CachesFullCatalogAndInvalidatesOnPatch(t *testing.T) {
	repo := newFakePlanRepo()
	cache := newFakeCache()
	svc := service.NewPlanService(repo, cache)

	all, err := svc.List(context.Background())
	require.NoError(t, err)
	assert.Len(t, all, 2)
	assert.NotEmpty(t, cache.values["cat:plans"])

	name := "Starter Plus"
	_, err = svc.Patch(context.Background(), domain.PlanStarter, &domain.PlanPatch{
		DisplayName: &name, RecordVersion: 1,
	})
	require.NoError(t, err)
	assert.Empty(t, cache.values["cat:plans"], "Patch must invalidate the cache")
}
