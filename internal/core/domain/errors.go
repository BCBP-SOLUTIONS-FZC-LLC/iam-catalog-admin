// Package domain holds the core entity types, value objects, and error
// catalogue for the Catalog / Admin Config Service. It imports nothing
// outside the standard library and third-party value libraries — no
// framework, no adapter, no infrastructure.
package domain

import "errors"

// Sentinel error codes. String value = wire code returned in the response
// body. Kept as errors.New so callers can wrap+unwrap with errors.Is. This
// is the same pattern as iam-org-membership's domain/errors.go, trimmed to
// the codes this service's two aggregates actually need.
var (
	// Common / cross-cutting
	ErrValidation             = errors.New("validation_error")
	ErrMissingIdentity        = errors.New("missing_identity_headers")
	ErrInsufficientRole       = errors.New("insufficient_role")
	ErrOptimisticLockConflict = errors.New("optimistic_lock_conflict")
	ErrDependencyUnavailable  = errors.New("dependency_unavailable")
	ErrNoMutableField         = errors.New("no_mutable_field")

	// Not-found (404 family)
	ErrDepartmentNotFound = errors.New("department_not_found")
	ErrPlanNotFound       = errors.New("plan_not_found")

	// Conflict (409 family)
	ErrConflict = errors.New("conflict")

	// Domain-rule (422 family)
	ErrFieldImmutable                  = errors.New("field_immutable")
	ErrSystemDepartmentCannotBeRetired = errors.New("system_department_cannot_be_retired")

	// Dependency (503 family)
	ErrDBUnavailable    = errors.New("db_unavailable")
	ErrCacheUnavailable = errors.New("cache_unavailable")
)

// DomainError wraps a sentinel with a human-readable message and optional
// cause. Handlers translate DomainError.Code into an HTTP status via the
// mapping in the http adapter.
type DomainError struct {
	Code    string
	Message string
	Cause   error
	// Details is a free-form map serialised into the response body — used
	// for error-specific context (e.g. record_version on a 409).
	Details map[string]any
}

func (e *DomainError) Error() string { return e.Code + ": " + e.Message }
func (e *DomainError) Unwrap() error { return e.Cause }

// NewError creates a DomainError whose Cause is the given sentinel and whose
// wire Code is the sentinel's string value.
func NewError(sentinel error, message string) *DomainError {
	return &DomainError{Code: sentinel.Error(), Message: message, Cause: sentinel}
}

// WithDetails attaches structured detail fields to the error body. Chainable.
func (e *DomainError) WithDetails(d map[string]any) *DomainError {
	e.Details = d
	return e
}
