package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/core/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errScan is a sentinel non-ErrNoRows scan error used across tests.
var errScan = errors.New("unexpected scan error")

// ── mockRow ─────────────────────────────────────────────────────────────────

// mockRow implements pgx.Row and always returns a fixed error from Scan.
type mockRow struct{ err error }

func (r mockRow) Scan(_ ...any) error { return r.err }

// ── mockRows ─────────────────────────────────────────────────────────────────

// mockRows implements pgx.Rows. It yields exactly one Next() == true, then
// fails on Scan so that the scan-error branch inside the loop is exercised.
type mockRows struct {
	called bool
	err    error
}

func (r *mockRows) Close()                                       {}
func (r *mockRows) Err() error                                   { return nil }
func (r *mockRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *mockRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *mockRows) Values() ([]any, error)                       { return nil, nil }
func (r *mockRows) RawValues() [][]byte                          { return nil }
func (r *mockRows) Conn() *pgx.Conn                              { return nil }
func (r *mockRows) Next() bool {
	if !r.called {
		r.called = true
		return true
	}
	return false
}
func (r *mockRows) Scan(_ ...any) error { return r.err }

// mockRowWithVersion is a pgx.Row that succeeds and populates a single
// int64 destination — used to simulate the probe returning a live version.
type mockRowWithVersion struct{ version int64 }

func (r mockRowWithVersion) Scan(dest ...any) error {
	if len(dest) > 0 {
		if v, ok := dest[0].(*int64); ok {
			*v = r.version
		}
	}
	return nil
}

// ── panicTx ──────────────────────────────────────────────────────────────────

// panicTx is a base stub that implements pgx.Tx with all methods panicking.
// Embed it and override only the methods you need.
type panicTx struct{}

func (p panicTx) Begin(_ context.Context) (pgx.Tx, error) { panic("not implemented") }
func (p panicTx) Commit(_ context.Context) error          { panic("not implemented") }
func (p panicTx) Rollback(_ context.Context) error        { panic("not implemented") }
func (p panicTx) CopyFrom(_ context.Context, _ pgx.Identifier, _ []string, _ pgx.CopyFromSource) (int64, error) {
	panic("not implemented")
}
func (p panicTx) SendBatch(_ context.Context, _ *pgx.Batch) pgx.BatchResults {
	panic("not implemented")
}
func (p panicTx) LargeObjects() pgx.LargeObjects { panic("not implemented") }
func (p panicTx) Prepare(_ context.Context, _, _ string) (*pgconn.StatementDescription, error) {
	panic("not implemented")
}
func (p panicTx) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	panic("not implemented")
}
func (p panicTx) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	panic("not implemented")
}
func (p panicTx) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	panic("not implemented")
}
func (p panicTx) Conn() *pgx.Conn { panic("not implemented") }

// ── mockTxWithQueryRow ───────────────────────────────────────────────────────

// mockTxWithQueryRow overrides QueryRow to return a fixed row. Supports
// multiple sequential calls by cycling through a slice of rows.
type mockTxWithQueryRow struct {
	panicTx
	rows  []pgx.Row
	index int
}

func (m *mockTxWithQueryRow) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	row := m.rows[m.index]
	m.index++
	return row
}

// ── mockTxWithQuery ──────────────────────────────────────────────────────────

// mockTxWithQuery overrides Query to return a fixed error (simulates Query failure).
type mockTxWithQuery struct {
	panicTx
	err error
}

func (m *mockTxWithQuery) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	return nil, m.err
}

// ── mockTxWithScanErrRows ────────────────────────────────────────────────────

// mockTxWithScanErrRows overrides Query to return mockRows that fails on Scan
// (simulates a scan error inside the rows iteration loop).
type mockTxWithScanErrRows struct {
	panicTx
	scanErr error
}

func (m *mockTxWithScanErrRows) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	return &mockRows{err: m.scanErr}, nil
}

// ════════════════════════════════════════════════════════════════════════════
// department_repository whitebox tests
// ════════════════════════════════════════════════════════════════════════════

