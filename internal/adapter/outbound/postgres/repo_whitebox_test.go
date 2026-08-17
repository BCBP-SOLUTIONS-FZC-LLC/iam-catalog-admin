package postgres

import (
	"context"
	"errors"
	"testing"

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
