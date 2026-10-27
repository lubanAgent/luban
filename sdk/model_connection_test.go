package luban

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(r *http.Request) (*http.Response, error) { return f(r) }

func TestProjectModelInvokerCanUseExistingHostGateway(t *testing.T) {
	hostGateway := &scriptedInvoker{responses: []ModelResponse{{
		Content: []ModelPart{{Type: PartText, Text: "company gateway response"}},
	}}}
	rt, err := New(Options{
		Config: Config{Agents: map[string]AgentConfig{
			"admissions": {
				ProjectKey: "company-admissions",
				Flow: []NodeConfig{{
					ModelName:    "qwen3.7-plus",
					SystemPrompt: "Answer briefly.",
				}},
			},
		}},
		ProjectModels: map[string]map[string]ModelInvoker{
			"company-admissions": {"qwen3.7-plus": hostGateway},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Register(nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := rt.Validate(); err != nil {
		t.Fatal(err)
	}

	result, err := rt.Run(newGinContextForAgentTest(t), "admissions", Request{
		Messages: []Message{{Role: RoleUser, Content: []Part{{Type: PartText, Text: "hello"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result != "company gateway response" {
		t.Fatalf("Run result = %#v", result)
	}
	requests := hostGateway.snapshot()
	if len(requests) != 1 || requests[0].ModelName != "qwen3.7-plus" {
		t.Fatalf("host gateway requests = %+v", requests)
	}
}

func TestServiceModelClientsOverrideConfiguredConnections(t *testing.T) {
	named := &scriptedInvoker{}
	fallback := &scriptedInvoker{}
	rt, err := New(Options{
		Projects: map[string][]ModelConnection{
			"project": {
				{BaseURL: "https://default.example/v1", APIKey: "default-key"},
				{ModelList: []string{"special"}, BaseURL: "https://special.example/v1", APIKey: "special-key"},
			},
		},
		ProjectModels:   map[string]map[string]ModelInvoker{"project": {"special": named}},
		ProjectDefaults: map[string]ModelInvoker{"project": fallback},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		want ModelInvoker
	}{{"special", named}, {"ordinary", fallback}} {
		got, err := rt.modelFor("project", test.name)
		if err != nil || got != test.want {
			t.Fatalf("modelFor(%q) = %T, %v", test.name, got, err)
		}
	}
	if got := rt.projectKeys(); len(got) != 1 || got[0] != "project" {
		t.Fatalf("project keys = %v", got)
	}
}

func TestProjectModelRoutingIsolatesConcurrentCredentials(t *testing.T) {
	var mu sync.Mutex
	seen := make(map[string]int)
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		key := r.Header.Get("Authorization")
		mu.Lock()
		seen[r.URL.Path+"|"+body.Model+"|"+key]++
		mu.Unlock()
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":11,"completion_tokens":3}}`)
	}))
	defer gateway.Close()
	projects := map[string][]ModelConnection{
		"school":  {{BaseURL: gateway.URL + "/school", APIKey: "school-key"}, {ModelList: []string{"special"}, BaseURL: gateway.URL + "/special", APIKey: "special-key"}},
		"trustee": {{BaseURL: gateway.URL + "/trustee", APIKey: "trustee-key"}},
	}
	rt, err := New(Options{Projects: projects})
	if err != nil {
		t.Fatal(err)
	}
	// Mutating the source after construction must not redirect a request.
	projects["school"][0].APIKey = "changed"
	var workers sync.WaitGroup
	for i := 0; i < 12; i++ {
		for _, target := range []struct{ project, model string }{{"school", "same"}, {"trustee", "same"}, {"school", "special"}} {
			workers.Add(1)
			go func(project, model string) {
				defer workers.Done()
				invoker, err := rt.modelFor(project, model)
				if err != nil {
					t.Error(err)
					return
				}
				ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
				response, err := invoker.Invoke(ctx, ModelRequest{ModelName: model, Messages: []ModelMessage{{Role: ModelRoleUser, Content: []ModelPart{{Type: PartText, Text: "hello"}}}}})
				if err != nil {
					t.Error(err)
					return
				}
				if response.Usage == nil || response.Usage.InputTokens != 11 {
					t.Errorf("usage = %+v", response.Usage)
				}
			}(target.project, target.model)
		}
	}
	workers.Wait()
	for _, key := range []string{"/school/chat/completions|same|Bearer school-key", "/trustee/chat/completions|same|Bearer trustee-key", "/special/chat/completions|special|Bearer special-key"} {
		if seen[key] != 12 {
			t.Errorf("request %s count = %d", key, seen[key])
		}
	}
	if len(seen) != 3 {
		t.Fatalf("unexpected credential routing: %+v", seen)
	}
	if _, err := rt.modelFor("", "same"); err == nil {
		t.Fatal("ambiguous project must fail")
	}
	if _, err := rt.modelFor("missing", "same"); err == nil {
		t.Fatal("unknown project must fail")
	}
}

func TestAnthropicRoundTripPreservesToolsSystemAndUsage(t *testing.T) {
	var got map[string]any
	doer := doerFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/messages" || r.Header.Get("x-api-key") != "secret" || r.Header.Get("anthropic-version") == "" {
			t.Errorf("unexpected request: %s", r.URL.Path)
		}
		if _, ok := GinContext(r.Context()); !ok {
			t.Error("HTTPDoer lost Gin context")
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"type":"message","content":[{"type":"text","text":"thinking"},{"type":"tool_use","id":"a","name":"query","input":{"id":1}},{"type":"tool_use","id":"b","name":"query","input":{"id":2}}],"stop_reason":"tool_use","usage":{"input_tokens":10,"cache_read_input_tokens":20,"cache_creation_input_tokens":30,"output_tokens":4}}`))}, nil
	})
	rt, err := New(Options{Projects: map[string][]ModelConnection{"project": {{BaseURL: "https://example.test/v1", APIKey: "secret", Protocol: "anthropic"}}}, HTTPDoer: doer})
	if err != nil {
		t.Fatal(err)
	}
	invoker, _ := rt.modelFor("", "claude")
	response, err := invoker.Invoke(newGinContextForAgentTest(t), ModelRequest{
		ModelName: "claude", Tools: []ToolDefinition{{Name: "query", Parameters: json.RawMessage(`{"type":"object"}`)}},
		Messages: []ModelMessage{
			{Role: ModelRoleSystem, Content: []ModelPart{{Type: PartText, Text: "system"}}},
			{Role: ModelRoleUser, Content: []ModelPart{{Type: PartImage, URL: "https://example.test/a.png"}}},
			{Role: ModelRoleAssistant, ToolCalls: []ToolCall{{ID: "one", Name: "query", Arguments: `{}`}, {ID: "two", Name: "query", Arguments: `{}`}}},
			{Role: ModelRoleTool, ToolCallID: "one", Content: []ModelPart{{Type: PartText, Text: "first"}}},
			{Role: ModelRoleTool, ToolCallID: "two", Content: []ModelPart{{Type: PartText, Text: "second"}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.ToolCalls) != 2 || response.ToolCalls[1].ID != "b" || response.Usage.InputTokens != 60 {
		t.Fatalf("response = %+v", response)
	}
	if got["max_tokens"] != float64(4096) || len(got["system"].([]any)) != 1 {
		t.Fatalf("payload = %+v", got)
	}
	messages := got["messages"].([]any)
	if len(messages) != 3 || len(messages[2].(map[string]any)["content"].([]any)) != 2 {
		t.Fatalf("tool results were not merged: %+v", messages)
	}
}

func TestOpenAIWirePreservesMediaAndParallelToolCalls(t *testing.T) {
	doer := doerFunc(func(r *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["model"] != "real-model" || body["tool_choice"] != "auto" || len(body["tools"].([]any)) != 1 {
			t.Fatalf("payload = %+v", body)
		}
		messages := body["messages"].([]any)
		parts := messages[0].(map[string]any)["content"].([]any)
		if len(parts) != 3 || parts[1].(map[string]any)["type"] != "image_url" || parts[2].(map[string]any)["type"] != "video_url" {
			t.Fatalf("media = %+v", parts)
		}
		if messages[2].(map[string]any)["tool_call_id"] != "old" {
			t.Fatalf("tool linkage = %+v", messages)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":null,"tool_calls":[{"id":"one","type":"function","function":{"name":"query","arguments":"{}"}},{"id":"two","type":"function","function":{"name":"query","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`))}, nil
	})
	rt, err := New(Options{Projects: map[string][]ModelConnection{"project": {{BaseURL: "https://example.test/v1", APIKey: "secret"}}}, HTTPDoer: doer})
	if err != nil {
		t.Fatal(err)
	}
	invoker, _ := rt.modelFor("", "real-model")
	response, err := invoker.Invoke(newGinContextForAgentTest(t), ModelRequest{ModelName: "real-model", Tools: []ToolDefinition{{Name: "query"}}, Messages: []ModelMessage{
		{Role: ModelRoleUser, Content: []ModelPart{{Type: PartText, Text: "hello"}, {Type: PartImage, URL: "https://a.test/image"}, {Type: PartVideo, URL: "https://a.test/video"}}},
		{Role: ModelRoleAssistant, ToolCalls: []ToolCall{{ID: "old", Name: "query", Arguments: `{}`}}},
		{Role: ModelRoleTool, ToolCallID: "old", Content: []ModelPart{{Type: PartText, Text: "result"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.ToolCalls) != 2 || response.Usage != nil {
		t.Fatalf("response = %+v", response)
	}
}

func TestModelFailuresAreRedactedAndPreserveErrorChain(t *testing.T) {
	cause := errors.New("authorization: secret-key")
	rt, err := New(Options{Projects: map[string][]ModelConnection{"p": {{BaseURL: "https://example.test", APIKey: "secret-key"}}}, HTTPDoer: doerFunc(func(*http.Request) (*http.Response, error) { return nil, cause })})
	if err != nil {
		t.Fatal(err)
	}
	invoker, _ := rt.modelFor("p", "m")
	_, err = invoker.Invoke(newGinContextForAgentTest(t), ModelRequest{ModelName: "m"})
	if err == nil || strings.Contains(err.Error(), "secret-key") || !errors.Is(err, cause) {
		t.Fatalf("error = %v", err)
	}
	for _, protocol := range []string{"openai", "anthropic"} {
		model := &protocolModel{connection: ModelConnection{Protocol: protocol}}
		_, err := model.Invoke(newGinContextForAgentTest(t), ModelRequest{Messages: []ModelMessage{{Role: ModelRoleUser, Content: []ModelPart{{Type: PartAudio, URL: "https://a.test/a.wav"}}}}})
		if err == nil || !strings.Contains(err.Error(), "unsupported") {
			t.Fatalf("%s audio error = %v", protocol, err)
		}
	}
	parent, cancel := context.WithCancel(context.Background())
	cancel()
	ctx := newGinContextForAgentTest(t)
	ctx.Request = ctx.Request.WithContext(parent)
	_, err = invoker.Invoke(ctx, ModelRequest{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestDBScopeSharesInjectedConnectionAndExpires(t *testing.T) {
	db := &gorm.DB{}
	var retained *gin.Context
	rt, err := New(Options{Config: Config{Agents: map[string]AgentConfig{"a": {Flow: []NodeConfig{{Func: "read"}}}}}, Databases: map[string]*gorm.DB{"school": db}})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Register(map[string]any{"read": func(ctx *gin.Context) error {
		retained = ctx.Copy()
		got, err := DB(ctx, "school")
		if got != db {
			return errors.New("wrong connection")
		}
		return err
	}}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Run(newGinContextForAgentTest(t), "a", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := DB(retained, "school"); err == nil {
		t.Fatal("retained context exposed DB after Run")
	}
}
