package http

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
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
