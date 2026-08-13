package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/pkg/requestctx"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/pkg/gincommon"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildIdentityRouter wires the real gincommon.ProtectedMiddlewares chain
// (the same one main.go uses) followed by IdentityBridgeMiddleware, so
// this test exercises the actual gateway-header parsing path rather than
// a hand-built stand-in.
func buildIdentityRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	cfg := gincommon.Config{ServiceName: "catalog-admin-config-test"}
	r.Use(gincommon.ProtectedMiddlewares(cfg)...)
	r.Use(IdentityBridgeMiddleware())
	r.GET("/", func(c *gin.Context) {
		rc, ok := requestctx.FromContext(c.Request.Context())
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "no requestctx"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"user_id": rc.UserID.String(), "roles": rc.Roles})
	})
	return r
}

func TestIdentityBridgeMiddleware_ValidHeaders(t *testing.T) {
	r := buildIdentityRouter()
	userID := uuid.New()
	tenantID := uuid.New()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("x-user-id", userID.String())
	req.Header.Set("x-tenant-id", tenantID.String())
	req.Header.Set("x-tenant-roles", "platform_operator")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), userID.String())
	assert.Contains(t, w.Body.String(), "platform_operator")
}

func TestIdentityBridgeMiddleware_InvalidUserIDHeader(t *testing.T) {
	r := buildIdentityRouter()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("x-user-id", "not-a-uuid")
	req.Header.Set("x-tenant-id", uuid.New().String())

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "missing_identity_headers")
}

func TestIdentityBridgeMiddleware_InvalidTenantIDHeader(t *testing.T) {
	r := buildIdentityRouter()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("x-user-id", uuid.New().String())
	req.Header.Set("x-tenant-id", "not-a-uuid")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "missing_identity_headers")
}

func TestIdentityBridgeMiddleware_SystemPrincipal(t *testing.T) {
	r := buildIdentityRouter()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("x-user-id", "iam-system")
	req.Header.Set("x-tenant-id", uuid.New().String())
	req.Header.Set("x-tenant-roles", "iam-system")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "00000000-0000-0000-0000-000000000000")
}

func TestIdentityBridgeMiddleware_NoUpstreamRequestContext_PassesThrough(t *testing.T) {
	// Calling the middleware directly (bypassing gincommon.ProtectedMiddlewares)
	// exercises the !ok branch: no platform RequestContext was ever stored,
	// so it must c.Next() through rather than reject.
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	IdentityBridgeMiddleware()(c)

	assert.False(t, c.IsAborted())
	_, ok := requestctx.FromContext(c.Request.Context())
	assert.False(t, ok, "no requestctx.RequestContext should have been installed")
}

func TestIdentityBridgeMiddleware_MissingHeaders_HandledUpstream(t *testing.T) {
	// ProtectedMiddlewares' own RequireAuth() rejects a request with no
	// identity headers at all before IdentityBridgeMiddleware ever runs.
	r := buildIdentityRouter()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
