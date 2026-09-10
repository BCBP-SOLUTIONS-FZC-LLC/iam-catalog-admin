// Unit tests for internal/core/service/department_service.go.
// Black-box, package unit_test — mirrors iam-org-membership's
// test/unit/*_test.go convention: hand-rolled fakes implementing the port
// interfaces directly, no mocking library, no Postgres/Valkey required.
package unit_test

import (
	"context"
	"testing"
	"time"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/core/port"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/core/service"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── fakeDepartmentRepo ────────────────────────────────────────────────

// fakeDepartmentRepo is an in-memory port.DepartmentRepository — no
// Postgres required.
type fakeDepartmentRepo struct {
	rows      map[uuid.UUID]domain.Department
	updateErr error
}

func newFakeDepartmentRepo() *fakeDepartmentRepo {
	return &fakeDepartmentRepo{rows: map[uuid.UUID]domain.Department{}}
}

func (f *fakeDepartmentRepo) List(_ context.Context, activeOnly bool) ([]domain.Department, error) {
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
	if f.updateErr != nil {
		return nil, f.updateErr
	}
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

var _ port.DepartmentRepository = (*fakeDepartmentRepo)(nil)

// ── fakeCache ────────────────────────────────────────────────────────
// Shared by every test/unit file in this package (also used by
// plan_service_test.go).

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

var _ port.Cache = (*fakeCache)(nil)

// ── DepartmentService ───────────────────────────────────────────────

func TestDepartmentService_CreateAndGet(t *testing.T) {
	repo := newFakeDepartmentRepo()
	svc := service.NewDepartmentService(repo, newFakeCache())

	d, err := svc.Create(context.Background(), "ENGINEERING", "Engineering", true)
	require.NoError(t, err)
	assert.Equal(t, "ENGINEERING", d.Code)
	assert.True(t, d.IsActive)

	got, err := svc.Get(context.Background(), d.ID)
	require.NoError(t, err)
	assert.Equal(t, d.Code, got.Code)
}

func TestDepartmentService_Create_EmptyFieldsRejected(t *testing.T) {
	svc := service.NewDepartmentService(newFakeDepartmentRepo(), newFakeCache())
	_, err := svc.Create(context.Background(), "", "Engineering", false)
	require.Error(t, err)
	var de *domain.DomainError
	require.ErrorAs(t, err, &de)
	assert.Equal(t, domain.ErrValidation.Error(), de.Code)
}

func TestDepartmentService_Create_DuplicateCode(t *testing.T) {
	svc := service.NewDepartmentService(newFakeDepartmentRepo(), newFakeCache())
	_, err := svc.Create(context.Background(), "LEGAL", "Legal", false)
	require.NoError(t, err)
	_, err = svc.Create(context.Background(), "LEGAL", "Legal Team", false)
	require.Error(t, err)
	var de *domain.DomainError
	require.ErrorAs(t, err, &de)
	assert.Equal(t, domain.ErrConflict.Error(), de.Code)
}

func TestDepartmentService_Patch_NoMutableField(t *testing.T) {
	svc := service.NewDepartmentService(newFakeDepartmentRepo(), newFakeCache())
	_, err := svc.Patch(context.Background(), uuid.New(), nil, nil, 1)
	require.Error(t, err)
	var de *domain.DomainError
	require.ErrorAs(t, err, &de)
	assert.Equal(t, domain.ErrNoMutableField.Error(), de.Code)
}

func TestDepartmentService_Patch_SystemDepartmentCannotBeRetired(t *testing.T) {
	repo := newFakeDepartmentRepo()
	svc := service.NewDepartmentService(repo, newFakeCache())
	d, err := svc.Create(context.Background(), "ENGINEERING", "Engineering", true)
	require.NoError(t, err)

	inactive := false
	_, err = svc.Patch(context.Background(), d.ID, nil, &inactive, d.RecordVersion)
	require.Error(t, err)
	var de *domain.DomainError
	require.ErrorAs(t, err, &de)
	assert.Equal(t, domain.ErrSystemDepartmentCannotBeRetired.Error(), de.Code)
}

func TestDepartmentService_Patch_EmptyNameRejected(t *testing.T) {
	svc := service.NewDepartmentService(newFakeDepartmentRepo(), newFakeCache())
	empty := ""
	_, err := svc.Patch(context.Background(), uuid.New(), &empty, nil, 1)
	require.Error(t, err)
	var de *domain.DomainError
	require.ErrorAs(t, err, &de)
	assert.Equal(t, domain.ErrValidation.Error(), de.Code)
}

// TestDepartmentService_Patch_NameImmutableCheckViolation simulates the
// real shape the prevent_system_department_name_change trigger produces:
// a *pgconn.PgError with SQLSTATE 23514 (check_violation) and ConstraintName
// "chk_system_department_name_immutable" — matched via
// pgcommon.IsCheckViolation/ConstraintName, not a substring search over
// the error message.
func TestDepartmentService_Patch_NameImmutableCheckViolation(t *testing.T) {
	repo := newFakeDepartmentRepo()
	repo.updateErr = &pgconn.PgError{
		Code:           "23514",
		ConstraintName: "chk_system_department_name_immutable",
		Message:        "system department name is immutable",
	}
	svc := service.NewDepartmentService(repo, newFakeCache())
	newName := "Renamed"
	_, err := svc.Patch(context.Background(), uuid.New(), &newName, nil, 1)
	require.Error(t, err)
	var de *domain.DomainError
	require.ErrorAs(t, err, &de)
	assert.Equal(t, domain.ErrSystemNameImmutable.Error(), de.Code)
	assert.Equal(t, "name", de.Details["field"])
}

func TestDepartmentService_Patch_OptimisticLockConflict(t *testing.T) {
	repo := newFakeDepartmentRepo()
	svc := service.NewDepartmentService(repo, newFakeCache())
	d, err := svc.Create(context.Background(), "FINANCE", "Finance", false)
	require.NoError(t, err)

	newName := "Finance & Accounting"
	_, err = svc.Patch(context.Background(), d.ID, &newName, nil, d.RecordVersion+1)
	require.Error(t, err)
	var de *domain.DomainError
	require.ErrorAs(t, err, &de)
	assert.Equal(t, domain.ErrOptimisticLockConflict.Error(), de.Code)
}

func TestDepartmentService_DeleteBlocked(t *testing.T) {
	svc := service.NewDepartmentService(newFakeDepartmentRepo(), newFakeCache())
	err := svc.DeleteBlocked()
	require.Error(t, err)
	var de *domain.DomainError
	require.ErrorAs(t, err, &de)
	assert.Equal(t, domain.ErrMethodNotAllowed.Error(), de.Code)
}

func TestDepartmentService_List_CachesFullCatalog(t *testing.T) {
	repo := newFakeDepartmentRepo()
	cache := newFakeCache()
	svc := service.NewDepartmentService(repo, cache)

	_, err := svc.Create(context.Background(), "LEGAL", "Legal", false)
	require.NoError(t, err)

	all, err := svc.List(context.Background(), false)
	require.NoError(t, err)
	assert.Len(t, all, 1)
	assert.NotEmpty(t, cache.values["cat:departments"], "List should populate the cat:departments cache key")

	// A create after the cache is warm must invalidate it so the next List
	// sees the new row rather than a stale cached snapshot.
	_, err = svc.Create(context.Background(), "DESIGN", "Design", false)
	require.NoError(t, err)
	assert.Empty(t, cache.values["cat:departments"], "Create must invalidate the cache")

	all, err = svc.List(context.Background(), false)
	require.NoError(t, err)
	assert.Len(t, all, 2)
}

func TestDepartmentService_List_ActiveOnlyFilter(t *testing.T) {
	repo := newFakeDepartmentRepo()
	svc := service.NewDepartmentService(repo, newFakeCache())

	d, err := svc.Create(context.Background(), "LEGAL", "Legal", false)
	require.NoError(t, err)
	inactive := false
	_, err = svc.Patch(context.Background(), d.ID, nil, &inactive, d.RecordVersion)
	require.NoError(t, err)

	all, err := svc.List(context.Background(), false)
	require.NoError(t, err)
	assert.Len(t, all, 1)

	activeOnly, err := svc.List(context.Background(), true)
	require.NoError(t, err)
	assert.Empty(t, activeOnly)
}
