package lubanhttp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	luban "github.com/lubanAgent/luban/sdk"
)

func TestHandlerRunsRuntimeAndRendersResult(t *testing.T) {
	rt, err := luban.New(luban.Options{Config: luban.Config{Agents: map[string]luban.AgentConfig{
		"course": {Flow: []luban.NodeConfig{{Func: "Handle"}}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Register(map[string]any{
		"Handle": func(*gin.Context) string { return "reviewed" },
	}, nil, nil); err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	rendered := false
	engine.POST("/review", Handler(rt, "course", func(ctx *gin.Context, output any, err error) {
		rendered = true
		if err != nil || output != "reviewed" {
			t.Errorf("output=%#v err=%v", output, err)
		}
		ctx.String(http.StatusOK, "ok")
	}))
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/review", nil))
	if !rendered || recorder.Code != http.StatusOK || recorder.Body.String() != "ok" {
		t.Fatalf("rendered=%t status=%d body=%q", rendered, recorder.Code, recorder.Body.String())
	}
}

func TestHandlerConstructorRejectsInvalidArguments(t *testing.T) {
	valid, err := luban.New(luban.Options{})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		run  func()
	}{
		{name: "nil Runtime", run: func() { Handler(nil, "app", func(*gin.Context, any, error) {}) }},
		{name: "empty appKey", run: func() { Handler(valid, "", func(*gin.Context, any, error) {}) }},
		{name: "nil Renderer", run: func() { Handler(valid, "app", nil) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("expected constructor panic")
				}
			}()
			test.run()
		})
	}
}
