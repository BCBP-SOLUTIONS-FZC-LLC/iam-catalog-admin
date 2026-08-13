package http

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/pkg/requestctx"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// newTestContext builds a *gin.Context with a request whose context.Context
// already carries a requestctx.RequestContext for the given roles — this
// bypasses IdentityBridgeMiddleware (gateway header parsing) so handler
// tests exercise only the handler + service + role-check logic.
func newTestContext(t *testing.T, method, target string, body []byte, roles []string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, target, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	rc := &requestctx.RequestContext{UserID: uuid.New(), TenantID: uuid.New(), Roles: roles}
	req = req.WithContext(requestctx.WithContext(req.Context(), rc))
	c.Request = req
	return c, w
}

// setParams installs gin path params (c.Param lookups) on a test context —
// gin.CreateTestContext doesn't run the router, so :id/:code style params
// must be set manually.
func setParams(c *gin.Context, params gin.Params) {
	c.Params = params
}
