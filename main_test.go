package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lubanAgent/luban/adapters"
	"github.com/lubanAgent/luban/agents"
	"github.com/lubanAgent/luban/server"
)

func TestHelpAndInvalidCommandsDoNotInitializeResources(t *testing.T) {
	// No conf/ directory, no credentials, no database. These commands must
	// finish before adapters.Configure, rather than failing resource startup.
	t.Chdir(t.TempDir())
	for _, test := range []struct {
		args []string
		code int
	}{
		{[]string{"--help"}, 0},
		{[]string{"run", "admissions", "--help"}, 0},
		{[]string{"unknown"}, 2},
		{[]string{"run"}, 2},
		{[]string{"run", "admissions", "--input", "{"}, 2},
	} {
		if code := run(test.args); code != test.code {
			t.Fatalf("command %v: exit=%d want=%d", test.args, code, test.code)
		}
	}
}

// Build and execute the real entry point from a fresh runtime directory. Only
// the gateway is fake; Configure, file loading, imports and CLI are production.
func TestRootBinaryRunsAllAgentsAndNewJSONAgentWithoutBusinessDB(t *testing.T) {
	root := t.TempDir()
	runtimeAssets(t, root)
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/gateway/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer smoke-key" {
			t.Error("configured company model endpoint/key not used")
		}
		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.Model == "" {
			t.Error("modelName from agent.json was not sent")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"smoke reply"},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":3}}`))
	}))
	defer gateway.Close()
	t.Setenv("LUBAN_OPENPROXY_BASE_URL", gateway.URL+"/gateway/v1")
	t.Setenv("LUBAN_OPENPROXY_KEY", "smoke-key")
	for _, key := range []string{"LUBAN_DB_ADDR", "LUBAN_DB_NAME", "LUBAN_DB_USER", "LUBAN_DB_PASSWORD"} {
		t.Setenv(key, "")
	}
	binary := filepath.Join(t.TempDir(), "luban")
	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build root: %v %s", err, output)
	}
	for _, test := range []struct{ key, input, want string }{
		{"summary", `{"userPrompt":"hello"}`, `"smoke reply"`},
		{"callsummary", `{"callId":9,"text":"hello"}`, `{"callId":9,"summary":"smoke reply"}`},
		{"added", `{"userPrompt":"hello"}`, `"smoke reply"`},
	} {
		t.Run(test.key, func(t *testing.T) {
			tracePath := filepath.Join(root, "log", "luban.log")
			if err := os.Remove(tracePath); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, binary, "run", test.key, "--input", test.input)
			command.Dir = root
			var stdout, stderr bytes.Buffer
			command.Stdout, command.Stderr = &stdout, &stderr
			if err := command.Run(); err != nil {
				t.Fatalf("root CLI failed: %v %s", err, stderr.String())
			}
			if strings.TrimSpace(stdout.String()) != test.want {
				t.Fatalf("stdout=%s", stdout.String())
			}
			traces, err := os.ReadFile(tracePath)
			if err != nil {
				t.Fatalf("read company Trace log: %v", err)
			}
			for _, field := range []string{`"msg":"luban_trace"`, `"traceEvent":`, `"runId":`, `"time":`, `"file":`, `"module":"luban"`} {
				if !bytes.Contains(traces, []byte(field)) {
					t.Errorf("company Trace log missing %s", field)
				}
			}
			if strings.Contains(stderr.String(), `"traceEvent":`) {
				t.Fatal("CLI bypassed company logging and wrote Trace to stderr")
			}
		})
	}
}

