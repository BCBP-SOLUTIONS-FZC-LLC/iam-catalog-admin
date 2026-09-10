package postgres

import (
	"context"
	"errors"
	"fmt"

	catmetrics "github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/adapter/outbound/metrics"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/core/port"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-pgcommon/pkg/pgcommon"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type DepartmentRepository struct {
	pool *pgcommon.Pool
}

var _ port.DepartmentRepository = (*DepartmentRepository)(nil)

func NewDepartmentRepository(pool *pgcommon.Pool) *DepartmentRepository {
	return &DepartmentRepository{pool: pool}
}

const departmentSelectColumns = `id, code, name, is_system, is_active, record_version, created_at, updated_at`

func scanDepartment(row pgx.Row) (*domain.Department, error) {
	var d domain.Department
	if err := row.Scan(&d.ID, &d.Code, &d.Name, &d.IsSystem, &d.IsActive, &d.RecordVersion, &d.CreatedAt, &d.UpdatedAt); err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *DepartmentRepository) List(ctx context.Context, activeOnly bool) ([]domain.Department, error) {
	var out []domain.Department
	err := withPool(ctx, r.pool, func(tx pgx.Tx) error {
		var e error
		out, e = deptListFromTx(ctx, tx, activeOnly)
		return e
	})
	return out, err
}

func deptListFromTx(ctx context.Context, tx pgx.Tx, activeOnly bool) ([]domain.Department, error) {
	sql := `SELECT ` + departmentSelectColumns + ` FROM departments`
	if activeOnly {
		sql += ` WHERE is_active = true`
	}
	sql += ` ORDER BY code`
	rows, err := tx.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Department
	for rows.Next() {
		d, err := scanDepartment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

func (r *DepartmentRepository) FindByID(ctx context.Context, id uuid.UUID) (*domain.Department, error) {
	var out *domain.Department
	err := withPool(ctx, r.pool, func(tx pgx.Tx) error {
		var e error
		out, e = deptFindByIDFromTx(ctx, tx, id)
		return e
	})
	return out, err
}

func deptFindByIDFromTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*domain.Department, error) {
	row := tx.QueryRow(ctx, `SELECT `+departmentSelectColumns+` FROM departments WHERE id = $1`, id)
	d, err := scanDepartment(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.NewError(domain.ErrDepartmentNotFound, "department not found")
		}
		return nil, err
	}
	return d, nil
}

func (r *DepartmentRepository) Insert(ctx context.Context, d *domain.Department) (*domain.Department, error) {
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	var out *domain.Department
	err := withPool(ctx, r.pool, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `
			INSERT INTO departments (id, code, name, is_system, is_active)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING `+departmentSelectColumns,
			d.ID, d.Code, d.Name, d.IsSystem, d.IsActive)
		created, scanErr := scanDepartment(row)
		if scanErr != nil {
			if pgcommon.IsUniqueViolation(scanErr) {
				return domain.NewError(domain.ErrConflict, "department code already exists").
					WithDetails(map[string]any{"code": "duplicate_code"})
			}
			return scanErr
		}
		out = created
		catmetrics.WritesTotal.WithLabelValues("departments", "insert").Inc()
		return nil
	})
	return out, err
}

func (r *DepartmentRepository) Update(ctx context.Context, id uuid.UUID, name *string, isActive *bool, expectedVersion int64) (*domain.Department, error) {
	if name == nil && isActive == nil {
		return r.FindByID(ctx, id)
	}
	args := []any{id, expectedVersion}
	set := ""
	if name != nil {
		args = append(args, *name)
		set = fmt.Sprintf(`name = $%d`, len(args))
	}
	if isActive != nil {
		args = append(args, *isActive)
		if set != "" {
			set += ", "
		}
		set += fmt.Sprintf(`is_active = $%d`, len(args))
	}
	sql := `UPDATE departments SET ` + set +
		` WHERE id = $1 AND record_version = $2 RETURNING ` + departmentSelectColumns

	var out *domain.Department
	err := withPool(ctx, r.pool, func(tx pgx.Tx) error {
		updated, err := deptUpdateFromTx(ctx, tx, id, sql, args)
		if err != nil {
			if errors.Is(err, domain.ErrOptimisticLockConflict) {
				catmetrics.OptimisticLockConflicts.WithLabelValues("departments").Inc()
			}
			return err
		}
		out = updated
		catmetrics.WritesTotal.WithLabelValues("departments", "update").Inc()
		return nil
	})
	return out, err
}

// deptUpdateFromTx is Update's own transaction body, factored out so the
// white-box test suite exercises the exact code path Update runs (previously
// this logic was duplicated inline in Update while this function sat unused
// — the tested path and the shipped path had silently diverged).
func deptUpdateFromTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, sql string, args []any) (*domain.Department, error) {
	row := tx.QueryRow(ctx, sql, args...)
	updated, err := scanDepartment(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			var currentVersion int64
			probe := tx.QueryRow(ctx, `SELECT record_version FROM departments WHERE id = $1`, id)
			if perr := probe.Scan(&currentVersion); perr != nil {
				if errors.Is(perr, pgx.ErrNoRows) {
					return nil, domain.NewError(domain.ErrDepartmentNotFound, "department not found")
				}
				return nil, perr
			}
			return nil, domain.NewError(domain.ErrOptimisticLockConflict, "record version conflict").WithDetails(map[string]any{
				"record_version": currentVersion,
			})
		}
		return nil, err
	}
	return updated, nil
}
