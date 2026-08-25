//go:build integration

// DB-level constraint and trigger tests — require real Postgres.
// Uses the same setupTestDB harness as integration_test.go.
package postgres_test

import (
	"context"
	"testing"

	pgadapter "github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/adapter/outbound/postgres"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/core/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── CA2-BL-03: system department name is immutable (DB trigger) ───────

// Scenario CA2-BL-03
func TestDepartmentRepository_SystemDeptName_Immutable(t *testing.T) {
	t.Parallel()
	pool, _ := setupTestDB(t)
	ctx := context.Background()
	repo := pgadapter.NewDepartmentRepository(pool)

	// IsActive must be true — chk_system_department_active blocks inactive system depts.
	d, err := repo.Insert(ctx, &domain.Department{Code: "sys-pg01", Name: "Original", IsSystem: true, IsActive: true})
	require.NoError(t, err)

	newName := "Renamed"
	_, err = repo.Update(ctx, d.ID, &newName, nil, d.RecordVersion)
	require.Error(t, err, "trg_system_department_name_immutable must reject renaming a system department")
}

// ── CA-BL-03: record_version is always ≥ 1 (DB CHECK record_version > 0) ──

// Scenario CA-BL-03
func TestDepartmentRepository_RecordVersion_AlwaysGT0(t *testing.T) {
	t.Parallel()
	pool, _ := setupTestDB(t)
	ctx := context.Background()
	repo := pgadapter.NewDepartmentRepository(pool)

	d, err := repo.Insert(ctx, &domain.Department{Code: "rv-pg02", Name: "RV Check"})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, d.RecordVersion, int64(1))
}

// ── CA-DI-03: any data change bumps record_version (trigger WHEN DISTINCT FROM) ──

// Scenario CA-DI-03
// The trigger is WHEN (OLD.* IS DISTINCT FROM NEW.*) — it fires only when data
// actually changes. This test verifies the trigger fires when name differs.
func TestDepartmentRepository_DataChangePatch_VersionIncrements(t *testing.T) {
	t.Parallel()
	pool, _ := setupTestDB(t)
	ctx := context.Background()
	repo := pgadapter.NewDepartmentRepository(pool)

	d, err := repo.Insert(ctx, &domain.Department{Code: "chng-pg03", Name: "Original Name", IsActive: true})
	require.NoError(t, err)
	require.Equal(t, int64(1), d.RecordVersion)

	newName := "Updated Name" // different from "Original Name" — trigger WILL fire
	updated, err := repo.Update(ctx, d.ID, &newName, nil, d.RecordVersion)
	require.NoError(t, err)
	assert.Equal(t, int64(2), updated.RecordVersion,
		"trg_touch_departments must bump record_version when data changes")
}

// ── CA3-BL-01: delete blocked by DB trigger ───────────────────────────

// Scenario CA3-BL-01 (DB-level defense-in-depth)
func TestDepartmentRepository_HardDelete_BlockedByTrigger(t *testing.T) {
	t.Parallel()
	pool, rawPool := setupTestDB(t)
	ctx := context.Background()
	repo := pgadapter.NewDepartmentRepository(pool)

	d, err := repo.Insert(ctx, &domain.Department{Code: "del-pg04", Name: "Delete Blocked"})
	require.NoError(t, err)

	// Attempt raw DELETE — should be blocked by prevent_department_delete trigger
	_, err = rawPool.Exec(ctx, "DELETE FROM departments WHERE id = $1", d.ID)
	require.Error(t, err, "prevent_department_delete trigger must block hard deletes")
}

// ── CA1-CON-03: retired dept code still unique (constraint ignores is_active) ──

// Scenario CA1-CON-03 (DB-level)
func TestDepartmentRepository_RetiredCode_StillUniqueConstrained(t *testing.T) {
	t.Parallel()
	pool, _ := setupTestDB(t)
	ctx := context.Background()
	repo := pgadapter.NewDepartmentRepository(pool)

	d, err := repo.Insert(ctx, &domain.Department{Code: "retired-pg05", Name: "First"})
	require.NoError(t, err)

	// Retire it
	f := false
	_, err = repo.Update(ctx, d.ID, nil, &f, d.RecordVersion)
	require.NoError(t, err)

	// Try to create with same code
	_, err = repo.Insert(ctx, &domain.Department{Code: "retired-pg05", Name: "Second"})
	require.Error(t, err, "retired code must still violate unique constraint")
}

// ── CA5-H-06: zero limit stored as 0, not NULL ────────────────────────

