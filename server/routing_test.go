package server_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
	luban "github.com/lubanAgent/luban/sdk"
	"github.com/lubanAgent/luban/server"
)

type routeHTTP func(*http.Request) (*http.Response, error)

func (fn routeHTTP) Do(r *http.Request) (*http.Response, error) { return fn(r) }

type routeModel struct {
	value string
	calls int
}

func (m *routeModel) Name() string { return m.value }
func (m *routeModel) Invoke(*gin.Context, luban.ModelRequest) (luban.ModelResponse, error) {
	m.calls++
	return luban.ModelResponse{Content: []luban.ModelPart{{Type: luban.PartText, Text: m.value}}}, nil
}

func TestConfiguredModelListWinsOverInjectedProjectDefault(t *testing.T) {
	t.Setenv("UNUSED_ROUTE_KEY", "")
	assets := fstest.MapFS{"conf/luban.yaml": {Data: []byte(`llm:
  demo:
    - baseUrl: https://default.invalid/v1
      apiKey: ${UNUSED_ROUTE_KEY}
    - modelList: [special]
      baseUrl: https://special.invalid/custom/v1
      apiKey: special-key
    - modelList: [override]
      baseUrl: https://overridden.invalid/v1
      apiKey: ${UNUSED_ROUTE_KEY}
    - modelList: [unused]
      baseUrl: https://unused.invalid/v1
      apiKey: ${UNUSED_ROUTE_KEY}
`)}}
	for _, model := range []string{"ordinary", "special", "override", "callsummary"} {
		key := model
		if model == "callsummary" {
			model = "ordinary"
		}
		assets["agents/"+key+"/agent.json"] = &fstest.MapFile{Data: []byte(`{"flow":[{"modelName":"` + model + `","systemPrompt":"test"}]}`)}
	}
	fallback, explicit := &routeModel{value: "DEFAULT"}, &routeModel{value: "INJECTED"}
	requests := 0
	app, err := server.New(server.Options{Assets: assets, ProjectDefaults: map[string]luban.ModelInvoker{"demo": fallback}, ProjectModels: map[string]map[string]luban.ModelInvoker{"demo": {"override": explicit}}, Stdout: io.Discard, Stderr: io.Discard,
		HTTPDoer: routeHTTP(func(r *http.Request) (*http.Response, error) {
			requests++
			if r.URL.String() != "https://special.invalid/custom/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer special-key" {
				t.Fatal("model-specific connection lost")
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"SPECIAL"}}]}`))}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	for key, want := range map[string]string{"ordinary": "DEFAULT", "special": "SPECIAL", "override": "INJECTED"} {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		got, err := app.Runtime().Run(ctx, key, luban.Request{Messages: []luban.Message{{Role: luban.RoleUser, Content: []luban.Part{{Type: luban.PartText, Text: "hello"}}}}})
		if err != nil || got != want {
			t.Fatalf("%s = %v %v, want %s", key, got, err, want)
		}
	}
	if requests != 1 || fallback.calls != 1 || explicit.calls != 1 {
		t.Fatalf("route calls HTTP=%d default=%d injected=%d", requests, fallback.calls, explicit.calls)
	}
}
