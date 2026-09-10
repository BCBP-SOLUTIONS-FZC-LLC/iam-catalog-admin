package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	catmetrics "github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/adapter/outbound/metrics"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/core/port"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-pgcommon/pkg/pgcommon"
	"github.com/jackc/pgx/v5"
)

type PlanRepository struct {
	pool *pgcommon.Pool
}

var _ port.PlanRepository = (*PlanRepository)(nil)

func NewPlanRepository(pool *pgcommon.Pool) *PlanRepository {
	return &PlanRepository{pool: pool}
}

const planCols = `code, display_name, workflow_template_limit, tender_limit, trial_duration_days, sso_enabled, custom_branding, feature_set, record_version, created_at, updated_at`

func scanPlan(row pgx.Row) (*domain.Plan, error) {
	var p domain.Plan
	var code, branding string
	var featureSetJSON []byte
	if err := row.Scan(&code, &p.DisplayName, &p.WorkflowTemplateLimit, &p.TenderLimit,
		&p.TrialDurationDays, &p.SSOEnabled, &branding, &featureSetJSON,
		&p.RecordVersion, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}
	p.Code = domain.TenantPlan(code)
	p.CustomBranding = domain.BrandingLevel(branding)
	if len(featureSetJSON) > 0 && string(featureSetJSON) != "null" {
		if err := json.Unmarshal(featureSetJSON, &p.FeatureSet); err != nil {
			return nil, err
		}
	}
	if p.FeatureSet == nil {
		p.FeatureSet = map[string]any{}
	}
	return &p, nil
}

func (r *PlanRepository) List(ctx context.Context) ([]domain.Plan, error) {
	var out []domain.Plan
	err := withPool(ctx, r.pool, func(tx pgx.Tx) error {
		var e error
		out, e = planListFromTx(ctx, tx)
		return e
	})
	return out, err
}

func planListFromTx(ctx context.Context, tx pgx.Tx) ([]domain.Plan, error) {
	rows, err := tx.Query(ctx, `SELECT `+planCols+` FROM plans ORDER BY code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Plan
	for rows.Next() {
		p, err := scanPlan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (r *PlanRepository) FindByCode(ctx context.Context, code domain.TenantPlan) (*domain.Plan, error) {
	var out *domain.Plan
	err := withPool(ctx, r.pool, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `SELECT `+planCols+` FROM plans WHERE code = $1`, string(code))
		p, err := scanPlan(row)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.NewError(domain.ErrPlanNotFound, "plan not found")
			}
			return err
		}
		out = p
		return nil
	})
	return out, err
}

func (r *PlanRepository) Update(ctx context.Context, code domain.TenantPlan, patch *domain.PlanPatch) (*domain.Plan, error) {
	if patch == nil {
		return nil, domain.NewError(domain.ErrValidation, "patch is required")
	}
	sets := []string{}
	args := []any{string(code), patch.RecordVersion}
	next := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if patch.DisplayName != nil {
		sets = append(sets, "display_name = "+next(*patch.DisplayName))
	}
	if patch.WorkflowTemplateLimit != nil {
		sets = append(sets, "workflow_template_limit = "+next(*patch.WorkflowTemplateLimit))
	}
	if patch.TenderLimit != nil {
		sets = append(sets, "tender_limit = "+next(*patch.TenderLimit))
	}
	if patch.TrialDurationDays != nil {
		sets = append(sets, "trial_duration_days = "+next(*patch.TrialDurationDays))
	}
	if patch.SSOEnabled != nil {
		sets = append(sets, "sso_enabled = "+next(*patch.SSOEnabled))
	}
	if patch.CustomBranding != nil {
		sets = append(sets, "custom_branding = "+next(string(*patch.CustomBranding)))
	}
	if patch.FeatureSet != nil {
		fsJSON, err := json.Marshal(patch.FeatureSet)
		if err != nil {
			return nil, err
		}
		sets = append(sets, "feature_set = "+next(string(fsJSON))+"::jsonb")
	}
	if len(sets) == 0 {
		return nil, domain.NewError(domain.ErrNoMutableField, "at least one field must be provided")
	}
	sql := `UPDATE plans SET ` + strings.Join(sets, ", ") +
		` WHERE code = $1 AND record_version = $2 RETURNING ` + planCols

	var out *domain.Plan
	err := withPool(ctx, r.pool, func(tx pgx.Tx) error {
		p, err := planUpdateFromTx(ctx, tx, code, sql, args)
		if err != nil {
			if errors.Is(err, domain.ErrOptimisticLockConflict) {
				catmetrics.OptimisticLockConflicts.WithLabelValues("plans").Inc()
			}
			return err
		}
		out = p
		catmetrics.WritesTotal.WithLabelValues("plans", "update").Inc()
		return nil
	})
	return out, err
}

// planUpdateFromTx is Update's own transaction body, factored out so the
// white-box test suite exercises the exact code path Update runs (previously
// this logic was duplicated inline in Update while this function sat unused
// — the tested path and the shipped path had silently diverged).
func planUpdateFromTx(ctx context.Context, tx pgx.Tx, code domain.TenantPlan, sql string, args []any) (*domain.Plan, error) {
	row := tx.QueryRow(ctx, sql, args...)
	p, err := scanPlan(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			var v int64
			probe := tx.QueryRow(ctx, `SELECT record_version FROM plans WHERE code = $1`, string(code))
			if perr := probe.Scan(&v); perr != nil {
				if errors.Is(perr, pgx.ErrNoRows) {
					return nil, domain.NewError(domain.ErrPlanNotFound, "plan not found")
				}
				return nil, perr
			}
			return nil, domain.NewError(domain.ErrOptimisticLockConflict, "record version conflict").
				WithDetails(map[string]any{"record_version": v})
		}
		return nil, err
	}
	return p, nil
}
