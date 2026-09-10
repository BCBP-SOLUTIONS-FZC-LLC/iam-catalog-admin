// Package requestctx carries the validated caller identity extracted from
// trusted gateway headers (x-user-id, x-tenant-id, x-tenant-roles) through
// the request scope. Handlers read this struct rather than reaching back
// into gin.Context. Ported unchanged from iam-org-membership's
// pkg/requestctx/context.go — this service has no tenant concept of its
// own, but still needs the role-check methods (IsOperator/IsSystem) since
// every route is either operator-gated or system-gated (LLD §10).
package requestctx

import (
	"context"

	"github.com/google/uuid"
)

type contextKey struct{}

// RequestContext holds the validated caller identity for a single request.
type RequestContext struct {
	UserID    uuid.UUID
	TenantID  uuid.UUID
	Roles     []string
	ClientIP  string
	UserAgent string
}

// WithContext returns a new context carrying rc.
func WithContext(ctx context.Context, rc *RequestContext) context.Context {
	return context.WithValue(ctx, contextKey{}, rc)
}

// FromContext retrieves the RequestContext set by the identity bridge middleware.
func FromContext(ctx context.Context) (*RequestContext, bool) {
	rc, ok := ctx.Value(contextKey{}).(*RequestContext)
	return rc, ok && rc != nil
}

// HasRole reports whether the caller was granted the given role by the gateway.
func (rc *RequestContext) HasRole(role string) bool {
	for _, r := range rc.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// IsOperator reports whether the caller carries the platform-level
// operator role. Operator endpoints re-check this before any DB access;
// gateway header hygiene prevents client-supplied elevation.
func (rc *RequestContext) IsOperator() bool {
	return rc.HasRole("platform_operator")
}

// IsSystem reports whether the caller is the reserved iam-system
// principal — accepted only on /api/v1/internal/* routes.
func (rc *RequestContext) IsSystem() bool {
	return rc.HasRole("iam-system")
}