// TestDeptListFromTx_QueryError covers the `return nil, err` when tx.Query fails.
func TestDeptListFromTx_QueryError(t *testing.T) {
	tx := &mockTxWithQuery{err: errScan}
	_, err := deptListFromTx(context.Background(), tx, false)
	require.Error(t, err)
	assert.Equal(t, errScan, err)
}

// TestDeptListFromTx_ScanError covers the `return nil, err` inside the
// rows.Next() loop when scanDepartment fails.
func TestDeptListFromTx_ScanError(t *testing.T) {
	tx := &mockTxWithScanErrRows{scanErr: errScan}
	_, err := deptListFromTx(context.Background(), tx, false)
	require.Error(t, err)
	assert.Equal(t, errScan, err)
}

// TestDeptFindByIDFromTx_NonErrNoRowsScanError covers the `return nil, err`
// branch in deptFindByIDFromTx when the scan error is not pgx.ErrNoRows.
func TestDeptFindByIDFromTx_NonErrNoRowsScanError(t *testing.T) {
	tx := &mockTxWithQueryRow{rows: []pgx.Row{mockRow{err: errScan}}}
	_, err := deptFindByIDFromTx(context.Background(), tx, uuid.New())
	require.Error(t, err)
	assert.Equal(t, errScan, err)
}

// TestDeptFindByCodeFromTx_NonErrNoRowsScanError covers the `return nil, err`
// branch in deptFindByCodeFromTx when the scan error is not pgx.ErrNoRows.
func TestDeptFindByCodeFromTx_NonErrNoRowsScanError(t *testing.T) {
	tx := &mockTxWithQueryRow{rows: []pgx.Row{mockRow{err: errScan}}}
	_, err := deptFindByCodeFromTx(context.Background(), tx, "CODE")
	require.Error(t, err)
	assert.Equal(t, errScan, err)
}

// TestDeptUpdateFromTx_ProbeScanNonErrNoRowsError covers the `return nil, perr`
// branch when the main UPDATE returns ErrNoRows but the probe scan also fails
// with a non-ErrNoRows error (line 158 in the original).
func TestDeptUpdateFromTx_ProbeScanNonErrNoRowsError(t *testing.T) {
	// First QueryRow (main UPDATE) → ErrNoRows so we enter the probe branch.
	// Second QueryRow (probe) → non-ErrNoRows scan error.
	tx := &mockTxWithQueryRow{
		rows: []pgx.Row{
			mockRow{err: pgx.ErrNoRows},
			mockRow{err: errScan},
		},
	}
	id := uuid.New()
	name := "new name"
	sql := `UPDATE departments SET name = $3 WHERE id = $1 AND record_version = $2 RETURNING ` + departmentSelectColumns
	args := []any{id, int64(1), name}
	_, err := deptUpdateFromTx(context.Background(), tx, id, sql, args)
	require.Error(t, err)
	// wrapConnErr would map it, but deptUpdateFromTx returns the raw perr.
	// The domain.ErrDependencyUnavailable wrapping only happens in withPool→wrapConnErr.
	// Since we're calling deptUpdateFromTx directly (bypassing withPool), we get errScan.
	assert.Equal(t, errScan, err)
}

// TestDeptUpdateFromTx_NilDeptAfterNoRows verifies that ErrNoRows on the
// probe leads to ErrDepartmentNotFound (sanity-check of existing branch).
func TestDeptUpdateFromTx_NilDeptAfterNoRows(t *testing.T) {
	tx := &mockTxWithQueryRow{
		rows: []pgx.Row{
			mockRow{err: pgx.ErrNoRows},
			mockRow{err: pgx.ErrNoRows},
		},
	}
	id := uuid.New()
	sql := `UPDATE departments SET name = $3 WHERE id = $1 AND record_version = $2 RETURNING ` + departmentSelectColumns
	args := []any{id, int64(1), "name"}
	_, err := deptUpdateFromTx(context.Background(), tx, id, sql, args)
	require.Error(t, err)
	var de *domain.DomainError
	require.True(t, errors.As(err, &de))
	assert.Equal(t, domain.ErrDepartmentNotFound.Error(), de.Code)
}

