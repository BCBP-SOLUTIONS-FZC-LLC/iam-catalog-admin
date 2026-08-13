package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeAuthErrors_AddsCodeTo401WithoutOne(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(NormalizeAuthErrors())
	r.GET("/", func(c *gin.Context) {
		// Mimics platform-gincommon's bare 401 body (no "code" field).
		c.AbortWithStatusJSON(http.StatusUnauthorized, map[string]any{"error": "missing or invalid identity", "status": 401})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnauthorized, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "missing_identity_headers", body["code"])
}

func TestNormalizeAuthErrors_PassesThroughNon401(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(NormalizeAuthErrors())
	r.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, true, body["ok"])
}

func TestNormalizeAuthErrors_LeavesExisting401CodeAlone(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(NormalizeAuthErrors())
	r.GET("/", func(c *gin.Context) {
		c.AbortWithStatusJSON(http.StatusUnauthorized, map[string]any{"code": "already_set", "status": 401})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	r.ServeHTTP(w, req)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "already_set", body["code"])
}
