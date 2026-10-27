package server_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	luban "github.com/lubanAgent/luban/sdk"
	"github.com/lubanAgent/luban/server"
)

type hostResource struct{ closed bool }

func (resource *hostResource) Close() error { resource.closed = true; return nil }

type resourceModel struct{ expected *hostResource }

func (*resourceModel) Name() string { return "resource-model" }
func (model *resourceModel) Invoke(ctx *gin.Context, _ luban.ModelRequest) (luban.ModelResponse, error) {
	resource, err := luban.Resource[*hostResource](ctx, "business")
	if err != nil {
		return luban.ModelResponse{}, err
	}
	if resource != model.expected || resource.closed {
		return luban.ModelResponse{}, errors.New("wrong or prematurely closed host resource")
	}
	return luban.ModelResponse{Content: []luban.ModelPart{{Type: luban.PartText, Text: "host resource available"}}}, nil
}

func TestServerForwardsResourcesWithoutOwningThem(t *testing.T) {
	resource := &hostResource{}
	resources := map[string]any{"business": resource}
	app, _, _ := newApp(t, fixture(t), func(options *server.Options) {
		options.Resources = resources
		options.ProjectDefaults = map[string]luban.ModelInvoker{"demo": &resourceModel{expected: resource}}
	})
	resources["business"] = &hostResource{}
	result, err := app.Runtime().Run(&gin.Context{}, "summary", luban.Request{
		Messages: []luban.Message{{Role: luban.RoleUser, Content: []luban.Part{{Type: luban.PartText, Text: "hello"}}}},
	})
	if err != nil || result != "host resource available" {
		t.Fatalf("server resource lookup: result=%v error=%v", result, err)
	}
	if err := app.Close(); err != nil || resource.closed {
		t.Fatal("server closed a business-owned resource")
	}
}

func TestDefaultHTTPSetupAndInjectedLifecycle(t *testing.T) {
	app, _, _ := newApp(t, fixture(t), nil)
	engine := gin.New()
	if err := app.SetupHTTP(engine); err != nil {
		t.Fatal(err)
	}
	if err := app.SetupHTTP(engine); err == nil {
		t.Fatal("duplicate HTTP setup accepted")
	}
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if response.Code != 200 || response.Body.String() != "success" {
		t.Fatalf("default readiness: %d %s", response.Code, response.Body.String())
	}
	engine.GET("/panic", func(*gin.Context) { panic("test") })
	response = httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/panic", nil))
	if response.Code != 500 {
		t.Fatal("default HTTP recovery is missing")
	}

	setupErr, listenErr := errors.New("setup failed"), errors.New("listen failed")
	var seenEngine *gin.Engine
	var seenAddress string
	host, _, _ := newApp(t, fixture(t), func(options *server.Options) {
		options.SetupHTTP = func(engine *gin.Engine) error {
			seenEngine = engine
			return setupErr
		}
		options.ServeHTTP = func(address string, engine *gin.Engine) error {
			seenAddress = address
			if engine != seenEngine {
				t.Fatal("host listener got a different router")
			}
			return listenErr
		}
	})
	engine = gin.New()
	if err := host.SetupHTTP(engine); !errors.Is(err, setupErr) || len(engine.Routes()) != 0 {
		t.Fatal("injected setup did not replace default setup")
	}
	if err := host.Serve(engine); !errors.Is(err, listenErr) || seenAddress != host.Address() {
		t.Fatalf("injected listener error=%v address=%q", err, seenAddress)
	}
	if host.SetupHTTP(nil) == nil || host.Serve(nil) == nil {
		t.Fatal("nil router accepted")
	}
	registered, _, _ := newApp(t, fixture(t), nil)
	engine = gin.New()
	if err := registered.RegisterHTTP(engine); err != nil {
		t.Fatal(err)
	}
	if err := registered.SetupHTTP(engine); err == nil {
		t.Fatal("HTTP setup accepted after Agent routes")
	}
}

func TestCommandParsingNeedsNoApp(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"help"}, {"run", "--help"}, {"run", "summary", "-h"}} {
		command, err := server.ParseCommand(args)
		if err != nil || !command.Help || command.IsHTTP() {
			t.Fatalf("help %v: command=%+v error=%v", args, command, err)
		}
	}
	command, err := server.ParseCommand(nil)
	if err != nil || !command.IsHTTP() {
		t.Fatal("empty command must start HTTP")
	}
	command, err = server.ParseCommand([]string{"run", "summary", "--input", `{"userPrompt":"hello"}`})
	if err != nil || command.IsHTTP() || command.Help {
		t.Fatalf("run parse: %+v error=%v", command, err)
	}
	for _, args := range [][]string{
		{"unknown"}, {"run"}, {"run", ""}, {"run", "--unknown"},
		{"run", "summary", "--input", "{"}, {"run", "summary", "--unknown"},
		{"run", "summary", "unexpected"}, {"--help", "unexpected"},
	} {
		if _, err := server.ParseCommand(args); err == nil {
			t.Fatalf("invalid command accepted: %v", args)
		}
	}
}
