//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ═════════════════════════════════════════════════════════════════════════
// CAT-4a — GET /api/v1/operator/plans
// ═════════════════════════════════════════════════════════════════════════

// Scenario CA4A-A-01: no identity headers → 401
func TestListPlans_NoIdentityHeaders_Returns401(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodGet, "/api/v1/operator/plans", nil, nil)
	assert.Equal(t, http.StatusUnauthorized, status, string(raw))
}

// Scenario CA4A-A-03: tenant_admin role is insufficient → 403
func TestListPlans_TenantAdminRole_Returns403(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodGet, "/api/v1/operator/plans", publicHeaders, nil)
	assert.Equal(t, http.StatusForbidden, status, string(raw))
}

// Scenario CA4A-H-02: each plan contains all expected fields
func TestListPlans_AllExpectedFieldsPresent(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodGet, "/api/v1/operator/plans", operatorHeaders, nil)
	require.Equal(t, http.StatusOK, status, string(raw))

	var body struct {
		Items []map[string]any `json:"items"`
	}
	require.NoError(t, json.Unmarshal(raw, &body))
	require.Len(t, body.Items, 3)

	requiredKeys := []string{"code", "display_name", "trial_duration_days", "sso_enabled", "custom_branding", "record_version"}
	for _, plan := range body.Items {
		for _, key := range requiredKeys {
			_, ok := plan[key]
			assert.True(t, ok, "plan %v missing required field %q", plan["code"], key)
		}
	}
}

// Scenario CA4A-BL-03: no DELETE route for plans (PLAN-4 fixed tier set)
func TestListPlans_NoDeleteRoute_Returns404Or405(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	req, err := http.NewRequest(http.MethodDelete, env.baseURL+"/api/v1/operator/plans/pro", nil)
	require.NoError(t, err)
	operatorHeaders(req)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	assert.Contains(t, []int{http.StatusNotFound, http.StatusMethodNotAllowed}, resp.StatusCode,
		"no DELETE route for plans (PLAN-4)")
}

// ═════════════════════════════════════════════════════════════════════════
// CAT-4b — GET /api/v1/operator/plans/:code
// ═════════════════════════════════════════════════════════════════════════

// Scenario CA4B-A-01: missing operator role → 403
func TestGetPlan_MissingOperatorRole_Returns403(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodGet, "/api/v1/operator/plans/pro", publicHeaders, nil)
	assert.Equal(t, http.StatusForbidden, status, string(raw))
}

// Scenario CA4B-H-01: GET starter plan → 200 with code=starter
func TestGetPlan_Starter_Returns200(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodGet, "/api/v1/operator/plans/starter", operatorHeaders, nil)
	require.Equal(t, http.StatusOK, status, string(raw))
	assert.Equal(t, "starter", decodeMap(t, raw)["code"])
}

// Scenario CA4B-H-02: GET pro plan → 200 with code=pro
func TestGetPlan_Pro_Returns200(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodGet, "/api/v1/operator/plans/pro", operatorHeaders, nil)
	require.Equal(t, http.StatusOK, status, string(raw))
	assert.Equal(t, "pro", decodeMap(t, raw)["code"])
}

// Scenario CA4B-H-03: GET enterprise plan → 200 with code=enterprise
func TestGetPlan_Enterprise_Returns200(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodGet, "/api/v1/operator/plans/enterprise", operatorHeaders, nil)
	require.Equal(t, http.StatusOK, status, string(raw))
	assert.Equal(t, "enterprise", decodeMap(t, raw)["code"])
}

// Scenario CA4B-V-02: uppercase "STARTER" → 404 (case-sensitive code lookup)
func TestGetPlan_UppercaseCode_Returns404(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, _ := doJSON(t, env, http.MethodGet, "/api/v1/operator/plans/STARTER", operatorHeaders, nil)
	assert.Equal(t, http.StatusNotFound, status)
}

