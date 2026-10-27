package server_test

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
	_ "github.com/lubanAgent/luban/agents/callsummary"
	luban "github.com/lubanAgent/luban/sdk"
	"github.com/lubanAgent/luban/server"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type overrideModel struct{}

func (*overrideModel) Name() string { return "override" }
func (*overrideModel) Invoke(_ *gin.Context, _ luban.ModelRequest) (luban.ModelResponse, error) {
	return luban.ModelResponse{Content: []luban.ModelPart{{Type: luban.PartText, Text: "injected"}}}, nil
}

type nilLogger struct{}

func (*nilLogger) Debugf(*gin.Context, string, ...any) {}
func (*nilLogger) Infof(*gin.Context, string, ...any)  {}
func (*nilLogger) Warnf(*gin.Context, string, ...any)  {}
func (*nilLogger) Errorf(*gin.Context, string, ...any) {}

func fixture(t *testing.T) fstest.MapFS {
	t.Helper()
	// 服务行为测试使用独立配置，不受开发者修改示例模型名或连接信息影响。
	// 真实 Agent 文件的加载由根工程的二进制测试覆盖。
	return fstest.MapFS{
		"conf/luban.yaml": {Data: []byte("llm:\n  demo:\n    - baseUrl: https://fake.test/v1\n      apiKey: fake-key\n")},
		"agents/summary/agent.json": {
			Data: []byte(`{"flow":[{"modelName":"deepseek-chat","systemPrompt":"test summary"}]}`),
		},
		"agents/callsummary/agent.json": {
			Data: []byte(`{"flow":[{"func":"PrepareCall"},{"modelName":"deepseek-chat","systemPrompt":"test call summary"},{"func":"BuildResult"}]}`),
		},
	}
}

type fakeHTTP struct{ requests int }

func (f *fakeHTTP) Do(r *http.Request) (*http.Response, error) {
	f.requests++
	var body struct {
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return nil, err
	}
	text := body.Messages[len(body.Messages)-1].Content
	raw, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "summary: " + text}, "finish_reason": "stop"}}})
	return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(raw))}, nil
}

func newApp(t *testing.T, assets fs.FS, modify func(*server.Options)) (*server.App, *fakeHTTP, *bytes.Buffer) {
	t.Helper()
	fake := &fakeHTTP{}
	trace := &bytes.Buffer{}
	options := server.Options{Assets: assets, HTTPDoer: fake, Stdout: trace, Stderr: io.Discard}
	if modify != nil {
		modify(&options)
	}
	app, err := server.New(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Close(); err != nil {
			t.Error(err)
		}
	})
	return app, fake, trace
}

