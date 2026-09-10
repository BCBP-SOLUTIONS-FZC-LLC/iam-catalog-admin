//go:build integration

// Package postgres_test is the Postgres-backed integration suite —
// black-box, mirrors iam-org-membership's test/postgres/ convention.
// Spins up a real Postgres container, runs this service's own migration
// (000001_init_schema — schema, triggers, and role/grants in one file —
// end to end, which doubles as the migration test), and exercises the
// exported repository constructors directly.
package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	pgadapter "github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/adapter/outbound/postgres"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/test/dbseed"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-pgcommon/pkg/pgcommon"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// setupTestDB spins up a throwaway Postgres container, runs this
// service's migrations against it, and returns:
//
//	pool    — pgcommon.Pool, used to construct the real repositories.
//	rawPool — dbseed.Pool (pgcommon underneath) for one-off raw SQL
//	          (trigger assertions) that has no repository method of its
//	          own — mirrors iam-org-membership / iam-realm-provisioner's
//	          test/dbseed convention. Tests never open a raw pgxpool.
func setupTestDB(t *testing.T) (*pgcommon.Pool, *dbseed.Pool) {
	t.Helper()
	ctx := context.Background()

	container, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("catalog_admin_test"),
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

	require.NoError(t, pgadapter.RunMigrations(ctx, dsn, nil))

	pool, err := pgcommon.NewPool(ctx, pgcommon.Config{DSN: dsn, MaxConns: 5})
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	rawPool, err := dbseed.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(rawPool.Close)

	return pool, rawPool
}

// findDeptByCode looks up a seeded department by its code via List — the
// repository has no exported FindByCode of its own (removed as dead code:
// no CAT endpoint looks up a department by code, only by ID, LLD §5.3).
// Test-only convenience for fetching a known seed row's ID/RecordVersion.
func findDeptByCode(t *testing.T, ctx context.Context, repo *pgadapter.DepartmentRepository, code string) domain.Department {
	t.Helper()
	all, err := repo.List(ctx, false)
	require.NoError(t, err)
	for _, d := range all {
		if d.Code == code {
			return d
		}
	}
	t.Fatalf("seeded department with code %q not found", code)
	return domain.Department{}
}

func TestDepartmentRepository_CreatePatchLifecycle(t *testing.T) {
	t.Parallel()
	pool, rawPool := setupTestDB(t)
	repo := pgadapter.NewDepartmentRepository(pool)
	ctx := context.Background()

	// Seed rows from the migration must already be present (D-4/OP-5).
	seeded, err := repo.List(ctx, false)
	require.NoError(t, err)
	require.Len(t, seeded, 5)

	activeSeeded, err := repo.List(ctx, true)
	require.NoError(t, err)
	require.Len(t, activeSeeded, 5, "all 5 seeded system departments start is_active=true")

	created, err := repo.Insert(ctx, &domain.Department{Code: "LEGAL_OPS", Name: "Legal Ops", IsSystem: false, IsActive: true})
	require.NoError(t, err)
	require.Equal(t, int64(1), created.RecordVersion)

	// D-2/D-10: code is immutable — the repository never attempts to set
	// it, so this is really a migration-trigger test: attempting a raw
	// UPDATE of code must fail.
	_, err = rawPool.Exec(ctx, `UPDATE departments SET code = 'CHANGED' WHERE id = $1`, created.ID)
	require.Error(t, err, "trg_department_code_immutable must block a code change")

	newName := "Legal Operations"
	updated, err := repo.Update(ctx, created.ID, &newName, nil, created.RecordVersion)
	require.NoError(t, err)
	require.Equal(t, "Legal Operations", updated.Name)
	require.Equal(t, int64(2), updated.RecordVersion)

	// Optimistic lock: stale record_version must 409.
	_, err = repo.Update(ctx, created.ID, &newName, nil, created.RecordVersion)
	require.Error(t, err)
	var de *domain.DomainError
	require.ErrorAs(t, err, &de)
	require.Equal(t, domain.ErrOptimisticLockConflict.Error(), de.Code)

	// D-4/OP-3: hard delete must be blocked at the trigger layer.
	_, err = rawPool.Exec(ctx, `DELETE FROM departments WHERE id = $1`, created.ID)
	require.Error(t, err, "trg_prevent_department_delete must block a hard delete")
}