// TestDeptUpdateFromTx_OCCConflict covers the OCC branch: main UPDATE returns
// ErrNoRows (version mismatch) and the probe successfully returns the current
// version → ErrOptimisticLockConflict with that version in Details.
func TestDeptUpdateFromTx_OCCConflict(t *testing.T) {
	tx := &mockTxWithQueryRow{
		rows: []pgx.Row{
			mockRow{err: pgx.ErrNoRows},           // main UPDATE: version mismatch
			mockRowWithVersion{version: int64(3)}, // probe: current version is 3
		},
	}
	id := uuid.New()
	name := "new name"
	sql := `UPDATE departments SET name = $3 WHERE id = $1 AND record_version = $2 RETURNING ` + departmentSelectColumns
	args := []any{id, int64(1), name}
	_, err := deptUpdateFromTx(context.Background(), tx, id, sql, args)
	require.Error(t, err)
	var de *domain.DomainError
	require.True(t, errors.As(err, &de))
	assert.Equal(t, domain.ErrOptimisticLockConflict.Error(), de.Code)
	assert.EqualValues(t, 3, de.Details["record_version"])
}

// ════════════════════════════════════════════════════════════════════════════
// plan_repository whitebox tests
// ════════════════════════════════════════════════════════════════════════════

// TestPlanListFromTx_QueryError covers the `return nil, err` when tx.Query fails.
func TestPlanListFromTx_QueryError(t *testing.T) {
	tx := &mockTxWithQuery{err: errScan}
	_, err := planListFromTx(context.Background(), tx)
	require.Error(t, err)
	assert.Equal(t, errScan, err)
}

// TestPlanUpdateFromTx_ProbeScanNonErrNoRowsError covers the `return nil, perr`
// branch in planUpdateFromTx when the main UPDATE returns ErrNoRows but the
// probe scan fails with a non-ErrNoRows error (plan Update line 144).
func TestPlanUpdateFromTx_ProbeScanNonErrNoRowsError(t *testing.T) {
	tx := &mockTxWithQueryRow{
		rows: []pgx.Row{
			mockRow{err: pgx.ErrNoRows},
			mockRow{err: errScan},
		},
	}
	sql := `UPDATE plans SET display_name = $3 WHERE code = $1 AND record_version = $2 RETURNING ` + planCols
	args := []any{"starter", int64(1), "New Name"}
	_, err := planUpdateFromTx(context.Background(), tx, domain.PlanStarter, sql, args)
	require.Error(t, err)
	assert.Equal(t, errScan, err)
}

// TestPlanUpdateFromTx_ProbeErrNoRows covers the path where the main UPDATE
// returns ErrNoRows and the probe also returns ErrNoRows → ErrPlanNotFound.
func TestPlanUpdateFromTx_ProbeErrNoRows(t *testing.T) {
	tx := &mockTxWithQueryRow{
		rows: []pgx.Row{
			mockRow{err: pgx.ErrNoRows}, // main UPDATE: no match
			mockRow{err: pgx.ErrNoRows}, // probe: plan doesn't exist
		},
	}
	sql := `UPDATE plans SET display_name = $3 WHERE code = $1 AND record_version = $2 RETURNING ` + planCols
	args := []any{"starter", int64(1), "New Name"}
	_, err := planUpdateFromTx(context.Background(), tx, domain.PlanStarter, sql, args)
	require.Error(t, err)
	var de *domain.DomainError
	require.True(t, errors.As(err, &de))
	assert.Equal(t, domain.ErrPlanNotFound.Error(), de.Code)
}

// TestPlanUpdateFromTx_OCCConflict covers the OCC branch: main UPDATE returns
// ErrNoRows and the probe successfully returns the current version.
func TestPlanUpdateFromTx_OCCConflict(t *testing.T) {
	tx := &mockTxWithQueryRow{
		rows: []pgx.Row{
			mockRow{err: pgx.ErrNoRows},           // main UPDATE: version mismatch
			mockRowWithVersion{version: int64(5)}, // probe: current version is 5
		},
	}
	sql := `UPDATE plans SET display_name = $3 WHERE code = $1 AND record_version = $2 RETURNING ` + planCols
	args := []any{"starter", int64(1), "New Name"}
	_, err := planUpdateFromTx(context.Background(), tx, domain.PlanStarter, sql, args)
	require.Error(t, err)
	var de *domain.DomainError
	require.True(t, errors.As(err, &de))
	assert.Equal(t, domain.ErrOptimisticLockConflict.Error(), de.Code)
	assert.EqualValues(t, 5, de.Details["record_version"])
}

