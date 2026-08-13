package requestctx

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestWithContextAndFromContext(t *testing.T) {
	rc := &RequestContext{UserID: uuid.New(), TenantID: uuid.New(), Roles: []string{"platform_operator"}}
	ctx := WithContext(context.Background(), rc)

	got, ok := FromContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, rc.UserID, got.UserID)
}

func TestFromContext_MissingReturnsFalse(t *testing.T) {
	_, ok := FromContext(context.Background())
	assert.False(t, ok)
}

func TestHasRole(t *testing.T) {
	rc := &RequestContext{Roles: []string{"platform_operator", "iam-system"}}
	assert.True(t, rc.HasRole("platform_operator"))
	assert.False(t, rc.HasRole("tenant_admin"))
}

func TestIsOperator(t *testing.T) {
	assert.True(t, (&RequestContext{Roles: []string{"platform_operator"}}).IsOperator())
	assert.False(t, (&RequestContext{Roles: []string{"tenant_admin"}}).IsOperator())
}

func TestIsSystem(t *testing.T) {
	assert.True(t, (&RequestContext{Roles: []string{"iam-system"}}).IsSystem())
	assert.False(t, (&RequestContext{Roles: []string{"platform_operator"}}).IsSystem())
}
