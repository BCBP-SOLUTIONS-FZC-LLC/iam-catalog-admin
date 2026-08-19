package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/pkg/gincommon"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestNewErrorResponse_NilContext(t *testing.T) {
	er := newErrorResponse(nil, "some_code", "message")
	assert.Equal(t, "some_code", er.Code)
	assert.Empty(t, er.RequestID)
	assert.Empty(t, er.TraceID)
}

// RequestIDFromContext(c) reads a value stamped by platform-gincommon's own
// request-ID middleware, which unit tests here never run — so it's always
// empty, and newErrorResponse must fall back to the raw x-request-id
// request header.
func TestNewErrorResponse_RequestIDFromRequestHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Request.Header.Set(gincommon.HeaderRequestID, "req-from-request-header")

	er := newErrorResponse(c, "some_code", "message")
	assert.Equal(t, "req-from-request-header", er.RequestID)
}

// With neither the context value nor the request header set, the final
// fallback reads whatever has already been written to the response's
// X-Request-ID header (e.g. by an earlier middleware in the chain).
func TestNewErrorResponse_RequestIDFromResponseHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Writer.Header().Set(gincommon.HeaderRequestIDResponse, "req-from-response-header")

	er := newErrorResponse(c, "some_code", "message")
	assert.Equal(t, "req-from-response-header", er.RequestID)
}