// Scenario CA4B-V-03: empty code path segment → 404
func TestGetPlan_EmptyCodePath_Returns404(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, _ := doJSON(t, env, http.MethodGet, "/api/v1/operator/plans/", operatorHeaders, nil)
	assert.Equal(t, http.StatusNotFound, status)
}

// ═════════════════════════════════════════════════════════════════════════
// CAT-5 — PATCH /api/v1/operator/plans/:code
// ═════════════════════════════════════════════════════════════════════════

// Scenario CA5-A-01: no identity headers → 401
func TestPatchPlan_NoIdentityHeaders_Returns401(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/plans/pro", nil,
		map[string]any{"display_name": "X", "record_version": 1})
	assert.Equal(t, http.StatusUnauthorized, status, string(raw))
}

// Scenario CA5-A-02: identity present but no roles → 403
func TestPatchPlan_NoRoles_Returns403(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/plans/pro", func(req *http.Request) {
		req.Header.Set("x-user-id", "11111111-1111-1111-1111-111111111111")
		req.Header.Set("x-tenant-id", "22222222-2222-2222-2222-222222222222")
	}, map[string]any{"display_name": "X", "record_version": 1})
	assert.Equal(t, http.StatusForbidden, status, string(raw))
}

// Scenario CA5-A-03: tenant_admin role → 403
func TestPatchPlan_TenantAdminRole_Returns403(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/plans/pro", publicHeaders,
		map[string]any{"display_name": "X", "record_version": 1})
	assert.Equal(t, http.StatusForbidden, status, string(raw))
}

// Scenario CA5-H-02: update pro plan display_name → 200
func TestPatchPlan_ProDisplayName_Updated(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/plans/pro", operatorHeaders,
		map[string]any{"display_name": "Pro Plan Updated", "record_version": 1})
	require.Equal(t, http.StatusOK, status, string(raw))
	assert.Equal(t, "Pro Plan Updated", decodeMap(t, raw)["display_name"])
}

// Scenario CA5-H-03: update enterprise plan display_name → 200
func TestPatchPlan_EnterpriseDisplayName_Updated(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/plans/enterprise", operatorHeaders,
		map[string]any{"display_name": "Enterprise Updated", "record_version": 1})
	require.Equal(t, http.StatusOK, status, string(raw))
	assert.Equal(t, "Enterprise Updated", decodeMap(t, raw)["display_name"])
}

// Scenario CA5-H-05: set positive workflow_template_limit → 200
func TestPatchPlan_SetPositiveWorkflowLimit_Returns200(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/plans/pro", operatorHeaders,
		map[string]any{"workflow_template_limit": 50, "record_version": 1})
	require.Equal(t, http.StatusOK, status, string(raw))
	assert.EqualValues(t, 50, decodeMap(t, raw)["workflow_template_limit"])
}

// Scenario CA5-H-06: workflow_template_limit=0 is a hard cap, NOT unlimited (distinct from null)
func TestPatchPlan_ZeroWorkflowLimit_StoredAsZeroNotNull(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/plans/pro", operatorHeaders,
		map[string]any{"workflow_template_limit": 0, "record_version": 1})
	require.Equal(t, http.StatusOK, status, string(raw))
	body := decodeMap(t, raw)
	assert.NotNil(t, body["workflow_template_limit"], "0 must be stored as 0, not as null (unlimited)")
	assert.EqualValues(t, 0, body["workflow_template_limit"])
}

// Scenario CA5-H-07: tender_limit=null → 200 (unlimited, CAT-D6)
func TestPatchPlan_TenderLimitNull_MeansUnlimited(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/plans/pro", operatorHeaders,
		map[string]any{"tender_limit": nil, "record_version": 1})
	require.Equal(t, http.StatusOK, status, string(raw))
	assert.Nil(t, decodeMap(t, raw)["tender_limit"])
}

