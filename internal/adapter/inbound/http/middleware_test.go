package http

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/pkg/requestctx"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runMiddleware(mw gin.HandlerFunc, req *http.Request) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	mw(c)
	return w
}

func TestRequireOperatorRole(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	reqWithRole := req.WithContext(requestctx.WithContext(req.Context(), &requestctx.RequestContext{Roles: []string{"platform_operator"}}))
	w := runMiddleware(RequireOperatorRole(), reqWithRole)
	assert.NotEqual(t, http.StatusForbidden, w.Code)

	reqNoRole := req.WithContext(requestctx.WithContext(req.Context(), &requestctx.RequestContext{Roles: []string{"tenant_admin"}}))
	w = runMiddleware(RequireOperatorRole(), reqNoRole)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestRequireSystemRole(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	reqWithRole := req.WithContext(requestctx.WithContext(req.Context(), &requestctx.RequestContext{Roles: []string{"iam-system"}}))
	w := runMiddleware(RequireSystemRole(), reqWithRole)
	assert.NotEqual(t, http.StatusForbidden, w.Code)

	reqNoRole := req.WithContext(requestctx.WithContext(req.Context(), &requestctx.RequestContext{Roles: []string{}}))
	w = runMiddleware(RequireSystemRole(), reqNoRole)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestRequireJSONContentType(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "text/plain")
	w := runMiddleware(RequireJSONContentType(), req)
	assert.Equal(t, http.StatusUnsupportedMediaType, w.Code)

	req2 := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte(`{}`)))
	req2.Header.Set("Content-Type", "application/json")
	w2 := runMiddleware(RequireJSONContentType(), req2)
	assert.NotEqual(t, http.StatusUnsupportedMediaType, w2.Code)

	// GET requests are never gated.
	req3 := httptest.NewRequest(http.MethodGet, "/", nil)
	w3 := runMiddleware(RequireJSONContentType(), req3)
	assert.NotEqual(t, http.StatusUnsupportedMediaType, w3.Code)

	// A POST/PUT/PATCH with no body at all (ContentLength == 0) is never
	// gated either — nothing to type-check.
	req4 := httptest.NewRequest(http.MethodPost, "/", nil)
	w4 := runMiddleware(RequireJSONContentType(), req4)
	assert.NotEqual(t, http.StatusUnsupportedMediaType, w4.Code)
}

func TestParseUUIDParam(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: "not-a-uuid"}}
	_, err := parseUUIDParam(c, "id")
	require.Error(t, err)
	var de *domain.DomainError
	require.ErrorAs(t, err, &de)
	assert.Equal(t, domain.ErrValidation.Error(), de.Code)

	c.Params = gin.Params{{Key: "id", Value: ""}}
	_, err = parseUUIDParam(c, "id")
	require.Error(t, err)
}

// TestHandleError_MaxBytesError verifies the chunked-request body-size path
// (CA-SEC-01): a *http.MaxBytesError from MaxBytesReader must produce 413,
// not fall through to the generic 500.
func TestHandleError_MaxBytesError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)

	HandleError(c, &http.MaxBytesError{Limit: 1 << 20})
	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
}

func TestHandleError_GenericFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	HandleError(c, errors.New("boom"))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// fakeLogger captures Error() calls so tests can assert on structured
// logging without depending on platform-gincommon's concrete Zap logger.
type fakeLogger struct {
	msg    string
	fields map[string]interface{}
}

func (f *fakeLogger) Error(msg string, fields map[string]interface{}) {
	f.msg = msg
	f.fields = fields
}

