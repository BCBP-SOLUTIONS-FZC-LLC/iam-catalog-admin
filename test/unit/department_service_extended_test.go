// Additional unit tests for DepartmentService — no I/O, no Docker required.
// Fakes (fakeDepartmentRepo, fakeCache) are defined in department_service_test.go.
// Error assertion pattern: var de *domain.DomainError; require.ErrorAs; de.Code.
package unit_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/core/port"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/core/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── helpers ────────────────────────────────────────────────────────────

func newDeptSvc() *service.DepartmentService {
	return service.NewDepartmentService(newFakeDepartmentRepo(), newFakeCache())
}

func mustCreateDept(t *testing.T, svc *service.DepartmentService, code, name string, isSystem bool) *domain.Department {
	t.Helper()
	d, err := svc.Create(context.Background(), code, name, isSystem)
	require.NoError(t, err)
	return d
}

func errCode(t *testing.T, err error) string {
	t.Helper()
	var de *domain.DomainError
	require.ErrorAs(t, err, &de)
	return de.Code
}

// ── CA1-V-05: whitespace-only code rejected ────────────────────────────

// Scenario CA1-V-05
func TestDepartmentService_Create_WhitespaceOnlyCode_Rejected(t *testing.T) {
	svc := newDeptSvc()
	_, err := svc.Create(context.Background(), "   ", "Name", false)
	require.Error(t, err)
	assert.Equal(t, domain.ErrValidation.Error(), errCode(t, err))
}

// ── CA1-H-02/03: is_system field stored correctly ─────────────────────

// Scenario CA1-H-02
func TestDepartmentService_Create_IsSystemTrue_StoredCorrectly(t *testing.T) {
	svc := newDeptSvc()
	d := mustCreateDept(t, svc, "sys-u01", "System Dept", true)
	assert.True(t, d.IsSystem)
	assert.True(t, d.IsActive, "is_active must default true")
	assert.EqualValues(t, 1, d.RecordVersion)
}

// Scenario CA1-H-03
func TestDepartmentService_Create_IsSystemFalse_StoredCorrectly(t *testing.T) {
	svc := newDeptSvc()
	d := mustCreateDept(t, svc, "cust-u02", "Custom Dept", false)
	assert.False(t, d.IsSystem)
	assert.True(t, d.IsActive)
}

// ── CA1-FMT-04/05: record_version=1 and is_active=true on creation ────

// Scenarios CA1-FMT-04, CA1-FMT-05
func TestDepartmentService_Create_InitialRecordVersionAndIsActive(t *testing.T) {
	svc := newDeptSvc()
	d := mustCreateDept(t, svc, "rv-u03", "RV Dept", false)
	assert.EqualValues(t, 1, d.RecordVersion)
	assert.True(t, d.IsActive)
}

// ── CA1-CON-03: retired code is still unique ───────────────────────────

// Scenario CA1-CON-03
func TestDepartmentService_Create_RetiredCode_StillConflicts(t *testing.T) {
	svc := newDeptSvc()
	d := mustCreateDept(t, svc, "retire-u04", "Retire Me", false)

	// retire the dept
	f := false
	_, err := svc.Patch(context.Background(), d.ID, nil, &f, 1)
	require.NoError(t, err)

	// try to create a new dept with the same code
	_, err = svc.Create(context.Background(), "retire-u04", "Reuse Attempt", false)
	require.Error(t, err)
	assert.Equal(t, domain.ErrConflict.Error(), errCode(t, err))
}

// ── CA2-H-05/06: idempotent retire / reactivate ───────────────────────

// Scenario CA2-H-05
func TestDepartmentService_Patch_RetireAlreadyRetired_Idempotent(t *testing.T) {
	svc := newDeptSvc()
	d := mustCreateDept(t, svc, "idem-retire-u05", "Idempotent Retire", false)
	f := false

	_, err := svc.Patch(context.Background(), d.ID, nil, &f, 1)
	require.NoError(t, err)
	_, err = svc.Patch(context.Background(), d.ID, nil, &f, 2)
	require.NoError(t, err, "retiring already-retired dept must be idempotent")
}

// Scenario CA2-H-06
func TestDepartmentService_Patch_ReactivateAlreadyActive_Idempotent(t *testing.T) {
	svc := newDeptSvc()
	d := mustCreateDept(t, svc, "idem-active-u06", "Idempotent Active", false)
	tr := true

	_, err := svc.Patch(context.Background(), d.ID, nil, &tr, 1)
	require.NoError(t, err, "reactivating already-active dept must be idempotent")
}

