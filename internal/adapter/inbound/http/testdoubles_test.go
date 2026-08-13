package http

import (
	"context"
	"time"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/core/service"
	"github.com/google/uuid"
)

// ── In-memory port fakes, scoped to this package's handler tests ────────

type fakeDepartmentRepo struct {
	rows    map[uuid.UUID]domain.Department
	listErr error
}

func newFakeDepartmentRepo() *fakeDepartmentRepo {
	return &fakeDepartmentRepo{rows: map[uuid.UUID]domain.Department{}}
}

func (f *fakeDepartmentRepo) List(_ context.Context, activeOnly bool) ([]domain.Department, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	var out []domain.Department
	for _, d := range f.rows {
		if activeOnly && !d.IsActive {
			continue
		}
		out = append(out, d)
	}
	return out, nil
}

func (f *fakeDepartmentRepo) FindByID(_ context.Context, id uuid.UUID) (*domain.Department, error) {
	d, ok := f.rows[id]
	if !ok {
		return nil, domain.NewError(domain.ErrDepartmentNotFound, "department not found")
	}
	return &d, nil
}

func (f *fakeDepartmentRepo) FindByCode(_ context.Context, code string) (*domain.Department, error) {
	for _, d := range f.rows {
		if d.Code == code {
			return &d, nil
		}
	}
	return nil, domain.NewError(domain.ErrDepartmentNotFound, "department not found")
}

func (f *fakeDepartmentRepo) Insert(_ context.Context, d *domain.Department) (*domain.Department, error) {
	for _, existing := range f.rows {
		if existing.Code == d.Code {
			return nil, domain.NewError(domain.ErrConflict, "department code already exists").
				WithDetails(map[string]any{"code": "duplicate_code"})
		}
	}
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	d.RecordVersion = 1
	f.rows[d.ID] = *d
	out := *d
	return &out, nil
}

func (f *fakeDepartmentRepo) Update(_ context.Context, id uuid.UUID, name *string, isActive *bool, expectedVersion int64) (*domain.Department, error) {
	d, ok := f.rows[id]
	if !ok {
		return nil, domain.NewError(domain.ErrDepartmentNotFound, "department not found")
	}
	if d.RecordVersion != expectedVersion {
		return nil, domain.NewError(domain.ErrOptimisticLockConflict, "record version conflict").
			WithDetails(map[string]any{"record_version": d.RecordVersion})
	}
	if d.IsSystem && isActive != nil && !*isActive {
		return nil, domain.NewError(domain.ErrSystemDepartmentCannotBeRetired, "chk_system_department_active")
	}
	if name != nil {
		d.Name = *name
	}
	if isActive != nil {
		d.IsActive = *isActive
	}
	d.RecordVersion++
	f.rows[id] = d
	out := d
	return &out, nil
}

type fakePlanRepo struct {
	rows    map[domain.TenantPlan]domain.Plan
	listErr error
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
	}}
}

func (f *fakePlanRepo) List(_ context.Context) ([]domain.Plan, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
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

type fakeCache struct {
	values map[string][]byte
}

func newFakeCache() *fakeCache { return &fakeCache{values: map[string][]byte{}} }

func (f *fakeCache) Get(_ context.Context, key string) ([]byte, error) { return f.values[key], nil }
func (f *fakeCache) Set(_ context.Context, key string, value []byte, _ time.Duration) error {
	f.values[key] = value
	return nil
}
func (f *fakeCache) Delete(_ context.Context, keys ...string) error {
	for _, k := range keys {
		delete(f.values, k)
	}
	return nil
}
func (f *fakeCache) Health(_ context.Context) error { return nil }
func (f *fakeCache) Close() error                   { return nil }

func newTestDepartmentService() *service.DepartmentService {
	return service.NewDepartmentService(newFakeDepartmentRepo(), newFakeCache())
}

func newTestPlanService() *service.PlanService {
	return service.NewPlanService(newFakePlanRepo(), newFakeCache())
}

// newTestDepartmentServiceWithRepo/newTestPlanServiceWithRepo let handler
// tests inject a repo double pre-configured with an error (e.g. listErr)
// to exercise the handlers' repo/service error-propagation branches.
func newTestDepartmentServiceWithRepo(repo *fakeDepartmentRepo) *service.DepartmentService {
	return service.NewDepartmentService(repo, newFakeCache())
}

func newTestPlanServiceWithRepo(repo *fakePlanRepo) *service.PlanService {
	return service.NewPlanService(repo, newFakeCache())
}
