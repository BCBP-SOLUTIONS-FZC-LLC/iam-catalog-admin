package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/pkg/gincommon"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSwaggerInitializerHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/swagger/swagger-initializer.js", nil)
	SwaggerInitializerHandler(c)
	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "javascript")
	assert.Contains(t, w.Body.String(), "SwaggerUIBundle")
}

func TestSwaggerThemeHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/swagger/swagger-theme.css", nil)
	SwaggerThemeHandler(c)
	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "css")
	assert.Contains(t, w.Body.String(), "swagger-ui")
}

// Scenario CA-DOCS-03: production mode + wrong/missing DOCS_AUTH_TOKEN → 401 unauthorized.
// NewRouter's docs auth gate (registerDocsRoutes) checks Authorization: Bearer <token>
// when Environment="production" and AuthToken is set. A wrong or absent token must
// return 401 — not 404 or a pass-through — before the Swagger handler fires.
func TestSwagger_ProductionWrongToken_Returns401(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const secret = "prod-secret"
	router := NewRouter(RouterConfig{
		GinConfig: gincommon.Config{ServiceName: "test"},
		Docs: DocsConfig{
			Environment: "production",
			Enabled:     true,
			AuthToken:   secret,
		},
		// Nil handlers: API routes are registered but will panic if hit.
		// We only exercise /swagger/*, which is registered before any API route.
	})
	srv := httptest.NewServer(router.Handler())
	t.Cleanup(srv.Close)

	do := func(authHeader string) int {
		req, err := http.NewRequest(http.MethodGet, srv.URL+"/swagger/index.html", nil)
		require.NoError(t, err)
		if authHeader != "" {
			req.Header.Set("Authorization", authHeader)
		}
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		resp.Body.Close() //nolint:errcheck
		return resp.StatusCode
	}

	// Missing token → 401
	assert.Equal(t, http.StatusUnauthorized, do(""))
	// Wrong token → 401
	assert.Equal(t, http.StatusUnauthorized, do("Bearer wrong-token"))
	// Wrong token of the SAME length as the correct one → 401. Exercises
	// subtle.ConstantTimeCompare's actual byte comparison, not just the
	// length-mismatch short-circuit the case above and below would also
	// pass under.
	sameLengthWrong := make([]byte, len(secret))
	for i := range sameLengthWrong {
		sameLengthWrong[i] = 'x'
	}
	assert.Equal(t, http.StatusUnauthorized, do("Bearer "+string(sameLengthWrong)))
	// Correct token → not 401 (swagger handler may return 200 or redirect)
	assert.NotEqual(t, http.StatusUnauthorized, do("Bearer "+secret))
}
