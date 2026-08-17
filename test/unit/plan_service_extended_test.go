// Additional unit tests for PlanService — no I/O, no Docker required.
// Fakes (fakePlanRepo, fakeCache) are defined in plan_service_test.go
// and department_service_test.go respectively.
package unit_test

import (
	"context"
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/core/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newPlanSvc() *service.PlanService {
	return service.NewPlanService(newFakePlanRepo(), newFakeCache())
}

func patchPlan(t *testing.T, svc *service.PlanService, code domain.TenantPlan, patch *domain.PlanPatch) (*domain.Plan, error) {
	t.Helper()
	return svc.Patch(context.Background(), code, patch)
}

func intPtr(n int) *int       { return &n }
func strPtr(s string) *string { return &s }

// ── CA5-H-06: zero workflow limit stored as 0, not null (CAT-D6) ──────

// Scenario CA5-H-06
func TestPlanService_Patch_ZeroWorkflowLimit_StoredAsZero(t *testing.T) {
	svc := newPlanSvc()
	p, err := patchPlan(t, svc, domain.PlanStarter, &domain.PlanPatch{
		WorkflowTemplateLimit: func() **int { v := intPtr(0); return &v }(),
		RecordVersion:         1,
	})
	require.NoError(t, err)
	require.NotNil(t, p.WorkflowTemplateLimit, "zero must be stored as 0, not as null (unlimited)")
	assert.Equal(t, 0, *p.WorkflowTemplateLimit)
}

// ── CA5-H-08: zero tender limit stored as 0, not null ─────────────────

// Scenario CA5-H-08
func TestPlanService_Patch_ZeroTenderLimit_StoredAsZero(t *testing.T) {
	svc := newPlanSvc()
	p, err := patchPlan(t, svc, domain.PlanStarter, &domain.PlanPatch{
		TenderLimit:   func() **int { v := intPtr(0); return &v }(),
		RecordVersion: 1,
	})
	require.NoError(t, err)
	require.NotNil(t, p.TenderLimit)
	assert.Equal(t, 0, *p.TenderLimit)
}

// ── CA5-H-14: trial_duration_days=0 is valid ──────────────────────────

// Scenario CA5-H-14
func TestPlanService_Patch_TrialDurationDaysZero_Valid(t *testing.T) {
	svc := newPlanSvc()
	p, err := patchPlan(t, svc, domain.PlanStarter, &domain.PlanPatch{
		TrialDurationDays: intPtr(0),
		RecordVersion:     1,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, p.TrialDurationDays)
}

// ── CA-BV-04: feature_set {key: null} is valid (nil is scalar) ────────

// Scenario CA-BV-04
func TestPlanService_Patch_FeatureSetNullValue_AcceptedAsScalar(t *testing.T) {
	svc := newPlanSvc()
	fs := map[string]any{"nullable_flag": nil}
	p, err := patchPlan(t, svc, domain.PlanStarter, &domain.PlanPatch{
		FeatureSet:    fs,
		RecordVersion: 1,
	})
	require.NoError(t, err)
	assert.Contains(t, p.FeatureSet, "nullable_flag")
}

// ── CA-BV-05 / CA-BL-04: empty feature_set {} clears all keys ─────────

// Scenarios CA-BV-05, CA-BL-04
func TestPlanService_Patch_FeatureSetEmptyMap_ClearsKeys(t *testing.T) {
	svc := newPlanSvc()
	// First set some keys
	_, err := patchPlan(t, svc, domain.PlanStarter, &domain.PlanPatch{
		FeatureSet:    map[string]any{"key": "value"},
		RecordVersion: 1,
	})
	require.NoError(t, err)

	// Now clear with empty map (not nil — empty map is distinct from absent)
	p, err := patchPlan(t, svc, domain.PlanStarter, &domain.PlanPatch{
		FeatureSet:    map[string]any{},
		RecordVersion: 2,
	})
	require.NoError(t, err)
	assert.Empty(t, p.FeatureSet, "empty map must clear all feature_set keys")
}

// ── CA-BV-06: nil feature_set in patch leaves existing keys unchanged ─

// Scenario CA-BV-06
func TestPlanService_Patch_FeatureSetNil_LeavesExistingKeysUnchanged(t *testing.T) {
	svc := newPlanSvc()
	_, err := patchPlan(t, svc, domain.PlanStarter, &domain.PlanPatch{
		FeatureSet:    map[string]any{"initial": "data"},
		RecordVersion: 1,
	})
	require.NoError(t, err)

	// FeatureSet=nil means "absent" — no update to feature_set field
	p, err := patchPlan(t, svc, domain.PlanStarter, &domain.PlanPatch{
		DisplayName:   strPtr("BV06 Check"),
		FeatureSet:    nil,
		RecordVersion: 2,
	})
	require.NoError(t, err)
	assert.Contains(t, p.FeatureSet, "initial", "nil feature_set must leave existing keys unchanged")
}

// ── CA5-BL-01: absent field leaves DB value unchanged (tri-state) ─────

// Scenario CA5-BL-01
func TestPlanService_Patch_AbsentField_LeftUnchanged(t *testing.T) {
	svc := newPlanSvc()
	// starter is seeded with workflow_template_limit=5; patch only display_name
	p, err := patchPlan(t, svc, domain.PlanStarter, &domain.PlanPatch{
		DisplayName:   strPtr("Only Name Changed"),
		RecordVersion: 1,
	})
	require.NoError(t, err)
	assert.Equal(t, "Only Name Changed", p.DisplayName)
	require.NotNil(t, p.WorkflowTemplateLimit)
	assert.Equal(t, 5, *p.WorkflowTemplateLimit, "absent field must leave existing value unchanged")
}

// ── CA-BL-02: missing record_version → 0 → OCC conflict ──────────────

// Scenario CA-BL-02
func TestPlanService_Patch_VersionZero_AlwaysConflicts(t *testing.T) {
	svc := newPlanSvc()
	_, err := patchPlan(t, svc, domain.PlanStarter, &domain.PlanPatch{
		DisplayName:   strPtr("X"),
		RecordVersion: 0, // missing record_version defaults to 0 in binding
	})
	require.Error(t, err)
	assert.Equal(t, domain.ErrOptimisticLockConflict.Error(), errCode(t, err))
}

// ── CA5-H-19: code field in patch body is not part of PlanPatch ───────

// Scenario CA5-H-19
func TestPlanService_Patch_RecordVersionIncrements_AfterUpdate(t *testing.T) {
	svc := newPlanSvc()
	p1, err := patchPlan(t, svc, domain.PlanStarter, &domain.PlanPatch{
		DisplayName:   strPtr("First"),
		RecordVersion: 1,
	})
	require.NoError(t, err)
	assert.EqualValues(t, 2, p1.RecordVersion)

	p2, err := patchPlan(t, svc, domain.PlanStarter, &domain.PlanPatch{
		DisplayName:   strPtr("Second"),
		RecordVersion: 2,
	})
	require.NoError(t, err)
	assert.EqualValues(t, 3, p2.RecordVersion, "version must increment monotonically")
}

// ── CA4A-BL-02: cache unavailable → plan list still succeeds ──────────

// Scenario CA4A-BL-02
func TestPlanService_List_CacheUnavailable_FailsOpen(t *testing.T) {
	svc := service.NewPlanService(newFakePlanRepo(), &alwaysErrCache{})
	plans, err := svc.List(context.Background())
	require.NoError(t, err, "cache failure must be swallowed — fail-open (CAT-FAIL-1)")
	assert.NotEmpty(t, plans)
}