func TestDepartmentRepository_SystemDepartmentCannotBeRetired(t *testing.T) {
	t.Parallel()
	pool, _ := setupTestDB(t)
	repo := pgadapter.NewDepartmentRepository(pool)
	ctx := context.Background()

	eng := findDeptByCode(t, ctx, repo, "ENGINEERING")
	require.True(t, eng.IsSystem)

	inactive := false
	_, err := repo.Update(ctx, eng.ID, nil, &inactive, eng.RecordVersion)
	require.Error(t, err, "chk_system_department_active must block retiring a system department")
}

func TestDepartmentRepository_DuplicateCodeConflict(t *testing.T) {
	t.Parallel()
	pool, _ := setupTestDB(t)
	repo := pgadapter.NewDepartmentRepository(pool)
	ctx := context.Background()

	_, err := repo.Insert(ctx, &domain.Department{Code: "ENGINEERING", Name: "Dup", IsActive: true})
	require.Error(t, err)
	var de *domain.DomainError
	require.ErrorAs(t, err, &de)
	require.Equal(t, domain.ErrConflict.Error(), de.Code)
}

func TestDepartmentRepository_FindByID_NotFound(t *testing.T) {
	t.Parallel()
	pool, _ := setupTestDB(t)
	repo := pgadapter.NewDepartmentRepository(pool)
	_, err := repo.FindByID(context.Background(), uuid.New())
	require.Error(t, err)
	var de *domain.DomainError
	require.ErrorAs(t, err, &de)
	require.Equal(t, domain.ErrDepartmentNotFound.Error(), de.Code)
}

func TestDepartmentRepository_Insert_NonUniqueCheckViolationPassesThrough(t *testing.T) {
	t.Parallel()
	pool, _ := setupTestDB(t)
	repo := pgadapter.NewDepartmentRepository(pool)
	// departments_name_not_empty is a CHECK constraint, not the unique-code
	// constraint the Insert path special-cases (SQLSTATE 23505) — this
	// exercises Insert's scanErr fallthrough for any other constraint
	// violation.
	_, err := repo.Insert(context.Background(), &domain.Department{Code: "EMPTYNAME", Name: "", IsActive: true})
	require.Error(t, err)
	var de *domain.DomainError
	require.False(t, errors.As(err, &de), "a non-duplicate-code constraint violation must not be mapped to a DomainError")
}

func TestDepartmentRepository_Update_NoFieldsDelegatesToFindByID(t *testing.T) {
	t.Parallel()
	pool, _ := setupTestDB(t)
	repo := pgadapter.NewDepartmentRepository(pool)
	ctx := context.Background()

	eng := findDeptByCode(t, ctx, repo, "ENGINEERING")

	got, err := repo.Update(ctx, eng.ID, nil, nil, eng.RecordVersion)
	require.NoError(t, err)
	require.Equal(t, eng.ID, got.ID)
	require.Equal(t, eng.RecordVersion, got.RecordVersion, "no-field Update must not bump record_version")
}

func TestDepartmentRepository_Update_CombinedNameAndIsActive(t *testing.T) {
	t.Parallel()
	pool, _ := setupTestDB(t)
	repo := pgadapter.NewDepartmentRepository(pool)
	ctx := context.Background()

	created, err := repo.Insert(ctx, &domain.Department{Code: "OPS", Name: "Ops", IsSystem: false, IsActive: true})
	require.NoError(t, err)

	newName := "Operations"
	inactive := false
	updated, err := repo.Update(ctx, created.ID, &newName, &inactive, created.RecordVersion)
	require.NoError(t, err, "combining name + is_active in one Update must produce a valid SET clause")
	require.Equal(t, "Operations", updated.Name)
	require.False(t, updated.IsActive)
}

