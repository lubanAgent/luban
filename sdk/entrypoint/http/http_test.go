package lubanhttp

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestSubmitHandlerReturnsTaskWithoutRunningRuntime(t *testing.T) {
	gin.SetMode(gin.TestMode)
	type command struct {
		Action string `json:"action"`
	}
	submissions := 0
	render := func(ctx *gin.Context, output any, err error) {
		if err != nil {
			ctx.String(http.StatusBadRequest, "invalid")
			return
		}
		ctx.JSON(http.StatusOK, output)
	}
	engine := gin.New()
	engine.POST("/submit", SubmitHandler(func(_ *gin.Context, input command) (map[string]any, error) {
		submissions++
		if input.Action != "confirm" {
			return nil, errors.New("unexpected action")
		}
		return map[string]any{"taskId": 42}, nil
	}, render))
	for _, test := range []struct {
		body string
		code int
	}{
		{body: `{"action":"confirm"}`, code: http.StatusOK},
		{body: `{`, code: http.StatusBadRequest},
		{body: `{"action":"invalid"}`, code: http.StatusBadRequest},
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/submit", strings.NewReader(test.body))
		request.Header.Set("Content-Type", "application/json")
		engine.ServeHTTP(recorder, request)
		if recorder.Code != test.code {
			t.Fatalf("body=%q status=%d response=%s", test.body, recorder.Code, recorder.Body.String())
		}
		if test.code == http.StatusOK && !strings.Contains(recorder.Body.String(), `"taskId":42`) {
			t.Fatalf("missing task id in %s", recorder.Body.String())
		}
	}
	if submissions != 2 {
		t.Fatalf("invalid JSON unexpectedly invoked submit: calls=%d", submissions)
	}
}

func TestSubmitHandlerRejectsMissingCallbacks(t *testing.T) {
	cases := []func(){
		func() { SubmitHandler[struct{}, struct{}](nil, func(*gin.Context, any, error) {}) },
		func() { SubmitHandler(func(*gin.Context, struct{}) (struct{}, error) { return struct{}{}, nil }, nil) },
	}
	for _, run := range cases {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatal("expected construction error")
				}
			}()
			run()
		}()
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

func TestHandlerBindsTypedInputAndRetainsBodyForGinOnlyFunctions(t *testing.T) {
	type input struct {
		Text string `json:"text"`
	}
	rt, err := luban.New(luban.Options{Config: luban.Config{Agents: map[string]luban.AgentConfig{
		"typed": {Flow: []luban.NodeConfig{{Func: "Typed"}}}, "gin": {Flow: []luban.NodeConfig{{Func: "ReadBody"}}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	if err := rt.Register(map[string]any{
		"Typed": func(_ *gin.Context, in input) string { calls++; return in.Text },
		"ReadBody": func(ctx *gin.Context) (string, error) {
			var in input
			err := ctx.ShouldBindJSON(&in)
			return in.Text, err
		},
	}, nil, nil); err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	render := func(ctx *gin.Context, out any, err error) {
		if err != nil {
			var invalid InputError
			if !errors.As(err, &invalid) {
				t.Errorf("input error not classified: %v", err)
			}
			ctx.Status(400)
			return
		}
		ctx.String(200, "%v", out)
	}
	for _, key := range []string{"typed", "gin"} {
		engine.POST("/"+key, Handler(rt, key, render))
	}
	for _, test := range []struct {
		key, body string
		status    int
	}{
		{"typed", `{"text":"hello"}`, 200}, {"gin", `{"text":"hello"}`, 200},
		{"typed", `{`, 400}, {"typed", `null`, 400}, {"typed", `{} {}`, 400},
		{"typed", strings.Repeat("x", (16<<20)+1), 400},
	} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest("POST", "/"+test.key, strings.NewReader(test.body))
		request.Header.Set("Content-Type", "application/json")
		engine.ServeHTTP(response, request)
		if response.Code != test.status || (test.status == 200 && response.Body.String() != "hello") {
			t.Fatalf("%s: status=%d body=%s", test.key, response.Code, response.Body.String())
		}
	}
	if calls != 1 {
		t.Fatalf("invalid input executed function: %d", calls)
	}
}

func TestHandlerModelFirstInputMatchesRuntimeDecoder(t *testing.T) {
	// Reuse the real SDK protocol layer; only the network is replaced.
	rt, err := luban.New(luban.Options{Config: luban.Config{Agents: map[string]luban.AgentConfig{"model": {Flow: []luban.NodeConfig{{ModelName: "m", SystemPrompt: "test"}}}}}, Models: map[string]luban.ModelInvoker{"m": httpInputModel{t: t}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Register(nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	engine.POST("/model", Handler(rt, "model", func(ctx *gin.Context, out any, err error) {
		if err != nil {
			var invalid InputError
			if !errors.As(err, &invalid) {
				t.Errorf("unexpected Run error: %v", err)
			}
			ctx.Status(400)
			return
		}
		ctx.String(200, "%v", out)
	}))
	for _, test := range []struct {
		body   string
		status int
	}{
		{`{"userPrompt":"hello","images":["https://example.com/a.png"],"videos":["https://example.com/a.mp4"]}`, 200},
		{`{"messages":[{"role":"user","content":[{"type":"text","text":"hello"},{"type":"image","url":"https://example.com/a.png"},{"type":"video","url":"https://example.com/a.mp4"}]}]}`, 200},
		{`{"messages":[],"userPrompt":"hello"}`, 400}, {`{}`, 400},
	} {
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, httptest.NewRequest("POST", "/model", strings.NewReader(test.body)))
		if recorder.Code != test.status {
			t.Fatalf("input=%s status=%d", test.body, recorder.Code)
		}
	}
}

type httpInputModel struct{ t *testing.T }

func (httpInputModel) Name() string { return "m" }
func (m httpInputModel) Invoke(_ *gin.Context, input luban.ModelRequest) (luban.ModelResponse, error) {
	parts := input.Messages[len(input.Messages)-1].Content
	if len(parts) != 3 || parts[0].Text != "hello" || parts[1].Type != luban.PartImage || parts[2].Type != luban.PartVideo {
		m.t.Fatalf("media binding lost: %+v", parts)
	}
	return luban.ModelResponse{Content: []luban.ModelPart{{Type: luban.PartText, Text: "ok"}}}, nil
}
