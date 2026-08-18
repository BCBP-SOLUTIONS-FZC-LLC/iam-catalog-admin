//go:build e2e

package e2e_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRoles_AppRoleHasNoBYPASSRLS closes LLD §9's claim that "CI verifies
// this [catalog_admin_app has no BYPASSRLS] the same way MIG-3/MIG-5
// verify it for O&M today" — previously an aspirational statement with no
// actual test or CI job behind it (found in a reconciliation pass against
// iam-org-membership's own TestRLS_Case1b_AppRoleHasNoBYPASSRLS, which
// this test mirrors). Neither table in this service is RLS-protected —
// there is nothing for catalog_admin_app to bypass — but the grant is
// withheld anyway, in defense in depth, per migration
// 000003_roles_grants.up.sql's own stated rationale, and this is the test
// that actually holds that line to account.
func TestRoles_AppRoleHasNoBYPASSRLS(t *testing.T) {
	env := newE2EEnv(t)

	var bypass bool
	err := env.rawPool.QueryRow(context.Background(), `
		SELECT rolbypassrls FROM pg_roles WHERE rolname = 'catalog_admin_app'`).Scan(&bypass)
	require.NoError(t, err, "catalog_admin_app role must exist (created by 000003_roles_grants.up.sql)")
	assert.False(t, bypass, "catalog_admin_app must NOT hold BYPASSRLS (LLD §9)")
}
