package domain

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewError_WrapsSentinel(t *testing.T) {
	err := NewError(ErrDepartmentNotFound, "department not found")
	assert.Equal(t, "department_not_found", err.Code)
	assert.Equal(t, "department not found", err.Message)
	assert.True(t, errors.Is(err, ErrDepartmentNotFound))
	assert.Equal(t, "department_not_found: department not found", err.Error())
}

func TestWithDetails_Chainable(t *testing.T) {
	err := NewError(ErrOptimisticLockConflict, "conflict").WithDetails(map[string]any{"record_version": int64(3)})
	assert.Equal(t, int64(3), err.Details["record_version"])
}

func TestDomainError_Unwrap(t *testing.T) {
	err := NewError(ErrValidation, "bad input")
	assert.Equal(t, ErrValidation, errors.Unwrap(err))
}