// Scenario CA5-H-08: tender_limit=0 → 200 (hard cap of zero, distinct from null)
func TestPatchPlan_TenderLimitZero_StoredAsZeroNotNull(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/plans/pro", operatorHeaders,
		map[string]any{"tender_limit": 0, "record_version": 1})
	require.Equal(t, http.StatusOK, status, string(raw))
	body := decodeMap(t, raw)
	assert.NotNil(t, body["tender_limit"])
	assert.EqualValues(t, 0, body["tender_limit"])
}

// Scenario CA5-H-10: disable sso_enabled → 200
func TestPatchPlan_DisableSSO_Returns200(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/plans/enterprise", operatorHeaders,
		map[string]any{"sso_enabled": false, "record_version": 1})
	require.Equal(t, http.StatusOK, status, string(raw))
	assert.Equal(t, false, decodeMap(t, raw)["sso_enabled"])
}

// Scenario CA5-H-11: custom_branding=logo → 200
func TestPatchPlan_CustomBrandingLogo_Returns200(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/plans/enterprise", operatorHeaders,
		map[string]any{"custom_branding": "logo", "record_version": 1})
	require.Equal(t, http.StatusOK, status, string(raw))
	assert.Equal(t, "logo", decodeMap(t, raw)["custom_branding"])
}

// Scenario CA5-H-12: custom_branding=none → 200
func TestPatchPlan_CustomBrandingNone_Returns200(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/plans/enterprise", operatorHeaders,
		map[string]any{"custom_branding": "none", "record_version": 1})
	require.Equal(t, http.StatusOK, status, string(raw))
	assert.Equal(t, "none", decodeMap(t, raw)["custom_branding"])
}

// Scenario CA5-H-13: feature_set with scalar values (string, bool, number) → 200
func TestPatchPlan_FeatureSetScalarValues_Returns200(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/plans/pro", operatorHeaders,
		map[string]any{
			"feature_set":    map[string]any{"rate_limit": 1000, "analytics": true, "tier": "gold"},
			"record_version": 1,
		})
	require.Equal(t, http.StatusOK, status, string(raw))
}

// Scenario CA5-H-14: trial_duration_days=0 is valid (means no trial)
func TestPatchPlan_TrialDurationDaysZero_Returns200(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/plans/starter", operatorHeaders,
		map[string]any{"trial_duration_days": 0, "record_version": 1})
	require.Equal(t, http.StatusOK, status, string(raw))
	assert.EqualValues(t, 0, decodeMap(t, raw)["trial_duration_days"])
}

// Scenario CA5-H-15: multiple fields updated atomically in one request
func TestPatchPlan_MultipleFields_AllUpdated(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/plans/pro", operatorHeaders,
		map[string]any{
			"display_name":   "Multi Plan",
			"sso_enabled":    true,
			"tender_limit":   100,
			"record_version": 1,
		})
	require.Equal(t, http.StatusOK, status, string(raw))
	body := decodeMap(t, raw)
	assert.Equal(t, "Multi Plan", body["display_name"])
	assert.Equal(t, true, body["sso_enabled"])
	assert.EqualValues(t, 100, body["tender_limit"])
}

// Scenario CA5-H-16: Unicode display_name stored and returned correctly
func TestPatchPlan_UnicodeDisplayName_StoredCorrectly(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	name := "خطة المؤسسة"
	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/plans/enterprise", operatorHeaders,
		map[string]any{"display_name": name, "record_version": 1})
	require.Equal(t, http.StatusOK, status, string(raw))
	assert.Equal(t, name, decodeMap(t, raw)["display_name"])
}

// Scenario CA5-H-18 / CROSS-CA-07 / CA-DI-04: two consecutive PATCHes with bumped versions → both 200
func TestPatchPlan_TwoConsecutivePatches_BothSucceed_VersionMonotonic(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	_, raw1 := doJSON(t, env, http.MethodPatch, "/api/v1/operator/plans/starter", operatorHeaders,
		map[string]any{"display_name": "First Patch", "record_version": 1})
	require.EqualValues(t, 2, decodeMap(t, raw1)["record_version"])

	_, raw2 := doJSON(t, env, http.MethodPatch, "/api/v1/operator/plans/starter", operatorHeaders,
		map[string]any{"display_name": "Second Patch", "record_version": 2})
	require.EqualValues(t, 3, decodeMap(t, raw2)["record_version"])
}

