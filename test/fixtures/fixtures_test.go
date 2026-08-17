//go:build integration

// Package fixtures_test exercises the fixture builders against a real Postgres
// container — verifies that fixture-seeded data is in the exact state tests
// will assume.  These run alongside test/postgres/ (same -tags=integration).
package fixtures_test

import (
	"context"
	"testing"

	pgadapter "github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/adapter/outbound/postgres"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/test/fixtures"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-pgcommon/pkg/pgcommon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func setupFixtureDB(t *testing.T) (*pgcommon.Pool, *fixtures.Fixtures) {
	t.Helper()
	ctx := context.Background()

	container, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("catalog_admin_fixtures"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
		),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	require.NoError(t, pgadapter.RunMigrations(ctx, dsn))

	pool, err := pgcommon.NewPool(ctx, pgcommon.Config{DSN: dsn, MaxConns: 5})
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	return pool, fixtures.New(pool)
}

// ── Department fixture tests ──────────────────────────────────────────

// Verifies that CustomDept creates a dept with is_active=true and is_system=false.
func TestFixture_CustomDept_ActiveAndNonSystem(t *testing.T) {
	pool, fx := setupFixtureDB(t)
	ctx := context.Background()
	_ = pool

	d := fx.CustomDept(t, ctx)
	assert.True(t, d.IsActive, "custom dept must be active by default")
	assert.False(t, d.IsSystem, "custom dept must not be a system dept")
	assert.EqualValues(t, 1, d.RecordVersion)
}

// Verifies that SystemDept creates a dept with is_system=true.
func TestFixture_SystemDept_IsSystemTrue(t *testing.T) {
	_, fx := setupFixtureDB(t)
	ctx := context.Background()

	d := fx.SystemDept(t, ctx)
	assert.True(t, d.IsSystem)
	assert.True(t, d.IsActive)
}

// Verifies that RetiredDept creates a dept with is_active=false.
func TestFixture_RetiredDept_IsActiveFalse(t *testing.T) {
	_, fx := setupFixtureDB(t)
	ctx := context.Background()

	d := fx.RetiredDept(t, ctx)
	assert.False(t, d.IsActive, "RetiredDept fixture must produce is_active=false")
}

// Verifies that ManyDepts creates n distinct departments.
func TestFixture_ManyDepts_CreatesDistinctRows(t *testing.T) {
	_, fx := setupFixtureDB(t)
	ctx := context.Background()

	depts := fx.ManyDepts(t, ctx, 5)
	require.Len(t, depts, 5)

	codes := map[string]bool{}
	for _, d := range depts {
		require.False(t, codes[d.Code], "all fixture codes must be unique")
		codes[d.Code] = true
	}
}

// Verifies uniqueCode generates distinct codes across calls.
func TestFixture_CustomDeptWithCode_StoresExactCode(t *testing.T) {
	_, fx := setupFixtureDB(t)
	ctx := context.Background()

	d := fx.CustomDeptWithCode(t, ctx, "fx-exact-code", "Exact Name")
	assert.Equal(t, "fx-exact-code", d.Code)
	assert.Equal(t, "Exact Name", d.Name)
}

// ── Plan fixture tests ────────────────────────────────────────────────

// Verifies StarterWithLimit produces correct limit values.
func TestFixture_StarterWithLimit_StoresLimits(t *testing.T) {
	_, fx := setupFixtureDB(t)
	ctx := context.Background()

	p := fx.StarterWithLimit(t, ctx, 50, 100)
	require.NotNil(t, p.WorkflowTemplateLimit)
	require.NotNil(t, p.TenderLimit)
	assert.Equal(t, 50, *p.WorkflowTemplateLimit)
	assert.Equal(t, 100, *p.TenderLimit)
}

// Verifies StarterUnlimited produces null limits (CAT-D6).
func TestFixture_StarterUnlimited_NullLimits(t *testing.T) {
	_, fx := setupFixtureDB(t)
	ctx := context.Background()

	p := fx.StarterUnlimited(t, ctx)
	assert.Nil(t, p.WorkflowTemplateLimit, "unlimited fixture must have nil workflow_template_limit")
	assert.Nil(t, p.TenderLimit, "unlimited fixture must have nil tender_limit")
}

// Verifies EnterpriseWithFeatureSet stores the provided feature flags.
func TestFixture_EnterpriseWithFeatureSet_StoresFlags(t *testing.T) {
	_, fx := setupFixtureDB(t)
	ctx := context.Background()

	fs := map[string]any{"sso": true, "support_tier": "enterprise", "max_api_calls": 10000}
	p := fx.EnterpriseWithFeatureSet(t, ctx, fs)
	assert.Equal(t, true, p.FeatureSet["sso"])
	assert.Equal(t, "enterprise", p.FeatureSet["support_tier"])
}