func TestHTTPAutoBindFunctionsModelAndTrace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app, fake, trace := newApp(t, fixture(t), nil)
	engine := gin.New()
	if err := app.RegisterHTTP(engine); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", "/luban/ai/agent/callsummary", strings.NewReader(`{"callId":123,"text":"客户希望明天回电"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"summary":"summary: 客户希望明天回电"`) || !strings.Contains(response.Body.String(), `"callId":123`) {
		t.Fatalf("response %d: %s", response.Code, response.Body.String())
	}
	if fake.requests != 1 {
		t.Fatalf("model requests = %d", fake.requests)
	}
	var runID string
	for _, line := range bytes.Split(bytes.TrimSpace(trace.Bytes()), []byte("\n")) {
		var event struct {
			RunID      string `json:"runId"`
			TraceEvent struct {
				RunID string `json:"runId"`
				Type  string `json:"type"`
			} `json:"traceEvent"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatal(err)
		}
		if runID == "" {
			runID = event.RunID
		}
		if runID == "" || event.RunID != runID || event.TraceEvent.RunID != runID {
			t.Fatalf("invalid Trace: %s", line)
		}
	}
	if strings.Contains(trace.String(), "fake-key") {
		t.Fatal("API key leaked into Trace")
	}
	if err := app.RegisterHTTP(engine); err == nil {
		t.Fatal("duplicate registration should fail")
	}
}

func TestHTTPShortInputErrorsAndCustomRenderer(t *testing.T) {
	app, fake, _ := newApp(t, fixture(t), nil)
	handler, err := app.HTTPHandler("summary")
	if err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	engine.POST("/summary", handler)
	for _, test := range []struct {
		body string
		code int
	}{
		{`{"userPrompt":"hello"}`, 200},
		{`{"messages":[{"role":"user","content":[{"type":"text","text":"history"}]}]}`, 200},
		{`{"userPrompt":"hello","messages":[]}`, 400},
		{`{`, 400}, {`{}`, 400}, {`{} {}`, 400},
	} {
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest("POST", "/summary", strings.NewReader(test.body)))
		if response.Code != test.code {
			t.Errorf("%s: got %d: %s", test.body, response.Code, response.Body.String())
		}
	}
	if fake.requests != 2 {
		t.Fatalf("invalid input reached model: %d", fake.requests)
	}
	calls := 0
	custom, _, _ := newApp(t, fixture(t), func(options *server.Options) {
		options.Renderer = func(ctx *gin.Context, output any, err error) { calls++; ctx.JSON(202, gin.H{"custom": output}) }
	})
	customHandler, _ := custom.HTTPHandler("summary")
	customEngine := gin.New()
	customEngine.POST("/summary", customHandler)
	customEngine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/summary", strings.NewReader(`{"userPrompt":"hello"}`)))
	if calls != 1 {
		t.Fatalf("Renderer calls = %d", calls)
	}
}

func TestRouteConflictDoesNotPartiallyMount(t *testing.T) {
	app, _, _ := newApp(t, fixture(t), nil)
	engine := gin.New()
	engine.POST("/luban/ai/agent/summary", func(*gin.Context) {})
	before := len(engine.Routes())
	if err := app.RegisterHTTP(engine); err == nil {
		t.Fatal("expected route conflict")
	}
	if len(engine.Routes()) != before {
		t.Fatal("router modified on failed registration")
	}
}

func TestHTTPAgentCanBeExcludedForBusinessControlledEntry(t *testing.T) {
	assets := fixture(t)
	assets["agents/admissions/agent.json"] = &fstest.MapFile{Data: []byte(`{"flow":[{"modelName":"demo","systemPrompt":"admissions"}]}`)}
	app, _, _ := newApp(t, assets, func(options *server.Options) {
		options.ExcludeHTTPAgents = []string{"admissions"}
	})
	engine := gin.New()
	if err := app.RegisterHTTP(engine); err != nil {
		t.Fatal(err)
	}
	paths := make(map[string]bool)
	for _, route := range engine.Routes() {
		paths[route.Path] = true
	}
	if paths["/luban/ai/agent/admissions"] {
		t.Fatal("business-controlled admissions Agent route was auto-mounted")
	}
	if !paths["/luban/ai/agent/summary"] {
		t.Fatal("other Agent route was removed by an exclusion")
	}
}

func TestStartupErrorsDoNotExposeSecrets(t *testing.T) {
	for _, test := range []struct{ name, config string }{
		{"missing env", "llm:\n  demo:\n    - baseUrl: https://fake.test\n      apiKey: ${LUBAN_TEST_MISSING_KEY}\n"},
		{"duplicate defaults", "llm:\n  demo:\n    - baseUrl: https://fake.test\n      apiKey: secret-key\n    - baseUrl: https://fake.test\n      apiKey: secret-key\n"},
		{"unknown key", "llm:\n  demo:\n    - baseUrl: https://fake.test\n      apikey: secret-key\n"},
		{"wrong prefix", "http:\n  routePrefix: /two/parts\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			assets := fixture(t)
			assets["conf/luban.yaml"] = &fstest.MapFile{Data: []byte(test.config)}
			_, err := server.New(server.Options{Assets: assets})
			if err == nil {
				t.Fatal("expected startup error")
			}
			if strings.Contains(err.Error(), "secret-key") {
				t.Fatalf("secret leaked: %v", err)
			}
		})
	}
}

func TestInjectedModelAndDatabaseOverrideUnusedCredentials(t *testing.T) {
	assets := fixture(t)
	assets["conf/luban.yaml"] = &fstest.MapFile{Data: []byte(`llm:
  demo:
    - baseUrl: https://fake.test/v1
      apiKey: ${UNUSED_MODEL_KEY}
    - modelList: [unused-model]
      baseUrl: ${UNUSED_MODEL_URL}
      apiKey: ${UNUSED_MODEL_KEY}
databases:
  school:
    driver: mysql
    dsn: ${UNUSED_DB_DSN}
`)}
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	model := &overrideModel{}
	app, err := server.New(server.Options{
		Assets: assets, Stdout: io.Discard, Stderr: io.Discard,
		ProjectModels: map[string]map[string]luban.ModelInvoker{
			"demo": {"deepseek-chat": model},
		},
		Databases: map[string]*gorm.DB{"school": db},
	})
	if err != nil {
		t.Fatal(err)
	}
	if selected, ok := app.Database("school"); !ok || selected != db {
		t.Fatal("injected database did not override configured database")
	}
	engine := gin.New()
	if err := app.RegisterHTTP(engine); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest("POST", "/luban/ai/agent/summary", strings.NewReader(`{"userPrompt":"hello"}`)))
	if response.Code != 200 || !strings.Contains(response.Body.String(), "injected") {
		t.Fatalf("injected model response %d: %s", response.Code, response.Body.String())
	}
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}
	if err := raw.Ping(); err != nil {
		t.Fatalf("server closed host-owned database: %v", err)
	}
}

func TestTypedNilInjectionFailsAtStartup(t *testing.T) {
	var logger *nilLogger
	options := server.Options{Assets: fixture(t), Logger: logger}
	if _, err := server.New(options); err == nil {
		t.Fatal("typed-nil Logger was accepted")
	}
}

func TestAgentLocalSkillLoadsAndMissingSkillFails(t *testing.T) {
	assets := fixture(t)
	assets["agents/summary/skills/guide/SKILL.md"] = &fstest.MapFile{Data: []byte("---\ndescription: local\n---\nlocal body")}
	assets["agents/summary/agent.json"] = &fstest.MapFile{Data: []byte(`{"flow":[{"modelName":"demo","systemPrompt":"test","skills":["guide"]}]}`)}
	app, _, _ := newApp(t, assets, nil)
	if app.Runtime() == nil {
		t.Fatal("missing Runtime")
	}
	assets["agents/summary/agent.json"] = &fstest.MapFile{Data: []byte(`{"flow":[{"modelName":"demo","systemPrompt":"test","skills":["missing"]}]}`)}
	if _, err := server.New(server.Options{Assets: assets}); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("missing Skill error = %v", err)
	}
}

func TestCLIAndInjectedDBLifecycle(t *testing.T) {
	app, _, output := newApp(t, fixture(t), func(options *server.Options) { options.Databases = map[string]*gorm.DB{"school": {}} })
	handled, code := app.RunCLI([]string{"run", "callsummary", "--input", `{"callId":7,"text":"hello"}`})
	if !handled || code != 0 || !strings.Contains(output.String(), `"summary":"summary: hello"`) {
		t.Fatalf("CLI result %v %d %s", handled, code, output.String())
	}
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRegistrationSurvivesTrimpathBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("build subprocess")
	}
	binary := t.TempDir() + "/luban"
	command := exec.Command("go", "build", "-trimpath", "-o", binary, "./server/testdata/trimpath")
	command.Dir = ".."
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	assets := t.TempDir()
	for name, file := range fixture(t) {
		target := filepath.Join(assets, name)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, file.Data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// This fixture imports no company adapters. Registration is checked without
	// initializing real company databases, IPS clients or logging resources.
	command = exec.Command(binary, assets, "unknown")
	command.Dir = ".."
	output, err := command.CombinedOutput()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 2 || !strings.Contains(string(output), "Usage:") {
		t.Fatalf("trimpath startup = %v\n%s", err, output)
	}
}