func TestDepartmentRepository_Update_UnknownIDIsNotFound(t *testing.T) {
	t.Parallel()
	pool, _ := setupTestDB(t)
	repo := pgadapter.NewDepartmentRepository(pool)
	newName := "Ghost"
	_, err := repo.Update(context.Background(), uuid.New(), &newName, nil, 1)
	require.Error(t, err)
	var de *domain.DomainError
	require.ErrorAs(t, err, &de)
	require.Equal(t, domain.ErrDepartmentNotFound.Error(), de.Code)
}

func TestPlanRepository_Update_NilPatchRejected(t *testing.T) {
	t.Parallel()
	pool, _ := setupTestDB(t)
	repo := pgadapter.NewPlanRepository(pool)
	_, err := repo.Update(context.Background(), domain.PlanStarter, nil)
	require.Error(t, err)
	var de *domain.DomainError
	require.ErrorAs(t, err, &de)
	require.Equal(t, domain.ErrValidation.Error(), de.Code)
}

func TestPlanRepository_Update_EmptyPatchRejected(t *testing.T) {
	t.Parallel()
	pool, _ := setupTestDB(t)
	repo := pgadapter.NewPlanRepository(pool)
	_, err := repo.Update(context.Background(), domain.PlanStarter, &domain.PlanPatch{RecordVersion: 1})
	require.Error(t, err)
	var de *domain.DomainError
	require.ErrorAs(t, err, &de)
	require.Equal(t, domain.ErrNoMutableField.Error(), de.Code)
}

func TestPlanRepository_Update_FeatureSetMarshalError(t *testing.T) {
	t.Parallel()
	pool, _ := setupTestDB(t)
	repo := pgadapter.NewPlanRepository(pool)
	_, err := repo.Update(context.Background(), domain.PlanStarter, &domain.PlanPatch{
		RecordVersion: 1,
		FeatureSet:    map[string]any{"bad": make(chan int)},
	})
	require.Error(t, err)
	var de *domain.DomainError
	require.False(t, errors.As(err, &de), "a json.Marshal failure is a raw error, not a DomainError")
}

func TestPlanRepository_Update_AllFieldsAtOnce(t *testing.T) {
	t.Parallel()
	pool, _ := setupTestDB(t)
	repo := pgadapter.NewPlanRepository(pool)
	ctx := context.Background()

	before, err := repo.FindByCode(ctx, domain.PlanPro)
	require.NoError(t, err)

	name := "Pro v2"
	wfLimit := 25
	wfLimitPtr := &wfLimit
	tenderLimit := 40
	tenderLimitPtr := &tenderLimit
	trialDays := 14
	sso := true
	branding := domain.BrandingLogo
	updated, err := repo.Update(ctx, domain.PlanPro, &domain.PlanPatch{
		DisplayName:           &name,
		WorkflowTemplateLimit: &wfLimitPtr,
		TenderLimit:           &tenderLimitPtr,
		TrialDurationDays:     &trialDays,
		SSOEnabled:            &sso,
		CustomBranding:        &branding,
		FeatureSet:            map[string]any{"custom_workflows": true},
		RecordVersion:         before.RecordVersion,
	})
	require.NoError(t, err)
	require.Equal(t, "Pro v2", updated.DisplayName)
	require.Equal(t, 25, *updated.WorkflowTemplateLimit)
	require.Equal(t, 40, *updated.TenderLimit)
	require.Equal(t, 14, updated.TrialDurationDays)
	require.True(t, updated.SSOEnabled)
	require.Equal(t, domain.BrandingLogo, updated.CustomBranding)
	require.Equal(t, true, updated.FeatureSet["custom_workflows"])
}

func TestPlanRepository_ScanPlan_MalformedFeatureSetJSON(t *testing.T) {
	t.Parallel()
	pool, rawPool := setupTestDB(t)
	repo := pgadapter.NewPlanRepository(pool)
	ctx := context.Background()

	// feature_set has no CHECK restricting it to a JSON object — corrupt it
	// with a JSON array so scanPlan's json.Unmarshal into map[string]any fails.
	_, err := rawPool.Exec(ctx, `UPDATE plans SET feature_set = '[1,2,3]'::jsonb WHERE code = 'starter'`)
	require.NoError(t, err)

	// wrapConnErr has no special case for a json.Unmarshal failure, so it
	// falls through to the generic ErrDependencyUnavailable mapping.
	_, err = repo.FindByCode(ctx, domain.PlanStarter)
	require.Error(t, err)
	var de *domain.DomainError
	require.ErrorAs(t, err, &de)
	require.Equal(t, domain.ErrDependencyUnavailable.Error(), de.Code)

	_, err = repo.List(ctx)
	require.Error(t, err, "List must propagate the same scan error for the corrupted row")
}