// Scenario CA5-H-19: code in PATCH body is silently ignored (code is path param only)
func TestPatchPlan_CodeInBody_Ignored(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/plans/pro", operatorHeaders,
		map[string]any{"code": "starter", "display_name": "Code Ignored", "record_version": 1})
	require.Equal(t, http.StatusOK, status, string(raw))
	assert.Equal(t, "pro", decodeMap(t, raw)["code"], "code in body must be ignored — path code wins")
}

// Scenario CA5-V-01: no mutable fields → 400 no_mutable_field
func TestPatchPlan_NoMutableFields_Returns400(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/plans/pro", operatorHeaders,
		map[string]any{"record_version": 1})
	require.Equal(t, http.StatusBadRequest, status, string(raw))
	assert.Equal(t, "no_mutable_field", decodeMap(t, raw)["code"])
}

// Scenario CA5-V-04: trial_duration_days < 0 → 400
func TestPatchPlan_NegativeTrialDays_Returns400(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/plans/starter", operatorHeaders,
		map[string]any{"trial_duration_days": -1, "record_version": 1})
	assert.Equal(t, http.StatusBadRequest, status, string(raw))
}

// Scenario CA5-V-05: invalid custom_branding value → 400
func TestPatchPlan_InvalidBranding_Returns400(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/plans/pro", operatorHeaders,
		map[string]any{"custom_branding": "premium", "record_version": 1})
	assert.Equal(t, http.StatusBadRequest, status, string(raw))
}

// Scenario CA5-V-09: display_name="" or whitespace-only → 400 validation_error
func TestPatchPlan_EmptyDisplayName_Returns400(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/plans/pro", operatorHeaders,
		map[string]any{"display_name": "   ", "record_version": 1})
	require.Equal(t, http.StatusBadRequest, status, string(raw))
	assert.Equal(t, "validation_error", decodeMap(t, raw)["code"])
}

// Scenario CA5-V-08: body is JSON array → 400
func TestPatchPlan_BodyIsArray_Returns400(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	req, err := http.NewRequest(http.MethodPatch, env.baseURL+"/api/v1/operator/plans/pro",
		bytes.NewBufferString(`[]`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	operatorHeaders(req)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	raw, _ := io.ReadAll(resp.Body)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode, string(raw))
}

// Scenario CA5-NF-02: uppercase "STARTER" → 404 (case-sensitive)
func TestPatchPlan_UppercaseCode_Returns404(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, _ := doJSON(t, env, http.MethodPatch, "/api/v1/operator/plans/STARTER", operatorHeaders,
		map[string]any{"display_name": "X", "record_version": 1})
	assert.Equal(t, http.StatusNotFound, status)
}

// Scenario CA5-OL-02: record_version=0 when seeded version is ≥1 → 409
func TestPatchPlan_VersionZero_Returns409(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/plans/pro", operatorHeaders,
		map[string]any{"display_name": "X", "record_version": 0})
	require.Equal(t, http.StatusConflict, status, string(raw))
	assert.Equal(t, "optimistic_lock_conflict", decodeMap(t, raw)["code"])
}

// Scenario CA5-OL-03: correct record_version → 200 with version incremented
func TestPatchPlan_CorrectVersion_Returns200_VersionIncremented(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/plans/starter", operatorHeaders,
		map[string]any{"display_name": "Correct Version", "record_version": 1})
	require.Equal(t, http.StatusOK, status, string(raw))
	assert.EqualValues(t, 2, decodeMap(t, raw)["record_version"])
}

