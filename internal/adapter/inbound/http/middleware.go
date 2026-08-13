// Package http implements the inbound HTTP surface: Gin handlers, DTOs,
// and the middleware chain that binds gateway-injected identity into the
// request context. Unlike iam-org-membership, there is no RLS GUC to
// bridge here (LLD §9 — neither departments nor plans carries a
// tenant_id), so IdentityBridgeMiddleware only does the identity-parsing
// half of O&M's GUCBridgeMiddleware.
package http

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/pkg/requestctx"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/pkg/gincommon"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// IdentityBridgeMiddleware runs after gincommon.ProtectedMiddlewares. It
// parses the gateway-injected identity into typed uuid.UUID values and
// stores a requestctx.RequestContext for handlers (role checks only — no
// RLS GUC to bridge in this service).
func IdentityBridgeMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		platformRc, ok := gincommon.RequestContext(c)
		if !ok {
			c.Next()
			return
		}
		var userID uuid.UUID
		if platformRc.UserID == "iam-system" {
			userID = uuid.Nil
		} else if id, err := uuid.Parse(platformRc.UserID); err == nil {
			userID = id
		} else {
			er := newErrorResponse(c, "missing_identity_headers", "x-user-id header is not a valid UUID", nil)
			er.Status = http.StatusUnauthorized
			c.AbortWithStatusJSON(http.StatusUnauthorized, er)
			return
		}
		tenantID, err := uuid.Parse(platformRc.TenantID)
		if err != nil {
			er := newErrorResponse(c, "missing_identity_headers", "x-tenant-id header is not a valid UUID", nil)
			er.Status = http.StatusUnauthorized
			c.AbortWithStatusJSON(http.StatusUnauthorized, er)
			return
		}
		rc := &requestctx.RequestContext{
			UserID: userID, TenantID: tenantID,
			Roles: platformRc.Roles, ClientIP: platformRc.ClientIP,
			UserAgent: c.Request.Header.Get("User-Agent"),
		}
		ctx := requestctx.WithContext(c.Request.Context(), rc)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

// RequireJSONContentType rejects POST/PUT/PATCH requests without
// application/json.
func RequireJSONContentType() gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.Request.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch:
		default:
			c.Next()
			return
		}
		if c.Request.ContentLength == 0 {
			c.Next()
			return
		}
		if c.ContentType() != "application/json" {
			er := newErrorResponse(c, "unsupported_media_type", "Content-Type must be application/json", nil)
			er.Status = http.StatusUnsupportedMediaType
			c.AbortWithStatusJSON(http.StatusUnsupportedMediaType, er)
			return
		}
		c.Next()
	}
}

// RequireSystemRole gates /api/v1/internal/* to the reserved iam-system
// principal (LLD §9 — CAT-I1/CAT-I2 are mesh-only). NetworkPolicy is the
// primary defence; this middleware is defense-in-depth.
func RequireSystemRole() gin.HandlerFunc {
	return func(c *gin.Context) {
		rc, ok := requestctx.FromContext(c.Request.Context())
		if !ok || !rc.HasRole("iam-system") {
			er := newErrorResponse(c, "insufficient_role", "internal route requires iam-system role", nil)
			er.Status = http.StatusForbidden
			c.AbortWithStatusJSON(http.StatusForbidden, er)
			return
		}
		c.Next()
	}
}

// RequireOperatorRole gates /api/v1/operator/* (LLD §9). Every operator
// route re-checks this in-handler too (requireOperator), mirroring O&M's
// AUTH-6 defense-in-depth pattern.
func RequireOperatorRole() gin.HandlerFunc {
	return func(c *gin.Context) {
		rc, ok := requestctx.FromContext(c.Request.Context())
		if !ok || !rc.IsOperator() {
			er := newErrorResponse(c, "insufficient_role", "operator route requires platform_operator role", nil)
			er.Status = http.StatusForbidden
			c.AbortWithStatusJSON(http.StatusForbidden, er)
			return
		}
		c.Next()
	}
}

// NormalizeAuthErrors intercepts 401 responses from platform-gincommon's
// auth middleware and rewrites them to match this service's standard
// error envelope. Ported unchanged from iam-org-membership's middleware.go.
func NormalizeAuthErrors() gin.HandlerFunc {
	return func(c *gin.Context) {
		buf := &bufferedWriter{ResponseWriter: c.Writer, buf: &bytes.Buffer{}}
		c.Writer = buf
		c.Next()
		if buf.status == http.StatusUnauthorized {
			var raw map[string]any
			if err := json.Unmarshal(buf.buf.Bytes(), &raw); err == nil {
				if _, hasCode := raw["code"]; !hasCode {
					raw["code"] = "missing_identity_headers"
					raw["error"] = "missing_identity_headers"
					rewritten, _ := json.Marshal(raw)
					buf.ResponseWriter.Header().Set("Content-Type", "application/json; charset=utf-8")
					buf.ResponseWriter.WriteHeader(http.StatusUnauthorized)
					_, _ = buf.ResponseWriter.Write(rewritten)
					return
				}
			}
		}
		if buf.status != 0 {
			buf.ResponseWriter.WriteHeader(buf.status)
		}
		_, _ = buf.ResponseWriter.Write(buf.buf.Bytes())
	}
}

type bufferedWriter struct {
	gin.ResponseWriter
	buf    *bytes.Buffer
	status int
}

