//go:build integration

package postgres_test

import (
	"context"
	"testing"

	pgadapter "github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/adapter/outbound/postgres"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/core/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPlanRepository_ScanPlan_NullJsonbFeatureSet covers the
// `p.FeatureSet = map[string]any{}` branch in scanPlan when the JSONB
// column contains the JSON literal `null` (distinct from SQL NULL, which the
// NOT NULL constraint blocks).
func TestPlanRepository_ScanPlan_NullJsonbFeatureSet(t *testing.T) {
	pool, rawPool := setupTestDB(t)
	repo := pgadapter.NewPlanRepository(pool)
	ctx := context.Background()

	_, err := rawPool.Exec(ctx, `UPDATE plans SET feature_set = 'null'::jsonb WHERE code = 'starter'`)
	require.NoError(t, err)

	p, err := repo.FindByCode(ctx, domain.PlanStarter)
	require.NoError(t, err)
	assert.NotNil(t, p.FeatureSet, "JSON-null feature_set must produce empty map, not nil")
}

// TestPlanRepository_Update_MainScanError covers the `return err` branch in
// planUpdateFromTx when the UPDATE's RETURNING row has a malformed feature_set
// that makes scanPlan fail with a non-ErrNoRows error.
func TestPlanRepository_Update_MainScanError(t *testing.T) {
	pool, rawPool := setupTestDB(t)
	repo := pgadapter.NewPlanRepository(pool)
	ctx := context.Background()

	// Corrupt the feature_set to a JSON array; scanPlan cannot unmarshal an
	// array into map[string]any — the error is non-ErrNoRows.
	_, err := rawPool.Exec(ctx, `UPDATE plans SET feature_set = '[1,2,3]'::jsonb WHERE code = 'pro'`)
	require.NoError(t, err)

	// Update does not touch feature_set, so the RETURNING row carries the
	// corrupted value → scanPlan returns a json.UnmarshalTypeError.
	sso := true
	_, err = repo.Update(ctx, domain.PlanPro, &domain.PlanPatch{SSOEnabled: &sso, RecordVersion: 1})
	require.Error(t, err, "Update must propagate the scan error when RETURNING row has malformed feature_set")
}