// ── CA2-V-03/04: nil name + nil isActive → no_mutable_field ──────────

// Scenarios CA2-V-03, CA2-V-04
func TestDepartmentService_Patch_BothFieldsNil_NoMutableField(t *testing.T) {
	svc := newDeptSvc()
	d := mustCreateDept(t, svc, "nil-fields-u07", "Nil Fields", false)

	_, err := svc.Patch(context.Background(), d.ID, nil, nil, 1)
	require.Error(t, err)
	assert.Equal(t, domain.ErrNoMutableField.Error(), errCode(t, err))
}

// ── CA-BL-01: missing record_version decoded as 0 → OCC conflict ──────

// Scenario CA-BL-01
func TestDepartmentService_Patch_VersionZero_AlwaysConflicts(t *testing.T) {
	svc := newDeptSvc()
	d := mustCreateDept(t, svc, "vz-u08", "Version Zero", false)
	name := "Updated"

	_, err := svc.Patch(context.Background(), d.ID, &name, nil, 0)
	require.Error(t, err)
	assert.Equal(t, domain.ErrOptimisticLockConflict.Error(), errCode(t, err))
}

// ── CA-DI-03: data-change patch increments record_version ─────────────

// Scenario CA-DI-03
// Note: the DB trigger fires WHEN (OLD.* IS DISTINCT FROM NEW.*).
// The fake repo always increments the version on any update call,
// matching service-level behaviour (the service always issues the UPDATE).
func TestDepartmentService_Patch_DataChange_VersionIncrements(t *testing.T) {
	svc := newDeptSvc()
	d := mustCreateDept(t, svc, "chng-u09", "Original Name", false)

	newName := "Updated Name" // explicitly different
	updated, err := svc.Patch(context.Background(), d.ID, &newName, nil, 1)
	require.NoError(t, err)
	assert.EqualValues(t, 2, updated.RecordVersion,
		"record_version must increment when data changes")
}

// ── CA6-BL-01: cache unavailable → fail-open, still returns data ──────

// Scenario CA6-BL-01
func TestDepartmentService_List_CacheUnavailable_FailsOpen(t *testing.T) {
	repo := newFakeDepartmentRepo()
	ctx := context.Background()
	_, _ = repo.Insert(ctx, &domain.Department{Code: "failopen-u10", Name: "FailOpen"})

	svc := service.NewDepartmentService(repo, &alwaysErrCache{})

	depts, err := svc.List(ctx, false)
	require.NoError(t, err, "cache error must be swallowed — fail-open (CAT-FAIL-1)")
	assert.NotEmpty(t, depts)
}

// ── CA-BL-03: record_version always ≥ 1 ──────────────────────────────

// Scenario CA-BL-03
func TestDepartmentService_Create_RecordVersionAlwaysGT0(t *testing.T) {
	svc := newDeptSvc()
	d := mustCreateDept(t, svc, "rvgt0-u11", "Always GT0", false)
	assert.GreaterOrEqual(t, d.RecordVersion, int64(1))
}

// ── CA-NOEVT-01 (service level): Create returns entity, no event ──────

// Scenario CA-NOEVT-01
func TestDepartmentService_Create_ReturnsEntityWithNoEventField(t *testing.T) {
	svc := newDeptSvc()
	d, err := svc.Create(context.Background(), "noevt-u12", "No Event", false)
	require.NoError(t, err)
	assert.NotEmpty(t, d.ID, "must return a valid department entity")
	// domain.Department has no event/outbox field — pure data model
	assert.NotEqual(t, uuid.Nil, d.ID)
}

// ── alwaysErrCache ────────────────────────────────────────────────────

var errCacheDown = errors.New("cache unavailable")

// alwaysErrCache simulates Valkey being completely unavailable.
type alwaysErrCache struct{}

func (*alwaysErrCache) Get(_ context.Context, _ string) ([]byte, error) {
	return nil, errCacheDown
}
func (*alwaysErrCache) Set(_ context.Context, _ string, _ []byte, _ time.Duration) error {
	return errCacheDown
}
func (*alwaysErrCache) Delete(_ context.Context, _ ...string) error { return errCacheDown }
func (*alwaysErrCache) Health(_ context.Context) error              { return errCacheDown }
func (*alwaysErrCache) Close() error                                { return nil }

var _ port.Cache = (*alwaysErrCache)(nil)
