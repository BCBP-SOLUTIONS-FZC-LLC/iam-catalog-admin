package port

import (
	"context"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/core/domain"
	"github.com/google/uuid"
)

// DepartmentRepository owns the global departments catalog (LLD §5.1). No
// RLS — this table has no tenant_id column at all. Writes are
// operator-only, gated at the handler layer (CAT-1/CAT-2).
type DepartmentRepository interface {
	List(ctx context.Context, activeOnly bool) ([]domain.Department, error)
	FindByID(ctx context.Context, id uuid.UUID) (*domain.Department, error)
	FindByCode(ctx context.Context, code string) (*domain.Department, error)
	Insert(ctx context.Context, d *domain.Department) (*domain.Department, error)
	Update(ctx context.Context, id uuid.UUID, name *string, isActive *bool, expectedVersion int64) (*domain.Department, error)
}