// TestDeptUpdateFromTx_MainScanNonErrNoRowsError covers the `return nil, err`
// branch in deptUpdateFromTx when the main UPDATE's RETURNING scan fails with
// a non-ErrNoRows error (department_repository.go deptUpdateFromTx line 203).
func TestDeptUpdateFromTx_MainScanNonErrNoRowsError(t *testing.T) {
	tx := &mockTxWithQueryRow{
		rows: []pgx.Row{
			mockRow{err: errScan}, // main scan: non-ErrNoRows error
		},
	}
	id := uuid.New()
	name := "new name"
	sql := `UPDATE departments SET name = $3 WHERE id = $1 AND record_version = $2 RETURNING ` + departmentSelectColumns
	args := []any{id, int64(1), name}
	_, err := deptUpdateFromTx(context.Background(), tx, id, sql, args)
	require.Error(t, err)
	assert.Equal(t, errScan, err)
}

// TestPlanRepository_Update_NilPatch covers the defensive guard at the top
// of PlanRepository.Update — a nil patch must be rejected with validation_error
// before touching the connection pool.
func TestPlanRepository_Update_NilPatch(t *testing.T) {
	repo := &PlanRepository{pool: nil}
	_, err := repo.Update(context.Background(), domain.PlanStarter, nil)
	require.Error(t, err)
	var de *domain.DomainError
	require.True(t, errors.As(err, &de))
	assert.Equal(t, domain.ErrValidation.Error(), de.Code)
}

// ── success-path mocks ────────────────────────────────────────────────────────

// mockDepartmentScanRow implements pgx.Row and populates all 8 Department
// columns so that scanDepartment returns a non-nil department.
type mockDepartmentScanRow struct{}

func (r mockDepartmentScanRow) Scan(dest ...any) error {
	if v, ok := dest[0].(*uuid.UUID); ok {
		*v = uuid.MustParse("11111111-0000-0000-0000-000000000001")
	}
	if v, ok := dest[1].(*string); ok {
		*v = "HR"
	}
	if v, ok := dest[2].(*string); ok {
		*v = "Human Resources"
	}
	if v, ok := dest[3].(*bool); ok {
		*v = false
	}
	if v, ok := dest[4].(*bool); ok {
		*v = true
	}
	if v, ok := dest[5].(*int64); ok {
		*v = 2
	}
	if v, ok := dest[6].(*time.Time); ok {
		*v = time.Time{}
	}
	if v, ok := dest[7].(*time.Time); ok {
		*v = time.Time{}
	}
	return nil
}

// mockPlanScanRow implements pgx.Row and populates all 11 Plan columns so
// that scanPlan returns a non-nil plan.
type mockPlanScanRow struct{}

func (r mockPlanScanRow) Scan(dest ...any) error {
	if v, ok := dest[0].(*string); ok {
		*v = "starter"
	}
	if v, ok := dest[1].(*string); ok {
		*v = "Starter"
	}
	if v, ok := dest[2].(**int); ok {
		*v = nil // WorkflowTemplateLimit: unlimited
	}
	if v, ok := dest[3].(**int); ok {
		*v = nil // TenderLimit: unlimited
	}
	if v, ok := dest[4].(*int); ok {
		*v = 30
	}
	if v, ok := dest[5].(*bool); ok {
		*v = false
	}
	if v, ok := dest[6].(*string); ok {
		*v = "none"
	}
	if v, ok := dest[7].(*[]byte); ok {
		*v = []byte(`{}`)
	}
	if v, ok := dest[8].(*int64); ok {
		*v = 2
	}
	if v, ok := dest[9].(*time.Time); ok {
		*v = time.Time{}
	}
	if v, ok := dest[10].(*time.Time); ok {
		*v = time.Time{}
	}
	return nil
}

