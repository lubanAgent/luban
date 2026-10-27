package adapters

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ips "git.yukework.com/saas/ips-client/v2"
	"github.com/gin-gonic/gin"
)

type fakeSession struct {
	session ips.IpsSession
	err     error
}

func (client fakeSession) GetSession(*gin.Context) (ips.IpsSession, error) {
	return client.session, client.err
}

func TestCompanyLoginProtectsBusinessIdentity(t *testing.T) {
	for _, test := range []struct {
		name   string
		client fakeSession
		allow  bool
	}{
		{"authenticated", fakeSession{session: ips.IpsSession{Uid: 123, Uname: "tester"}}, true},
		{"empty session", fakeSession{}, false},
		{"failed session", fakeSession{session: ips.IpsSession{Uid: 123}, err: errors.New("invalid session")}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			engine := gin.New()
			engine.Use(recoverRequest, checkLogin(test.client))
			called := false
			engine.POST("/test", func(ctx *gin.Context) {
				called = true
				if ctx.GetInt64("operatorId") != 123 || ctx.GetString("CtxIpsUname") != "tester" {
					t.Fatal("authenticated identity was not forwarded")
				}
				ctx.Status(http.StatusNoContent)
			})
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/test", nil))
			if called != test.allow || (!test.allow && !strings.Contains(response.Body.String(), `"errNo":5002`)) {
				t.Fatalf("login result = %v, response = %s", called, response.Body.String())
			}
		})
	}
}

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
