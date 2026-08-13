package service

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/core/port"
	"github.com/google/uuid"
)

// departmentsCacheTTL is LLD §8's cat:departments TTL — short because this
// service's own DB is the true source; the cache mainly shields read
// replicas from GET /api/v1/departments traffic, not a freshness
// guarantee for consumers (those get their own longer-TTL om:departments
// key, populated from CAT-I1, per §8).
const departmentsCacheTTL = 60 * time.Second

// DepartmentService implements CAT-1, CAT-2, CAT-3, CAT-6, CAT-7, and the
// listing half of CAT-I1. Every write method assumes the handler-layer
// platform_operator gate (LLD §9) has already run.
type DepartmentService struct {
	repo  port.DepartmentRepository
	cache port.Cache
}

func NewDepartmentService(repo port.DepartmentRepository, cache port.Cache) *DepartmentService {
	return &DepartmentService{repo: repo, cache: cache}
}

// List serves CAT-6 (public) and CAT-I1 (internal bulk, activeOnly=false).
// The full catalog is read-through cached at cat:departments (LLD §8);
// activeOnly filtering is applied in-memory after a cache hit so both
// callers share one cache entry.
func (s *DepartmentService) List(ctx context.Context, activeOnly bool) ([]domain.Department, error) {
	all, err := s.listAllCached(ctx)
	if err != nil {
		return nil, err
	}
	if !activeOnly {
		return all, nil
	}
	out := make([]domain.Department, 0, len(all))
	for _, d := range all {
		if d.IsActive {
			out = append(out, d)
		}
	}
	return out, nil
}

func (s *DepartmentService) listAllCached(ctx context.Context) ([]domain.Department, error) {
	if s.cache != nil {
		if raw, err := s.cache.Get(ctx, "cat:departments"); err == nil && raw != nil {
			var cached []domain.Department
			if jerr := json.Unmarshal(raw, &cached); jerr == nil {
				return cached, nil
			}
		}
	}
	all, err := s.repo.List(ctx, false)
	if err != nil {
		return nil, err
	}
	if s.cache != nil {
		if raw, jerr := json.Marshal(all); jerr == nil {
			_ = s.cache.Set(ctx, "cat:departments", raw, departmentsCacheTTL)
		}
	}
	return all, nil
}

// Get serves CAT-7 — single department read. Not cache-fronted (LLD §8
// only names the whole-catalog key); falls straight through to Postgres,
// which is CAT-FAIL-1's source of truth regardless.
func (s *DepartmentService) Get(ctx context.Context, id uuid.UUID) (*domain.Department, error) {
	return s.repo.FindByID(ctx, id)
}

// Create is CAT-1 — add a global-catalog department.
func (s *DepartmentService) Create(ctx context.Context, code, name string, isSystem bool) (*domain.Department, error) {
	if strings.TrimSpace(code) == "" || strings.TrimSpace(name) == "" {
		return nil, domain.NewError(domain.ErrValidation, "code and name are required")
	}
	d, err := s.repo.Insert(ctx, &domain.Department{
		Code:     code,
		Name:     name,
		IsSystem: isSystem,
		IsActive: true,
	})
	if err != nil {
		return nil, err
	}
	s.invalidateCache(ctx)
	return d, nil
}

// Patch is CAT-2 — update name or is_active (code + is_system immutable).
func (s *DepartmentService) Patch(ctx context.Context, id uuid.UUID, name *string, isActive *bool, expectedVersion int64) (*domain.Department, error) {
	if name == nil && isActive == nil {
		return nil, domain.NewError(domain.ErrNoMutableField, "at least one of name or is_active must be provided").
			WithDetails(map[string]any{"code": "no_mutable_field"})
	}
	if name != nil && *name == "" {
		return nil, domain.NewError(domain.ErrValidation, "name must not be empty")
	}
	// D-7/D-9 (system dept retirement) is blocked at the DB level by
	// chk_system_department_active — surfaces as a CHECK violation which
	// bubbles up as a raw error. Map it explicitly here for a clean 422.
	d, err := s.repo.Update(ctx, id, name, isActive, expectedVersion)
	if err != nil {
		if isCheckViolation(err, "chk_system_department_active") {
			return nil, domain.NewError(domain.ErrSystemDepartmentCannotBeRetired, "system department cannot be retired")
		}
		if isCheckViolation(err, "system department name is immutable") {
			return nil, domain.NewError(domain.ErrFieldImmutable, "system department name is immutable").
				WithDetails(map[string]any{"code": "field_immutable", "field": "name"})
		}
		return nil, err
	}
	s.invalidateCache(ctx)
	return d, nil
}

// DeleteBlocked is CAT-3 — always returns the 405-equivalent domain error
// (departments are never hard-deleted; retire via CAT-2 is_active=false).
func (s *DepartmentService) DeleteBlocked() error {
	return domain.NewError(domain.ErrValidation, "departments cannot be deleted; retire via is_active=false").
		WithDetails(map[string]any{"code": "cannot_delete_system_department"})
}

func (s *DepartmentService) invalidateCache(ctx context.Context) {
	if s.cache != nil {
		_ = s.cache.Delete(ctx, "cat:departments")
	}
}

// isCheckViolation is a best-effort matcher for named CHECK constraints,
// ported unchanged from iam-org-membership's operator_service.go.
func isCheckViolation(err error, name string) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	if msg == "" {
		return false
	}
	return contains(msg, name)
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && indexOf(s, substr) >= 0
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
