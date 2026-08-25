package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/pkg/gincommon"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── readyz health-check white-box tests ──────────────────────────────────
// These exercise the two 503 branches that e2e cannot reach without
// deliberately bringing down Postgres or Valkey.

type errPinger struct{ err error }

func (p *errPinger) Health(_ context.Context) error { return p.err }

type okPinger struct{}

func (p *okPinger) Health(_ context.Context) error { return nil }

// TestReadyz_PostgresDown verifies a 503 when Postgres health check fails.
func TestReadyz_PostgresDown(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &healthHandlers{
		postgres: &errPinger{err: errors.New("connection refused")},
		cache:    &okPinger{},
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/readyz", nil)
	h.readyz(c)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// TestReadyz_CacheDown verifies a 503 when Valkey health check fails (but
// Postgres is healthy) — cache is advisory everywhere except /readyz so the
// pod is removed from rotation rather than silently amplifying DB load.
func TestReadyz_CacheDown(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &healthHandlers{
		postgres: &okPinger{},
		cache:    &errPinger{err: errors.New("dial tcp: connection refused")},
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/readyz", nil)
	h.readyz(c)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// TestDocsRoutes_Inactive_Returns404 verifies that when docs are not active
// (production + disabled), the /swagger/* routes are never registered.
func TestDocsRoutes_Inactive_Returns404(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	registerDocsRoutes(r, RouterConfig{
		Docs: DocsConfig{Environment: "production", Enabled: false},
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/swagger/index.html", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestReadyz_BothHealthy verifies the 200 success path when both Postgres and
// Valkey are healthy (line 174: c.JSON(http.StatusOK, ...)).
func TestReadyz_BothHealthy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &healthHandlers{postgres: &okPinger{}, cache: &okPinger{}}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/readyz", nil)
	h.readyz(c)
	assert.Equal(t, http.StatusOK, w.Code)
}

// warnCaptureLogger is a minimal port.Logger stub that records whether Warn
// was called — used to test the "production docs without auth token" warning
// branch (registerDocsRoutes lines 212-214).
type warnCaptureLogger struct{ warned bool }

func (l *warnCaptureLogger) Debug(string, map[string]interface{}) {}
func (l *warnCaptureLogger) Info(string, map[string]interface{})  {}
func (l *warnCaptureLogger) Warn(string, map[string]interface{})  { l.warned = true }
func (l *warnCaptureLogger) Error(string, map[string]interface{}) {}

// TestRegisterDocsRoutes_ProdNoAuthToken_WithLogger verifies that enabling
// docs in production without an auth token emits a Warn log (lines 212-214).
func TestRegisterDocsRoutes_ProdNoAuthToken_WithLogger(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	l := &warnCaptureLogger{}
	registerDocsRoutes(r, RouterConfig{
		Docs:      DocsConfig{Environment: "production", Enabled: true, AuthToken: ""},
		GinConfig: gincommon.Config{Logger: l},
	})
	assert.True(t, l.warned, "expected Warn to be called for unauthenticated production docs")
}

// newTestRouter builds a minimal but complete Router that covers the body-size
// and NoMethod middleware (routes that go through the global Use() chain).
func newTestRouter() *Router {
	deptSvc := newTestDepartmentService()
	planSvc := newTestPlanService()
	return NewRouter(RouterConfig{
		GinConfig:         gincommon.Config{ServiceName: "test"},
		Docs:              DocsConfig{Environment: "dev"},
		DepartmentHandler: NewDepartmentHandler(deptSvc),
		PlanHandler:       NewPlanHandler(planSvc),
		InternalHandler:   NewInternalHandler(deptSvc, planSvc),
		Postgres:          &okPinger{},
		Cache:             &okPinger{},
	})
}

// TestNewRouter_BodySizeLimitExceeded verifies the body-size middleware returns
// 413 when ContentLength > 1 MB (lines 113-122 in NewRouter).
func TestNewRouter_BodySizeLimitExceeded(t *testing.T) {
	router := newTestRouter()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	req.ContentLength = 2 << 20 // 2 MB > 1 MB limit
	router.Handler().ServeHTTP(w, req)
	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
}

// TestNewRouter_NoMethodHandler verifies that hitting a registered path with
// the wrong HTTP method triggers the custom NoMethod handler (lines 96-103)
// and also exercises the body-size pass-through (lines 123-124).
func TestNewRouter_NoMethodHandler(t *testing.T) {
	router := newTestRouter()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/readyz", nil)
	router.Handler().ServeHTTP(w, req)
	require.Equal(t, http.StatusMethodNotAllowed, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "method_not_allowed", body["code"])
}

// TestDocsRoutes_CSSAndInitializerBranches exercises the two non-default
// branches in the /swagger/*any route switch.
func TestDocsRoutes_CSSAndInitializerBranches(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	registerDocsRoutes(r, RouterConfig{
		Docs: DocsConfig{Environment: "dev", Enabled: true},
	})

	cases := []struct {
		path        string
		contentType string
	}{
		{"/swagger/index.css", "css"},
		{"/swagger/swagger-initializer.js", "javascript"},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "path=%s", tc.path)
		assert.Contains(t, w.Header().Get("Content-Type"), tc.contentType, "path=%s", tc.path)
	}
}