func (w *bufferedWriter) WriteHeader(code int) { w.status = code }
func (w *bufferedWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.buf.Write(b)
}
func (w *bufferedWriter) Status() int {
	if w.status == 0 {
		return w.ResponseWriter.Status()
	}
	return w.status
}
func (w *bufferedWriter) Written() bool { return w.buf.Len() > 0 || w.status != 0 }
func (w *bufferedWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

// HandleError writes a JSON error response derived from err. Recognises
// *domain.DomainError and maps its Code to an HTTP status. Ported
// unchanged from iam-org-membership's middleware.go, trimmed to this
// service's smaller error catalogue.
func HandleError(c *gin.Context, err error) {
	var de *domain.DomainError
	if errors.As(err, &de) {
		status := domainErrorStatus(de)
		body := newErrorResponse(c, de.Code, de.Message, nil)
		body.Status = status
		mergedBody := errorResponseWithDetails(body, de.Details)
		c.AbortWithStatusJSON(status, mergedBody)
		return
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if isDBUnavailableSQLState(pgErr.Code) {
			er := newErrorResponse(c, domain.ErrDBUnavailable.Error(), "database unavailable", nil)
			er.Status = http.StatusServiceUnavailable
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, er)
			return
		}
	}
	log.Printf("[DEBUG] unhandled 500 error type=%T value=%v", err, err)
	er := newErrorResponse(c, "internal_error", "an unexpected error occurred", nil)
	er.Status = http.StatusInternalServerError
	c.AbortWithStatusJSON(http.StatusInternalServerError, er)
}

// isDBUnavailableSQLState returns true for SQLSTATE classes that indicate
// a connectivity or resource-exhaustion failure rather than a logic error.
func isDBUnavailableSQLState(code string) bool {
	if len(code) < 2 {
		return false
	}
	switch strings.ToUpper(code[:2]) {
	case "08", "53", "57", "58":
		return true
	}
	return false
}

func errorResponseWithDetails(er ErrorResponse, details map[string]any) map[string]any {
	out := map[string]any{
		"error":   er.Error,
		"code":    er.Code,
		"status":  er.Status,
		"message": er.Message,
	}
	if er.TraceID != "" {
		out["trace_id"] = er.TraceID
	}
	if er.RequestID != "" {
		out["request_id"] = er.RequestID
	}
	for k, v := range details {
		out[k] = v
	}
	return out
}

// domainErrorStatus maps a DomainError to its HTTP status.
func domainErrorStatus(de *domain.DomainError) int {
	switch {
	case errors.Is(de.Cause, domain.ErrValidation),
		errors.Is(de.Cause, domain.ErrNoMutableField):
		return http.StatusBadRequest
	case errors.Is(de.Cause, domain.ErrMissingIdentity):
		return http.StatusUnauthorized
	case errors.Is(de.Cause, domain.ErrInsufficientRole):
		return http.StatusForbidden
	case errors.Is(de.Cause, domain.ErrDepartmentNotFound),
		errors.Is(de.Cause, domain.ErrPlanNotFound):
		return http.StatusNotFound
	case errors.Is(de.Cause, domain.ErrOptimisticLockConflict),
		errors.Is(de.Cause, domain.ErrConflict):
		return http.StatusConflict
	case errors.Is(de.Cause, domain.ErrDBUnavailable),
		errors.Is(de.Cause, domain.ErrCacheUnavailable),
		errors.Is(de.Cause, domain.ErrDependencyUnavailable):
		return http.StatusServiceUnavailable
	case errors.Is(de.Cause, domain.ErrMethodNotAllowed):
		return http.StatusMethodNotAllowed
	default:
		// Remaining domain codes (field_immutable, system_name_immutable,
		// system_department_cannot_be_retired) are 422 domain-rule violations.
		return http.StatusUnprocessableEntity
	}
}

// parseUUIDParam parses a required path param as a UUID, returning a
// validation DomainError on failure.
func parseUUIDParam(c *gin.Context, name string) (uuid.UUID, error) {
	raw := c.Param(name)
	if raw == "" {
		return uuid.Nil, domain.NewError(domain.ErrValidation, name+" is required").
			WithDetails(map[string]any{"code": "invalid_uuid"})
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, domain.NewError(domain.ErrValidation, name+" is not a valid UUID").
			WithDetails(map[string]any{"code": "invalid_uuid"})
	}
	return id, nil
}

// requireOperator is the AUTH-6-equivalent gate for /operator/* routes.
// Re-checked here in addition to the RequireOperatorRole middleware
// (defense-in-depth), mirroring O&M's requireOperator.
func requireOperator(c *gin.Context) error {
	rc, ok := requestctx.FromContext(c.Request.Context())
	if !ok {
		return domain.NewError(domain.ErrMissingIdentity, "missing identity")
	}
	if !rc.IsOperator() {
		return domain.NewError(domain.ErrInsufficientRole, "platform_operator required")
	}
	return nil
}

// requireSystem is the defense-in-depth gate re-checked inside every
// /internal/* handler, mirroring requireOperator's pattern.
func requireSystem(c *gin.Context) error {
	rc, ok := requestctx.FromContext(c.Request.Context())
	if !ok {
		return domain.NewError(domain.ErrMissingIdentity, "missing identity")
	}
	if !rc.IsSystem() {
		return domain.NewError(domain.ErrInsufficientRole, "iam-system required")
	}
	return nil
}