func TestPlanRepository_FindByCode_NotFoundAfterRowRemoved(t *testing.T) {
	t.Parallel()
	pool, rawPool := setupTestDB(t)
	repo := pgadapter.NewPlanRepository(pool)
	ctx := context.Background()

	// Unlike departments, plans has no delete-prevention trigger — the only
	// way to genuinely hit repo-level "not found" for a code that's a valid
	// tenant_plan enum member (FindByCode/Update never see an invalid code;
	// the service layer rejects those before the repo is called).
	_, err := rawPool.Exec(ctx, `DELETE FROM plans WHERE code = 'starter'`)
	require.NoError(t, err)

	_, err = repo.FindByCode(ctx, domain.PlanStarter)
	require.Error(t, err)
	var de *domain.DomainError
	require.ErrorAs(t, err, &de)
	require.Equal(t, domain.ErrPlanNotFound.Error(), de.Code)
}

func TestPlanRepository_Update_NotFoundAfterRowRemoved(t *testing.T) {
	t.Parallel()
	pool, rawPool := setupTestDB(t)
	repo := pgadapter.NewPlanRepository(pool)
	ctx := context.Background()

	_, err := rawPool.Exec(ctx, `DELETE FROM plans WHERE code = 'starter'`)
	require.NoError(t, err)

	name := "Ghost"
	_, err = repo.Update(ctx, domain.PlanStarter, &domain.PlanPatch{DisplayName: &name, RecordVersion: 1})
	require.Error(t, err)
	var de *domain.DomainError
	require.ErrorAs(t, err, &de)
	require.Equal(t, domain.ErrPlanNotFound.Error(), de.Code)
}

func TestPlanRepository_SeededTiersAndPatch(t *testing.T) {
	t.Parallel()
	pool, _ := setupTestDB(t)
	repo := pgadapter.NewPlanRepository(pool)
	ctx := context.Background()

	all, err := repo.List(ctx)
	require.NoError(t, err)
	require.Len(t, all, 3)

	enterprise, err := repo.FindByCode(ctx, domain.PlanEnterprise)
	require.NoError(t, err)
	require.Nil(t, enterprise.WorkflowTemplateLimit, "CAT-D6: enterprise ships NULL = unlimited")
	require.Nil(t, enterprise.TenderLimit)

	var nilLimit *int
	updated, err := repo.Update(ctx, domain.PlanStarter, &domain.PlanPatch{
		WorkflowTemplateLimit: &nilLimit, RecordVersion: 1,
	})
	require.NoError(t, err)
	require.Nil(t, updated.WorkflowTemplateLimit)
	require.Equal(t, int64(2), updated.RecordVersion)

	// Optimistic lock conflict on stale version.
	name := "Starter"
	_, err = repo.Update(ctx, domain.PlanStarter, &domain.PlanPatch{DisplayName: &name, RecordVersion: 1})
	require.Error(t, err)
	var de *domain.DomainError
	require.ErrorAs(t, err, &de)
	require.Equal(t, domain.ErrOptimisticLockConflict.Error(), de.Code)
}

func TestPlanRepository_TouchRowBumpsVersionAndTimestamp(t *testing.T) {
	t.Parallel()
	pool, _ := setupTestDB(t)
	repo := pgadapter.NewPlanRepository(pool)
	ctx := context.Background()

	before, err := repo.FindByCode(ctx, domain.PlanPro)
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)
	sso := true
	after, err := repo.Update(ctx, domain.PlanPro, &domain.PlanPatch{SSOEnabled: &sso, RecordVersion: before.RecordVersion})
	require.NoError(t, err)
	require.True(t, after.UpdatedAt.After(before.UpdatedAt))
	require.Equal(t, before.RecordVersion+1, after.RecordVersion)
}
