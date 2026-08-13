package port

import (
	"context"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/core/domain"
)

// PlanRepository owns the global plans catalog (LLD §5.2). Reads are
// unauthenticated at the DB layer; writes are gated to platform_operator
// via CAT-5, PATCH-only (PLAN-4 — no create/delete, the tier set is fixed
// to the tenant_plan ENUM).
type PlanRepository interface {
	List(ctx context.Context) ([]domain.Plan, error)
	FindByCode(ctx context.Context, code domain.TenantPlan) (*domain.Plan, error)
	Update(ctx context.Context, code domain.TenantPlan, patch *domain.PlanPatch) (*domain.Plan, error)
}
