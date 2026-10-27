package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
	_ "github.com/lubanAgent/luban/agents"
	_ "github.com/lubanAgent/luban/agents/callsummary"
	"github.com/lubanAgent/luban/models"
	luban "github.com/lubanAgent/luban/sdk"
	"github.com/lubanAgent/luban/server"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Injection tests own their connection config. They must not load company
// credentials or depend on the service's company credentials.
func injectionAssets(t *testing.T) fs.FS {
	t.Helper()
	files := fstest.MapFS{
		"conf/luban.yaml": {Data: []byte("http:\n  excludeAgents: [admissions]\nllm:\n  admissions:\n    - baseUrl: https://fake.test/v1\n      apiKey: test-key\n")},
	}
	source := os.DirFS(".")
	err := fs.WalkDir(source, "agents", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || (!strings.HasSuffix(name, "/agent.json") && !strings.HasSuffix(name, "/SKILL.md")) {
			return nil
		}
		data, err := fs.ReadFile(source, name)
		if err != nil {
			return err
		}
		files[name] = &fstest.MapFile{Data: data}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// 注入测试使用固定模型名，避免开发者修改 summary 示例后误走其他调用器。
	// 真实 Agent 配置的加载和执行由 main_test.go 的根二进制测试覆盖。
	files["agents/summary/agent.json"] = &fstest.MapFile{
		Data: []byte(`{"flow":[{"modelName":"deepseek-chat","systemPrompt":"test summary"}]}`),
	}
	return files
}

type modelHTTP struct {
	requests int
}

type injectedModel struct{ calls int }

type failingModel struct{}

func (*failingModel) Name() string { return "company-openproxy" }
func (*failingModel) Invoke(*gin.Context, luban.ModelRequest) (luban.ModelResponse, error) {
	return luban.ModelResponse{}, errors.New("company gateway failed")
}

type loggerRecorder struct{ errors int }

func (*loggerRecorder) Debugf(*gin.Context, string, ...any) {}
func (*loggerRecorder) Infof(*gin.Context, string, ...any)  {}
func (*loggerRecorder) Warnf(*gin.Context, string, ...any)  {}
func (logger *loggerRecorder) Errorf(*gin.Context, string, ...any) {
	logger.errors++
}

func (*injectedModel) Name() string { return "company-openproxy" }
func (model *injectedModel) Invoke(_ *gin.Context, request luban.ModelRequest) (luban.ModelResponse, error) {
	model.calls++
	if request.ModelName != "deepseek-chat" {
		return luban.ModelResponse{}, io.ErrUnexpectedEOF
	}
	return luban.ModelResponse{Content: []luban.ModelPart{{Type: luban.PartText, Text: "company gateway response"}}}, nil
}

func (client *modelHTTP) Do(request *http.Request) (*http.Response, error) {
	client.requests++
	var input struct {
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		return nil, err
	}
	text := "summary"
	if len(input.Messages) > 0 {
		text = "summary: " + input.Messages[len(input.Messages)-1].Content
	}
	body, _ := json.Marshal(map[string]any{
		"choices": []any{map[string]any{
			"message":       map[string]any{"content": text},
			"finish_reason": "stop",
		}},
	})
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(body)),
	}, nil
}

func TestCompanyModelInvokerOverridesLubanHTTPForItsProject(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("LUBAN_API_KEY", "test-key")
	invoker := &injectedModel{}
	httpClient := &modelHTTP{}
	app, err := server.New(server.Options{Assets: injectionAssets(t),
		HTTPDoer: httpClient,
		ProjectModels: map[string]map[string]luban.ModelInvoker{
			"admissions": {"deepseek-chat": invoker},
		},
		Stdout: io.Discard,
		Stderr: io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Close(); err != nil {
			t.Error(err)
		}
	})
	engine := gin.New()
	if err := app.RegisterHTTP(engine); err != nil {
		t.Fatal(err)
	}

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/luban/ai/agent/summary", strings.NewReader(`{"userPrompt":"hello"}`))
	request.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "company gateway response") {
		t.Fatalf("company model response %d: %s", response.Code, response.Body.String())
	}
	if invoker.calls != 1 || httpClient.requests != 0 {
		t.Fatalf("custom calls=%d built-in HTTP calls=%d", invoker.calls, httpClient.requests)
	}
}

