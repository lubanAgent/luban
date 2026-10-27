package luban

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
)

type resourceClient struct {
	name   string
	calls  atomic.Int64
	closed atomic.Bool
}

func (client *resourceClient) Name() string { return client.name }
func (client *resourceClient) Close() error {
	client.closed.Store(true)
	return nil
}

func TestResourceRejectsInvalidRegistration(t *testing.T) {
	for _, resources := range []map[string]any{
		{"": &resourceClient{}},
		{" ": &resourceClient{}},
		{"cache": nil},
		{"cache": (*resourceClient)(nil)},
	} {
		if _, err := New(Options{Resources: resources}); err == nil {
			t.Fatal("invalid resources accepted")
		}
	}
}

func TestResourceScopeAndBusinessErrors(t *testing.T) {
	for _, mode := range []string{"success", "stop", "error", "panic"} {
		t.Run(mode, func(t *testing.T) {
			client := &resourceClient{name: "cache"}
			var captured *gin.Context
			rt, err := New(Options{
				Config:    Config{Agents: map[string]AgentConfig{"test": {Flow: []NodeConfig{{Func: "work"}}}}},
				Resources: map[string]any{"cache": client},
			})
			if err != nil {
				t.Fatal(err)
			}
			err = rt.Register(map[string]any{"work": func(ctx *gin.Context) (string, error) {
				captured = ctx.Copy()
				got, err := Resource[*resourceClient](ctx, "cache")
				if err != nil || got != client {
					return "", fmt.Errorf("wrong resource: %v", err)
				}
				named, err := Resource[interface{ Name() string }](ctx, "cache")
				if err != nil || named.Name() != "cache" {
					return "", errors.New("resource cannot be retrieved by business interface")
				}
				for _, name := range []string{"missing", ""} {
					if _, err := Resource[any](ctx, name); err == nil {
						return "", errors.New("invalid resource lookup accepted")
					}
				}
				if _, err := Resource[string](ctx, "cache"); err == nil || !strings.Contains(err.Error(), `"cache"`) {
					return "", errors.New("resource type mismatch not reported")
				}
				switch mode {
				case "stop":
					Stop(ctx)
				case "error":
					return "", errors.New("business failed")
				case "panic":
					panic("business panicked")
				}
				return got.Name(), nil
			}}, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			ctx := &gin.Context{}
			result, err := rt.Run(ctx, "test", nil)
			failed := mode == "error" || mode == "panic"
			if (err != nil) != failed || (!failed && result != "cache") {
				t.Fatalf("result = %v, error = %v", result, err)
			}
			for _, ended := range []*gin.Context{ctx, captured, nil} {
				if _, err := Resource[*resourceClient](ended, "cache"); err == nil {
					t.Fatal("resource available outside active Run")
				}
			}
			if client.closed.Load() {
				t.Fatal("Run closed a host-owned client")
			}
		})
	}
}

func TestResourcesAreCopiedAndIsolatedAcrossRuntimes(t *testing.T) {
	makeRuntime := func(name string) *Runtime {
		t.Helper()
		resources := map[string]any{"cache": &resourceClient{name: name}}
		rt, err := New(Options{
			Config:    Config{Agents: map[string]AgentConfig{"test": {Flow: []NodeConfig{{Func: "read"}}}}},
			Resources: resources,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := rt.Register(map[string]any{"read": func(ctx *gin.Context) (string, error) {
			client, err := Resource[*resourceClient](ctx, "cache")
			if err != nil {
				return "", err
			}
			return client.Name(), nil
		}}, nil, nil); err != nil {
			t.Fatal(err)
		}
		resources["cache"] = &resourceClient{name: "replacement"}
		return rt
	}
	first, second := makeRuntime("first"), makeRuntime("second")
	var wg sync.WaitGroup
	for range 12 {
		for name, rt := range map[string]*Runtime{"first": first, "second": second} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				result, err := rt.Run(&gin.Context{}, "test", nil)
				if err != nil || result != name {
					t.Errorf("runtime %s: result=%v, error=%v", name, result, err)
				}
			}()
		}
	}
	wg.Wait()
}

func TestResourceAvailableInToolsAndConcurrentBatchItems(t *testing.T) {
	client := &resourceClient{name: "cos"}
	model := &scriptedInvoker{respond: func(request ModelRequest) (ModelResponse, error) {
		for _, message := range request.Messages {
			if message.Role == ModelRoleTool {
				return ModelResponse{Content: []ModelPart{{Type: PartText, Text: "done"}}}, nil
			}
		}
		return ModelResponse{ToolCalls: []ToolCall{{ID: "read", Name: "read", Arguments: "{}"}}}, nil
	}}
	rt, err := New(Options{
		Config: Config{Agents: map[string]AgentConfig{"test": {Flow: []NodeConfig{
			{Func: "prepare"}, {ModelName: "model", SystemPrompt: "Read a resource.", Tools: []string{"read"}}, {Func: "finish"},
		}}}},
		Models: map[string]ModelInvoker{"model": model}, Resources: map[string]any{"cos": client},
	})
	if err != nil {
		t.Fatal(err)
	}
	tool := callbackTool{name: "read", input: struct{}{}, invoke: func(ctx *gin.Context, _ any) (any, error) {
		got, err := Resource[*resourceClient](ctx, "cos")
		if err != nil {
			return nil, err
		}
		got.calls.Add(1)
		return "material", nil
	}}
	err = rt.Register(map[string]any{
		"prepare": func(ctx *gin.Context, batch bool) (any, error) {
			if _, err := Resource[*resourceClient](ctx, "cos"); err != nil {
				return nil, err
			}
			messages := []Message{{Role: RoleUser, Content: []Part{{Type: PartText, Text: "read"}}}}
			if !batch {
				return Request{Messages: messages}, nil
			}
			return Batch{Concurrency: 3, Items: []BatchItem{{Messages: messages}, {Messages: messages}, {Messages: messages}}}, nil
		},
		"finish": func(ctx *gin.Context, output any) (int64, error) {
			if batch, ok := output.(BatchResult[string]); ok {
				for _, item := range batch.Items {
					if item.Err != nil || item.Value != "done" {
						return 0, fmt.Errorf("batch item failed: %v", item.Err)
					}
				}
			} else if output != "done" {
				return 0, fmt.Errorf("unexpected output %T", output)
			}
			got, err := Resource[*resourceClient](ctx, "cos")
			if err != nil {
				return 0, err
			}
			return got.calls.Load(), nil
		},
	}, []Tool{tool}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		batch bool
		want  int64
	}{{false, 1}, {true, 4}} {
		result, err := rt.Run(&gin.Context{}, "test", test.batch)
		if err != nil || result != test.want {
			t.Fatalf("batch=%v result=%v error=%v", test.batch, result, err)
		}
	}
}
