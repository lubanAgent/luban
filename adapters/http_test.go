package adapters

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"git.yukework.com/pkg/golib/v2/env"
	serverhttp "git.yukework.com/pkg/golib/v2/server/http"
	ips "git.yukework.com/saas/ips-client/v2"
	"github.com/gin-gonic/gin"
	"github.com/lubanAgent/luban/conf"
)

type sessionCallback func(*gin.Context) (ips.IpsSession, error)

func (get sessionCallback) GetSession(ctx *gin.Context) (ips.IpsSession, error) { return get(ctx) }

func TestCompanyHTTPBootstrapKeepsInfrastructureOutsideLogin(t *testing.T) {
	if env.GetRunEnv() != env.RunEnvTest {
		t.Skip("golib starts a profiling listener in non-test environments")
	}
	// One real golib bootstrap per test process: its runtime collectors are
	// process globals. Pprof is disabled; all requests use httptest, no listener.
	oldMode := gin.Mode()
	t.Cleanup(func() { gin.SetMode(oldMode) })
	loginCalls := 0
	client := sessionCallback(func(ctx *gin.Context) (ips.IpsSession, error) {
		loginCalls++
		if ctx.GetHeader("X-Test-Login") != "yes" {
			return ips.IpsSession{}, nil
		}
		return ips.IpsSession{Uid: 123, Uname: "tester"}, nil
	})
	engine := gin.New()
	if err := companyHTTPSetup(&conf.Company{}, client)(engine); err != nil {
		t.Fatal(err)
	}
	engine.POST("/business", func(ctx *gin.Context) {
		ctx.String(200, "%d|%s", ctx.GetInt64("operatorId"), ctx.ClientIP())
	})
	for _, path := range []string{"/ready", "/runtime-metrics"} {
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK || loginCalls != 0 {
			t.Fatalf("infrastructure %s: code=%d login calls=%d", path, response.Code, loginCalls)
		}
	}
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/business", nil))
	if !strings.Contains(response.Body.String(), `"errNo":5002`) || loginCalls != 1 {
		t.Fatal("business route bypassed login")
	}
	request := httptest.NewRequest(http.MethodPost, "/business", nil)
	request.Header.Set("X-Test-Login", "yes")
	request.Header.Set("X-Original-Forwarded-For", "203.0.113.7")
	response = httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Body.String() != "123|203.0.113.7" {
		t.Fatalf("identity or company proxy IP handling missing: %s", response.Body.String())
	}
}

func TestCompanyListenerReceivesCompanyConfiguration(t *testing.T) {
	listenErr := errors.New("listener test failure")
	for _, address := range []string{"", ":8099"} {
		engine := gin.New()
		config := serverhttp.ServerConfig{
			Address: address, ReadTimeout: 31 * time.Second,
			WriteTimeout: 121 * time.Second, GracefulTimeout: 17 * time.Second,
			AliasPath: map[string][]string{"/real": {"/alias"}},
		}
		called := false
		serve := companyHTTPServer(config, func(got *gin.Engine, gotConfig serverhttp.ServerConfig) error {
			called = true
			wantAddress := address
			if wantAddress == "" {
				wantAddress = ":8088"
			}
			if got != engine || gotConfig.Address != wantAddress ||
				gotConfig.ReadTimeout != config.ReadTimeout || gotConfig.WriteTimeout != config.WriteTimeout ||
				gotConfig.GracefulTimeout != config.GracefulTimeout || gotConfig.AliasPath["/real"][0] != "/alias" {
				t.Fatal("company HTTP settings were not preserved")
			}
			select {
			case gotConfig.CloseChan <- struct{}{}:
			default:
				t.Fatal("golib shutdown notification would block")
			}
			return listenErr
		})
		if err := serve(":8088", engine); !errors.Is(err, listenErr) || !called {
			t.Fatalf("listener error = %v, called=%v", err, called)
		}
	}
}