func TestCompanyLoggerAndRendererAreUsed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("LUBAN_API_KEY", "test-key")
	logger := &loggerRecorder{}
	renderCalls := 0
	app, err := server.New(server.Options{Assets: injectionAssets(t),
		Logger: logger,
		ProjectModels: map[string]map[string]luban.ModelInvoker{
			"admissions": {"deepseek-chat": &failingModel{}},
		},
		Renderer: func(ctx *gin.Context, _ any, err error) {
			renderCalls++
			if err == nil {
				t.Error("Renderer received nil error")
			}
			ctx.JSON(http.StatusBadGateway, gin.H{"companyError": true})
		},
		Stdout: io.Discard,
		Stderr: io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Close(); err != nil {
			t.Error(err)
		}
	})
	engine := gin.New()
	if err := app.RegisterHTTP(engine); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/luban/ai/agent/summary", strings.NewReader(`{"userPrompt":"hello"}`))
	request.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusBadGateway || renderCalls != 1 || logger.errors != 1 {
		t.Fatalf("status=%d renderer=%d loggerErrors=%d body=%s", response.Code, renderCalls, logger.errors, response.Body.String())
	}
}

func TestGenericDependenciesAreInjectedAndOwnedByHost(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("LUBAN_API_KEY", "test-key")

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&models.Strategy{},
		&models.StrategyConversation{},
		&models.StrategyMessage{},
		&models.StrategyMessageTask{},
		&models.StrategyGenTask{},
		&models.AgentDraft{},
		&models.AgentArtifact{},
		&models.ConfigItem{},
	); err != nil {
		t.Fatal(err)
	}

	modelClient := &modelHTTP{}
	app, err := server.New(server.Options{Assets: injectionAssets(t),
		HTTPDoer: modelClient,
		Databases: map[string]*gorm.DB{
			"admissions": db,
		},
		ExcludeHTTPAgents: []string{"admissions"},
		Stdout:            io.Discard,
		Stderr:            io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	if err := app.RegisterHTTP(engine); err != nil {
		t.Fatal(err)
	}

	routes := make(map[string]bool)
	for _, route := range engine.Routes() {
		routes[route.Method+" "+route.Path] = true
	}
	if routes["POST /admissions/mis/strategy/conversation/message"] {
		t.Fatal("generic bootstrap must not enable admissions business routes")
	}
	for _, old := range []string{
		"POST /admissions/mis/strategy/create",
		"POST /admissions/mis/strategy/conversation/confirm",
		"GET /admissions/mis/strategy/conversation/message/result",
		"GET /admissions/mis/strategy/task/progress",
	} {
		if routes[old] {
			t.Fatalf("old admissions route still exposed: %s", old)
		}
	}
	if routes["POST /luban/ai/agent/admissions"] {
		t.Fatal("trusted admissions task Agent must not have an automatic public HTTP route")
	}

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/luban/ai/agent/summary", strings.NewReader(`{"userPrompt":"部署和联调"}`))
	request.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "summary: 部署和联调") {
		t.Fatalf("summary response %d: %s", response.Code, response.Body.String())
	}
	if modelClient.requests != 1 {
		t.Fatalf("model HTTP requests = %d, want 1", modelClient.requests)
	}

	if err := app.Close(); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Ping(); err != nil {
		t.Fatalf("injected company database was closed by Luban: %v", err)
	}
	_ = sqlDB.Close()
}

func TestDatabaseInjectionDoesNotImplicitlyEnableBusinessRoutes(t *testing.T) {
	t.Setenv("LUBAN_API_KEY", "test-key")
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	app, err := server.New(server.Options{Assets: injectionAssets(t),
		Databases: map[string]*gorm.DB{"admissions": db},
		Stdout:    io.Discard,
		Stderr:    io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = app.Close()
		if raw, err := db.DB(); err == nil {
			_ = raw.Close()
		}
	})
	engine := gin.New()
	if err := app.RegisterHTTP(engine); err != nil {
		t.Fatal(err)
	}
	for _, route := range engine.Routes() {
		if strings.HasPrefix(route.Path, "/admissions/mis/strategy/") {
			t.Fatalf("database-only injection exposed business route %s", route.Path)
		}
	}
}

func TestAdmissionsRoutesRemainAbsentWithoutAdmissionsDatabase(t *testing.T) {
	t.Setenv("LUBAN_API_KEY", "test-key")
	app, err := server.New(server.Options{Assets: injectionAssets(t),
		Stdout: io.Discard,
		Stderr: io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Close(); err != nil {
			t.Error(err)
		}
	})

	engine := gin.New()
	if err := app.RegisterHTTP(engine); err != nil {
		t.Fatal(err)
	}
	for _, route := range engine.Routes() {
		if strings.HasPrefix(route.Path, "/admissions/") {
			t.Fatalf("unexpected admissions route without injected DB: %s %s", route.Method, route.Path)
		}
	}
}
