package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/pkg/gincommon"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
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

// TestNewErrorResponse_TraceIDFromSpan verifies that a valid OTel span in the
// request context populates the trace_id field (the span.SpanContext().IsValid()
// branch in newErrorResponse).
func TestNewErrorResponse_TraceIDFromSpan(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tp := sdktrace.NewTracerProvider()
	tracer := tp.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test-span")
	defer span.End()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)

	er := newErrorResponse(c, "some_code", "message")
	assert.NotEmpty(t, er.TraceID)
	assert.Len(t, er.TraceID, 32)
}

// TestNewErrorResponse_RequestIDFromGinContextValue verifies the first
// RequestIDFromContext(c) branch — the value gincommon's RequestIDMiddleware
// stamps into the gin context key "request_id".
func TestNewErrorResponse_RequestIDFromGinContextValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Set("request_id", "req-from-gin-context")

	er := newErrorResponse(c, "some_code", "message")
	assert.Equal(t, "req-from-gin-context", er.RequestID)
}
