// Package fixtures provides shared test-data builders for all test tiers.
// Fixtures create a known, deterministic DB state that tests can assert against.
// Works with a real Postgres connection (test/postgres/) or inside e2e harness.
//
// Usage:
//
//	f := fixtures.New(pool)
//	dept := f.SystemDept(ctx, "engineering")
//	plan := f.StarterPlan(ctx)
package fixtures

import (
	"context"
	"fmt"
	"testing"

	pgadapter "github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/adapter/outbound/postgres"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-pgcommon/pkg/pgcommon"
	"github.com/stretchr/testify/require"
)

// Fixtures is a stateful fixture builder bound to a Postgres pool.
// Each method inserts a row and returns the created entity.
type Fixtures struct {
	pool     *pgcommon.Pool
	deptRepo *pgadapter.DepartmentRepository
	planRepo *pgadapter.PlanRepository
	counter  int
}

// New creates a Fixtures bound to the given pool.
func New(pool *pgcommon.Pool) *Fixtures {
	return &Fixtures{
		pool:     pool,
		deptRepo: pgadapter.NewDepartmentRepository(pool),
		planRepo: pgadapter.NewPlanRepository(pool),
	}
}

// uniqueCode returns a unique code for each call within a test run.
func (f *Fixtures) uniqueCode(prefix string) string {
	f.counter++
	return fmt.Sprintf("%s_%04d", prefix, f.counter)
}

// ── Department fixtures ───────────────────────────────────────────────

// CustomDept inserts a custom (non-system) active department.
func (f *Fixtures) CustomDept(t *testing.T, ctx context.Context) *domain.Department {
	t.Helper()
	d, err := f.deptRepo.Insert(ctx, &domain.Department{
		Code:     f.uniqueCode("fx_dept"),
		Name:     "Fixture Department",
		IsSystem: false,
	})
	require.NoError(t, err)
	return d
}

// CustomDeptWithCode inserts a custom department with the given code.
func (f *Fixtures) CustomDeptWithCode(t *testing.T, ctx context.Context, code, name string) *domain.Department {
	t.Helper()
	d, err := f.deptRepo.Insert(ctx, &domain.Department{Code: code, Name: name, IsSystem: false})
	require.NoError(t, err)
	return d
}

// SystemDept inserts a system department (is_system=true).
func (f *Fixtures) SystemDept(t *testing.T, ctx context.Context) *domain.Department {
	t.Helper()
	d, err := f.deptRepo.Insert(ctx, &domain.Department{
		Code:     f.uniqueCode("fx_sys"),
		Name:     "Fixture System Dept",
		IsSystem: true,
	})
	require.NoError(t, err)
	return d
}

// RetiredDept inserts a department then retires it.
func (f *Fixtures) RetiredDept(t *testing.T, ctx context.Context) *domain.Department {
	t.Helper()
	d := f.CustomDept(t, ctx)
	f2 := false
	retired, err := f.deptRepo.Update(ctx, d.ID, nil, &f2, d.RecordVersion)
	require.NoError(t, err)
	return retired
}

// ManyDepts inserts n custom departments and returns them.
func (f *Fixtures) ManyDepts(t *testing.T, ctx context.Context, n int) []*domain.Department {
	t.Helper()
	out := make([]*domain.Department, n)
	for i := 0; i < n; i++ {
		out[i] = f.CustomDept(t, ctx)
	}
	return out
}

// ── Plan fixtures ─────────────────────────────────────────────────────

// PatchedPlan patches an existing seeded plan and returns the updated entity.
// Useful to put a plan into a known state with specific field values.
func (f *Fixtures) PatchedPlan(t *testing.T, ctx context.Context, code domain.TenantPlan, patch *domain.PlanPatch) *domain.Plan {
	t.Helper()
	p, err := f.planRepo.Update(ctx, code, patch)
	require.NoError(t, err)
	return p
}

// StarterWithLimit patches the starter plan to have specific limits.
func (f *Fixtures) StarterWithLimit(t *testing.T, ctx context.Context, workflowLimit, tenderLimit int) *domain.Plan {
	t.Helper()
	wl := &workflowLimit
	tl := &tenderLimit
	return f.PatchedPlan(t, ctx, domain.PlanStarter, &domain.PlanPatch{
		WorkflowTemplateLimit: &wl,
		TenderLimit:           &tl,
		RecordVersion:         1,
	})
}

// StarterUnlimited patches the starter plan to have null (unlimited) limits.
func (f *Fixtures) StarterUnlimited(t *testing.T, ctx context.Context) *domain.Plan {
	t.Helper()
	var nilPtr *int
	return f.PatchedPlan(t, ctx, domain.PlanStarter, &domain.PlanPatch{
		WorkflowTemplateLimit: &nilPtr,
		TenderLimit:           &nilPtr,
		RecordVersion:         1,
	})
}

// EnterpriseWithFeatureSet patches the enterprise plan with custom feature flags.
func (f *Fixtures) EnterpriseWithFeatureSet(t *testing.T, ctx context.Context, fs map[string]any) *domain.Plan {
	t.Helper()
	return f.PatchedPlan(t, ctx, domain.PlanEnterprise, &domain.PlanPatch{
		FeatureSet:    fs,
		RecordVersion: 1,
	})
}