// Scenario CA5-H-06 (DB-level)
func TestPlanRepository_ZeroLimit_StoredAsZeroNotNull(t *testing.T) {
	t.Parallel()
	pool, rawPool := setupTestDB(t)
	ctx := context.Background()
	repo := pgadapter.NewPlanRepository(pool)

	limit := 0
	limitPtr := &limit
	patch := &domain.PlanPatch{
		WorkflowTemplateLimit: &limitPtr,
		RecordVersion:         1,
	}
	_, err := repo.Update(ctx, domain.PlanStarter, patch)
	require.NoError(t, err)

	var stored *int
	err = rawPool.QueryRow(ctx,
		"SELECT workflow_template_limit FROM plans WHERE code = 'starter'").Scan(&stored)
	require.NoError(t, err)
	require.NotNil(t, stored, "zero limit must be stored as 0, not NULL")
	assert.Equal(t, 0, *stored)
}

// ── CA5-H-04/07: explicit null limit stored as NULL (unlimited, CAT-D6) ─

// Scenarios CA5-H-04, CA5-H-07
func TestPlanRepository_NullLimit_StoredAsNull(t *testing.T) {
	t.Parallel()
	pool, rawPool := setupTestDB(t)
	ctx := context.Background()
	repo := pgadapter.NewPlanRepository(pool)

	var nilPtr *int // nil = unlimited
	patch := &domain.PlanPatch{
		WorkflowTemplateLimit: &nilPtr,
		RecordVersion:         1,
	}
	_, err := repo.Update(ctx, domain.PlanStarter, patch)
	require.NoError(t, err)

	var stored *int
	err = rawPool.QueryRow(ctx,
		"SELECT workflow_template_limit FROM plans WHERE code = 'starter'").Scan(&stored)
	require.NoError(t, err)
	assert.Nil(t, stored, "explicit null must be stored as DB NULL (= unlimited per CAT-D6)")
}

// ── CA-BV-09/10: max int32 limits stored correctly ────────────────────

// Scenarios CA-BV-09, CA-BV-10
func TestPlanRepository_MaxIntLimit_Stored(t *testing.T) {
	t.Parallel()
	pool, _ := setupTestDB(t)
	ctx := context.Background()
	repo := pgadapter.NewPlanRepository(pool)

	maxVal := 2147483647
	maxPtr := &maxVal
	patch := &domain.PlanPatch{
		WorkflowTemplateLimit: &maxPtr,
		TenderLimit:           &maxPtr,
		RecordVersion:         1,
	}
	p, err := repo.Update(ctx, domain.PlanEnterprise, patch)
	require.NoError(t, err)
	require.NotNil(t, p.WorkflowTemplateLimit)
	require.NotNil(t, p.TenderLimit)
	assert.Equal(t, maxVal, *p.WorkflowTemplateLimit)
	assert.Equal(t, maxVal, *p.TenderLimit)
}

// ── CA-BV-04: feature_set null value stored as JSONB null ────────────

// Scenario CA-BV-04
func TestPlanRepository_FeatureSetNullValue_AcceptedAsScalar(t *testing.T) {
	t.Parallel()
	pool, _ := setupTestDB(t)
	ctx := context.Background()
	repo := pgadapter.NewPlanRepository(pool)

	fs := map[string]any{"nullable_flag": nil}
	patch := &domain.PlanPatch{
		FeatureSet:    fs,
		RecordVersion: 1,
	}
	p, err := repo.Update(ctx, domain.PlanStarter, patch)
	require.NoError(t, err)
	assert.Contains(t, p.FeatureSet, "nullable_flag")
}

// ── CA-NOEVT-01/05: no outbox_events table, no RLS GUC ───────────────

// Scenarios CA-NOEVT-01, CA-NOEVT-05
func TestDatabase_NoOutboxTable_NoRLSGUC(t *testing.T) {
	t.Parallel()
	_, rawPool := setupTestDB(t)
	ctx := context.Background()

	var exists bool
	err := rawPool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM information_schema.tables
         WHERE table_schema='public' AND table_name='outbox_events')`).Scan(&exists)
	require.NoError(t, err)
	assert.False(t, exists, "catalog-admin must have no outbox_events table (LLD §10)")

	var guc string
	err = rawPool.QueryRow(ctx,
		"SELECT COALESCE(current_setting('app.tenant_id', true), '')").Scan(&guc)
	require.NoError(t, err)
	assert.Empty(t, guc, "catalog-admin never sets app.tenant_id GUC — no RLS (LLD §10)")
}