// Verifies PatchedPlan returns the correct updated plan.
func TestFixture_PatchedPlan_RecordVersionIncremented(t *testing.T) {
	_, fx := setupFixtureDB(t)
	ctx := context.Background()

	name := "Patched By Fixture"
	p := fx.PatchedPlan(t, ctx, domain.PlanPro, &domain.PlanPatch{
		DisplayName:   &name,
		RecordVersion: 1,
	})
	assert.Equal(t, "Patched By Fixture", p.DisplayName)
	assert.EqualValues(t, 2, p.RecordVersion)
}

// ── Fixture-driven scenario tests ────────────────────────────────────

// CA6-H-02: active_only=true filter — fixture provides the known data state.
func TestFixture_ActiveOnlyFilter_ExcludesRetiredDepts(t *testing.T) {
	pool, fx := setupFixtureDB(t)
	ctx := context.Background()
	deptRepo := pgadapter.NewDepartmentRepository(pool)

	active := fx.CustomDept(t, ctx)
	retired := fx.RetiredDept(t, ctx)

	activeOnly, err := deptRepo.List(ctx, true)
	require.NoError(t, err)

	activeIDs := map[string]bool{}
	for _, d := range activeOnly {
		activeIDs[d.ID.String()] = true
	}
	assert.True(t, activeIDs[active.ID.String()], "active dept must appear in active_only list")
	assert.False(t, activeIDs[retired.ID.String()], "retired dept must not appear in active_only list")
}

// CA2-OL-01: stale record_version rejected — fixture provides dept at known version.
func TestFixture_OptimisticLock_StaleVersionRejected(t *testing.T) {
	pool, fx := setupFixtureDB(t)
	ctx := context.Background()
	deptRepo := pgadapter.NewDepartmentRepository(pool)

	d := fx.CustomDept(t, ctx) // version = 1
	name := "First"
	_, err := deptRepo.Update(ctx, d.ID, &name, nil, d.RecordVersion) // bumps to 2
	require.NoError(t, err)

	name2 := "Second"
	_, err = deptRepo.Update(ctx, d.ID, &name2, nil, d.RecordVersion) // stale version=1
	require.Error(t, err, "stale record_version must fail with OCC error")

	var de *domain.DomainError
	require.ErrorAs(t, err, &de)
	assert.Equal(t, domain.ErrOptimisticLockConflict.Error(), de.Code)
}

// CA5-H-18: two consecutive plan patches — fixture starts at known version.
func TestFixture_ConsecutivePlanPatches_VersionMonotonic(t *testing.T) {
	pool, fx := setupFixtureDB(t)
	ctx := context.Background()
	planRepo := pgadapter.NewPlanRepository(pool)

	// Start from a known state
	name1 := "First Patch"
	p1 := fx.PatchedPlan(t, ctx, domain.PlanStarter, &domain.PlanPatch{
		DisplayName:   &name1,
		RecordVersion: 1,
	})
	require.EqualValues(t, 2, p1.RecordVersion)

	name2 := "Second Patch"
	p2, err := planRepo.Update(ctx, domain.PlanStarter, &domain.PlanPatch{
		DisplayName:   &name2,
		RecordVersion: p1.RecordVersion,
	})
	require.NoError(t, err)
	assert.EqualValues(t, 3, p2.RecordVersion, "version must increment monotonically")
}

// CA-DI-05: internal plans record_versions map matches per-code versions.
func TestFixture_InternalPlans_RecordVersionsConsistent(t *testing.T) {
	pool, fx := setupFixtureDB(t)
	ctx := context.Background()
	planRepo := pgadapter.NewPlanRepository(pool)

	// Patch pro plan so its version is known
	name := "Fixture Pro"
	patched := fx.PatchedPlan(t, ctx, domain.PlanPro, &domain.PlanPatch{
		DisplayName:   &name,
		RecordVersion: 1,
	})
	require.EqualValues(t, 2, patched.RecordVersion)

	// List all plans — version must match what was returned by patch
	plans, err := planRepo.List(ctx)
	require.NoError(t, err)
	for _, p := range plans {
		if p.Code == domain.PlanPro {
			assert.Equal(t, patched.RecordVersion, p.RecordVersion,
				"plan list must reflect the updated record_version")
		}
	}
}