// TestHandleError_GenericFallback_UsesStructuredLoggerWhenSet verifies the
// unhandled-500 branch routes through SetLogger's structured logger
// (carrying request_id/trace_id) rather than the stdlib log.Printf
// fallback, once a logger has been installed — production-readiness fix:
// this path previously always used log.Printf, unstructured and without
// trace correlation.
func TestHandleError_GenericFallback_UsesStructuredLoggerWhenSet(t *testing.T) {
	fl := &fakeLogger{}
	SetLogger(fl)
	t.Cleanup(func() { SetLogger(nil) })

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	HandleError(c, errors.New("boom"))

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, "unhandled 500 error", fl.msg)
	assert.Equal(t, "boom", fl.fields["error"])
	assert.Contains(t, fl.fields["error_type"], "errors.errorString")
}

func TestHandleError_DomainErrorMapping(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	HandleError(c, domain.NewError(domain.ErrDepartmentNotFound, "not found"))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestHandleError_SubCodeOverride_SyncsErrorField verifies LLD §20's
// documented invariant ("error and code must agree, like every other
// error this service returns") holds even when a handler attaches a more
// specific sub-code via WithDetails on top of a generic sentinel — e.g.
// duplicate_code on domain.ErrConflict, invalid_uuid on
// domain.ErrValidation. Regression test for the CAT-Q6 mismatch, where
// "error" previously stayed on the generic sentinel after "code" was
// overridden.
func TestHandleError_SubCodeOverride_SyncsErrorField(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name string
		err  error
		code string
	}{
		{
			name: "duplicate_code sub-code over conflict sentinel",
			err: domain.NewError(domain.ErrConflict, "department code already exists").
				WithDetails(map[string]any{"code": "duplicate_code"}),
			code: "duplicate_code",
		},
		{
			name: "invalid_uuid sub-code over validation_error sentinel",
			err: domain.NewError(domain.ErrValidation, "id is not a valid UUID").
				WithDetails(map[string]any{"code": "invalid_uuid"}),
			code: "invalid_uuid",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

			HandleError(c, tc.err)

			var body map[string]any
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			assert.Equal(t, tc.code, body["code"])
			assert.Equal(t, tc.code, body["error"])
		})
	}
}

func TestHandleError_DependencyUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	HandleError(c, domain.NewError(domain.ErrDependencyUnavailable, "database unavailable"))
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandleError_MissingIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	HandleError(c, domain.NewError(domain.ErrMissingIdentity, "missing identity"))
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandleError_PgErrorUnavailableSQLState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	HandleError(c, &pgconn.PgError{Code: "57014", Message: "canceling statement due to statement timeout"})
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandleError_PgErrorNonUnavailableSQLState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	HandleError(c, &pgconn.PgError{Code: "23505", Message: "duplicate key"})
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestRequireOperator_NoIdentityContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	err := requireOperator(c)
	require.Error(t, err)
	var de *domain.DomainError
	require.ErrorAs(t, err, &de)
	assert.Equal(t, domain.ErrMissingIdentity.Error(), de.Code)
}

func TestRequireSystem_NoIdentityContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	err := requireSystem(c)
	require.Error(t, err)
	var de *domain.DomainError
	require.ErrorAs(t, err, &de)
	assert.Equal(t, domain.ErrMissingIdentity.Error(), de.Code)
}

func TestBufferedWriter_DirectMethods(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	// Fresh writer: Write() before any WriteHeader() call must default
	// status to 200, and Written()/Status() must reflect that.
	buf := &bufferedWriter{ResponseWriter: c.Writer, buf: &bytes.Buffer{}}
	assert.False(t, buf.Written())
	n, err := buf.Write([]byte("hi"))
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	assert.Equal(t, http.StatusOK, buf.status)
	assert.Equal(t, http.StatusOK, buf.Status())
	assert.True(t, buf.Written())

	n2, err := buf.WriteString("more")
	require.NoError(t, err)
	assert.Equal(t, 4, n2)
	assert.Equal(t, "himore", buf.buf.String())

	// A writer that never had Write/WriteHeader called falls back to the
	// underlying ResponseWriter's own Status()/Written() reporting.
	buf2 := &bufferedWriter{ResponseWriter: c.Writer, buf: &bytes.Buffer{}}
	assert.Equal(t, c.Writer.Status(), buf2.Status())
}
