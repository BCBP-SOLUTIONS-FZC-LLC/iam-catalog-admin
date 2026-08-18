package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
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