// Scenario CA5-M-01: malformed JSON → 400
func TestPatchPlan_MalformedJSON_Returns400(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	req, err := http.NewRequest(http.MethodPatch, env.baseURL+"/api/v1/operator/plans/pro",
		bytes.NewBufferString(`{bad json`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	operatorHeaders(req)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	raw, _ := io.ReadAll(resp.Body)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode, string(raw))
}

// Scenario CA5-M-02: empty body → 400 no_mutable_field
func TestPatchPlan_EmptyBody_Returns400(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/plans/pro", operatorHeaders,
		map[string]any{})
	require.Equal(t, http.StatusBadRequest, status, string(raw))
	assert.Equal(t, "no_mutable_field", decodeMap(t, raw)["code"])
}

// Scenario CA5-CT-01: Content-Type: text/plain → 415
func TestPatchPlan_WrongContentType_Returns415(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	req, err := http.NewRequest(http.MethodPatch, env.baseURL+"/api/v1/operator/plans/pro",
		bytes.NewBufferString(`{"display_name":"X","record_version":1}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "text/plain")
	operatorHeaders(req)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	raw, _ := io.ReadAll(resp.Body)
	assert.Equal(t, http.StatusUnsupportedMediaType, resp.StatusCode, string(raw))
}

// Scenario CA5-NOEVT-01: no outbox_events table after plan patch
func TestPatchPlan_NoOutboxEventsEmitted(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	doJSON(t, env, http.MethodPatch, "/api/v1/operator/plans/pro", operatorHeaders,
		map[string]any{"display_name": "No Events Plan", "record_version": 1}) //nolint:errcheck

	var exists bool
	err := env.rawPool.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM information_schema.tables
         WHERE table_schema='public' AND table_name='outbox_events')`).Scan(&exists)
	require.NoError(t, err)
	assert.False(t, exists, "outbox_events must not exist — catalog-admin has no outbox (LLD §7)")
}

// Scenario CA5-BL-02 / CROSS-CA-04: after PATCH, GET /operator/plans reflects updated value
func TestPatchPlan_CacheInvalidated_ListReflectsUpdate(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	doJSON(t, env, http.MethodPatch, "/api/v1/operator/plans/starter", operatorHeaders,
		map[string]any{"display_name": "Cache Busted", "record_version": 1}) //nolint:errcheck

	_, raw := doJSON(t, env, http.MethodGet, "/api/v1/operator/plans", operatorHeaders, nil)
	assert.Contains(t, string(raw), "Cache Busted", "updated plan must be visible in subsequent list")
}

// Scenario CA5-CON-01: concurrent PATCHes with same version → one 200, one 409
func TestPatchPlan_ConcurrentSameVersion_ExactlyOneWins(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	results := make([]int, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			status, _ := doJSON(t, env, http.MethodPatch, "/api/v1/operator/plans/pro", operatorHeaders,
				map[string]any{"display_name": "Concurrent", "record_version": 1})
			results[idx] = status
		}(i)
	}
	wg.Wait()

	okCount, conflictCount := 0, 0
	for _, s := range results {
		switch s {
		case http.StatusOK:
			okCount++
		case http.StatusConflict:
			conflictCount++
		}
	}
	assert.Equal(t, 1, okCount, "exactly one goroutine must win the optimistic lock")
	assert.Equal(t, 1, conflictCount, "exactly one goroutine must receive 409 optimistic_lock_conflict")
}

// Scenario CA-BL-02: record_version missing from plan PATCH body → decoded as 0 → 409 (not 400)
func TestPatchPlan_MissingRecordVersion_Returns409NotBadRequest(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	status, raw := doJSON(t, env, http.MethodPatch, "/api/v1/operator/plans/pro", operatorHeaders,
		map[string]any{"display_name": "No Version"}) // record_version absent → 0
	require.Equal(t, http.StatusConflict, status, string(raw))
	assert.Equal(t, "optimistic_lock_conflict", decodeMap(t, raw)["code"])
}