func runtimeAssets(t *testing.T, root string) {
	t.Helper()
	// Keep production Agent inputs, but never copy a developer's connections.
	// Model traffic must use the test gateway, and business databases stay off.
	for _, directory := range []string{"agents"} {
		err := fs.WalkDir(os.DirFS("."), directory, func(name string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, "/agent.json") && !strings.HasSuffix(name, "/SKILL.md") {
				return nil
			}
			raw, err := os.ReadFile(name)
			if err != nil {
				return err
			}
			destination := filepath.Join(root, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
				return err
			}
			return os.WriteFile(destination, raw, 0600)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "agents/added"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "agents/added/agent.json"), []byte(`{"flow":[{"modelName":"deepseek-chat","systemPrompt":"test"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"conf/luban.yaml": `http:
  routePrefix: /luban/ai/agent
  excludeAgents: [admissions]
backgroundAgents: []
llm:
  admissions:
    - baseUrl: ${LUBAN_OPENPROXY_BASE_URL}
      apiKey: ${LUBAN_OPENPROXY_KEY}
`,
		"conf/mount/config.yaml": `log:
  level: info
  stdout: false
  buffer:
    switch: "false"
`,
		"conf/mount/resource.yaml": "mysql: {}\n",
		"conf/mount/api.yaml":      "modelHTTP:\n  timeout: 10s\n",
	} {
		destination := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(destination, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRootHTTPAssemblyLoadsAddedAgentAndUsesInjectedHostLifecycle(t *testing.T) {
	root := t.TempDir()
	runtimeAssets(t, root)
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"HTTP reply"}}]}`))
	}))
	defer gateway.Close()
	t.Setenv("LUBAN_OPENPROXY_BASE_URL", gateway.URL+"/v1")
	t.Setenv("LUBAN_OPENPROXY_KEY", "smoke-key")
	t.Chdir(root)
	var traces bytes.Buffer
	options := server.Options{Assets: os.DirFS("."), TraceWriter: server.NewJSONTraceWriter(&traces),
		// A caller can still inject its own middleware and listener.
		SetupHTTP: func(engine *gin.Engine) error {
			engine.Use(func(ctx *gin.Context) {
				if ctx.GetHeader("X-Test-Login") != "yes" {
					ctx.AbortWithStatus(401)
					return
				}
				ctx.Next()
			})
			return nil
		},
		ServeHTTP: func(_ string, engine *gin.Engine) error {
			for _, key := range []string{"summary", "callsummary", "added"} {
				body := `{"userPrompt":"hello"}`
				if key == "callsummary" {
					body = `{"callId":1,"text":"hello"}`
				}
				for _, login := range []bool{false, true} {
					recorder := httptest.NewRecorder()
					request := httptest.NewRequest("POST", "/luban/ai/agent/"+key, strings.NewReader(body))
					if login {
						request.Header.Set("X-Test-Login", "yes")
					}
					request.Header.Set("Content-Type", "application/json")
					engine.ServeHTTP(recorder, request)
					if !login {
						if recorder.Code != 401 {
							t.Fatal("host authentication was bypassed")
						}
						continue
					}
					if recorder.Code != 200 || !strings.Contains(recorder.Body.String(), `"errNo":0`) || !strings.Contains(recorder.Body.String(), "HTTP reply") {
						t.Fatalf("%s: %d %s", key, recorder.Code, recorder.Body.String())
					}
				}
			}
			for _, route := range engine.Routes() {
				if strings.Contains(route.Path, "admissions") {
					t.Fatalf("disabled task business exposed: %s", route.Path)
				}
			}
			return nil
		},
	}
	command, err := server.ParseCommand(nil)
	if err != nil {
		t.Fatal(err)
	}
	closeHost, err := adapters.Configure(&options, command)
	if err != nil {
		t.Fatal(err)
	}
	defer closeHost()
	app, err := server.New(options)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	engine := gin.New()
	if err := app.SetupHTTP(engine); err != nil {
		t.Fatal(err)
	}
	if err := app.RegisterHTTP(engine); err != nil {
		t.Fatal(err)
	}
	if err := agents.Mount(context.Background(), engine, app); err != nil {
		t.Fatal(err)
	}
	if err := app.Serve(engine); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(traces.String(), `"traceEvent":`) || !strings.Contains(traces.String(), `"runId":`) {
		t.Fatal("HTTP Trace missing")
	}
}
