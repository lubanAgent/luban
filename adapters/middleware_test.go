package adapters

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCompanyRecoveryUsesCompanyResponse(t *testing.T) {
	engine := gin.New()
	engine.Use(recoverRequest)
	engine.POST("/test", func(*gin.Context) { panic("private panic detail") })
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/test", nil))
	if !strings.Contains(response.Body.String(), `"errNo":5000`) || strings.Contains(response.Body.String(), "private panic detail") {
		t.Fatalf("unexpected panic response: %s", response.Body.String())
	}
}
