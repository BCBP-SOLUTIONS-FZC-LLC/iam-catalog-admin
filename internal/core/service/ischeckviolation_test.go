package service

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// isCheckViolation's own call site (Patch) always guards err != nil before
// calling it, so its defensive nil/empty-message branches are unreachable
// from Patch — exercised directly here instead.

func TestIsCheckViolation_NilError(t *testing.T) {
	assert.False(t, isCheckViolation(nil, "whatever"))
}

func TestIsCheckViolation_EmptyMessage(t *testing.T) {
	assert.False(t, isCheckViolation(errors.New(""), "whatever"))
}

func TestIsCheckViolation_Match(t *testing.T) {
	assert.True(t, isCheckViolation(errors.New("boom: chk_system_department_active violated"), "chk_system_department_active"))
}

func TestIsCheckViolation_NoMatch(t *testing.T) {
	assert.False(t, isCheckViolation(errors.New("some other error"), "chk_system_department_active"))
}