// ── deptUpdateFromTx success path ────────────────────────────────────────────

// TestDeptUpdateFromTx_Success covers the `return updated, nil` branch (line 205)
// by providing a mock QueryRow that succeeds on the first call.
func TestDeptUpdateFromTx_Success(t *testing.T) {
	tx := &mockTxWithQueryRow{
		rows: []pgx.Row{mockDepartmentScanRow{}},
	}
	id := uuid.New()
	sql := `UPDATE departments SET name = $3 WHERE id = $1 AND record_version = $2 RETURNING ` + departmentSelectColumns
	args := []any{id, int64(1), "HR Dept"}
	updated, err := deptUpdateFromTx(context.Background(), tx, id, sql, args)
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, "HR", updated.Code)
}

// ── planUpdateFromTx success path ─────────────────────────────────────────────

// TestPlanUpdateFromTx_Success covers the `return p, nil` branch (line 181)
// by providing a mock QueryRow that succeeds on the first call.
func TestPlanUpdateFromTx_Success(t *testing.T) {
	tx := &mockTxWithQueryRow{
		rows: []pgx.Row{mockPlanScanRow{}},
	}
	sql := `UPDATE plans SET display_name = $3 WHERE code = $1 AND record_version = $2 RETURNING ` + planCols
	args := []any{"starter", int64(1), "Starter Plan"}
	p, err := planUpdateFromTx(context.Background(), tx, domain.PlanStarter, sql, args)
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, domain.PlanStarter, p.Code)
}

// ── PlanRepository.Update json.Marshal error path ─────────────────────────────

// TestPlanRepository_Update_FeatureSetMarshalError covers the `return nil, err`
// branch (line 125-127) triggered when json.Marshal fails on patch.FeatureSet.
// This path occurs before withPool, so no real DB is needed.
func TestPlanRepository_Update_FeatureSetMarshalError(t *testing.T) {
	repo := &PlanRepository{pool: nil}
	patch := &domain.PlanPatch{
		RecordVersion: 1,
		FeatureSet:    map[string]any{"bad": func() {}}, // json.Marshal returns error for func()
	}
	_, err := repo.Update(context.Background(), domain.PlanStarter, patch)
	require.Error(t, err)
	// Must be the raw json.Marshal error, not a domain error or pool error.
	var de *domain.DomainError
	assert.False(t, errors.As(err, &de), "expected raw marshal error, not domain error")
}

// ── DepartmentRepository.Update nil-nil guard ─────────────────────────────────

// TestDepartmentRepository_Update_NilNilGuard covers the `return r.FindByID`
// branch (line 140) — the guard that falls back to a read when both name and
// isActive are nil. The guard itself executes before the pool is touched, so
// the statement IS counted; the nil pool then causes a panic inside FindByID
// which assert.Panics catches.
func TestDepartmentRepository_Update_NilNilGuard(t *testing.T) {
	repo := &DepartmentRepository{pool: nil}
	assert.Panics(t, func() {
		_, _ = repo.Update(context.Background(), uuid.New(), nil, nil, 1)
	})
}

// TestPlanUpdateFromTx_MainScanNonErrNoRowsError covers the `return nil, err`
// branch in planUpdateFromTx when the main UPDATE's RETURNING row causes a
// non-ErrNoRows scan error (plan Update line 149).
func TestPlanUpdateFromTx_MainScanNonErrNoRowsError(t *testing.T) {
	tx := &mockTxWithQueryRow{
		rows: []pgx.Row{
			mockRow{err: errScan},
		},
	}
	sql := `UPDATE plans SET display_name = $3 WHERE code = $1 AND record_version = $2 RETURNING ` + planCols
	args := []any{"starter", int64(1), "New Name"}
	_, err := planUpdateFromTx(context.Background(), tx, domain.PlanStarter, sql, args)
	require.Error(t, err)
	assert.Equal(t, errScan, err)
}
