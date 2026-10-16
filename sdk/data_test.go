package luban

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type callbackTool struct {
	name   string
	input  any
	invoke func(*gin.Context, any) (any, error)
}

func (t callbackTool) Name() string        { return t.name }
func (t callbackTool) Description() string { return "Exercise callback behavior." }
func (t callbackTool) Input() any          { return t.input }
func (t callbackTool) Invoke(ctx *gin.Context, input any) (any, error) {
	return t.invoke(ctx, input)
}

type callbackModel func(*gin.Context, ModelRequest) (ModelResponse, error)

func (callbackModel) Name() string { return "callback" }
func (m callbackModel) Invoke(ctx *gin.Context, request ModelRequest) (ModelResponse, error) {
	return m(ctx, request)
}

type callbackTraceWriter func(*gin.Context, TraceEvent)

func (w callbackTraceWriter) WriteTrace(ctx *gin.Context, event TraceEvent) { w(ctx, event) }

type stoppingSkillBackend struct{ recoverInside bool }

func (stoppingSkillBackend) List(*gin.Context) ([]SkillInfo, error) {
	return []SkillInfo{{Name: "guide", Description: "Task guidance."}}, nil
}

func (b stoppingSkillBackend) Get(ctx *gin.Context, _ string) (Skill, error) {
	if b.recoverInside {
		defer func() { _ = recover() }()
	}
	Stop(ctx)
	return Skill{Name: "guide", Description: "Task guidance.", Content: "Follow the rules."}, nil
}

func TestSharedDataLifecycle(t *testing.T) {
	for _, mode := range []string{"success", "stop", "error", "panic"} {
		t.Run(mode, func(t *testing.T) {
			var captured *gin.Context
			rt := newBusinessRuntime(t, []NodeConfig{{Func: "work"}}, map[string]any{
				"work": func(ctx *gin.Context) (string, error) {
					if value, ok := Get(ctx, "missing"); ok || value != nil {
						return "", errors.New("unexpected missing value")
					}
					if err := Set(ctx, "nil", nil); err != nil {
						return "", err
					}
					if value, ok := Get(ctx, "nil"); !ok || value != nil {
						return "", errors.New("stored nil lost its presence")
					}
					if err := Set(ctx, "id", 42); err != nil {
						return "", err
					}
					captured = ctx.Copy()
					switch mode {
					case "stop":
						Stop(ctx)
					case "error":
						return "partial", errors.New("business failed")
					case "panic":
						panic("business panicked")
					}
					return "done", nil
				},
			})
			sawFinalTraceData := false
			rt.traceWriter = callbackTraceWriter(func(ctx *gin.Context, event TraceEvent) {
				if event.Type == "run" {
					value, ok := Get(ctx, "id")
					sawFinalTraceData = ok && value == 42
				}
			})
			ctx := &gin.Context{}
			result, err := rt.Run(ctx, "test", nil)
			wantErr := mode == "error" || mode == "panic"
			if (err != nil) != wantErr || (!wantErr && result != "done") || (wantErr && result != nil) {
				t.Fatalf("result=%v error=%v", result, err)
			}
			if captured == nil || !sawFinalTraceData || RunID(ctx) == "" {
				t.Fatalf("captured=%v final trace data=%t runID=%q", captured, sawFinalTraceData, RunID(ctx))
			}
			for _, ended := range []*gin.Context{ctx, captured, nil} {
				if value, exists := Get(ended, "id"); value != nil || exists {
					t.Fatalf("data leaked after Run: %v, %t", value, exists)
				}
				if err := Set(ended, "id", 99); err == nil {
					t.Fatal("Set accepted data after Run")
				}
				Stop(ended) // Ended copies and nil retain the no-op contract.
			}
		})
	}
}

func TestSharedDataIsolatedAcrossRunsAndRunFrom(t *testing.T) {
	const runs = 16
	ready := make(chan struct{}, runs)
	release := make(chan struct{})
	rt := newBusinessRuntime(t, []NodeConfig{{Func: "work"}}, map[string]any{
		"work": func(ctx *gin.Context, id int) (int, error) {
			if _, exists := Get(ctx, "id"); exists {
				return 0, errors.New("previous Run data leaked")
			}
			if err := Set(ctx, "id", id); err != nil {
				return 0, err
			}
			if id < runs {
				ready <- struct{}{}
				select {
				case <-release:
				case <-time.After(5 * time.Second):
					return 0, errors.New("concurrent Runs did not reach the barrier")
				}
			}
			value, _ := Get(ctx, "id")
			if value != id {
				return 0, fmt.Errorf("Run %d read another Run's value %v", id, value)
			}
			return id, nil
		},
	})
	var wg sync.WaitGroup
	for id := 0; id < runs; id++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if result, err := rt.Run(&gin.Context{}, "test", id); err != nil || result != id {
				t.Errorf("Run %d: result=%v err=%v", id, result, err)
			}
		}(id)
	}
	for range runs {
		select {
		case <-ready:
		case <-time.After(5 * time.Second):
			t.Fatal("Runs did not start")
		}
	}
	close(release)
	wg.Wait()
	ctx := &gin.Context{}
	for id := runs; id < runs+2; id++ {
		result, err := rt.RunFrom(ctx, "test", "work", id)
		if err != nil || result != id {
			t.Fatalf("sequential RunFrom: result=%v err=%v", result, err)
		}
	}
}

func TestSharedDataConnectsParallelToolsAndFlow(t *testing.T) {
	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	invoker := &scriptedInvoker{responses: []ModelResponse{
		{ToolCalls: []ToolCall{
			{ID: "a", Name: "a", Arguments: "{}"},
			{ID: "b", Name: "b", Arguments: "{}"},
		}, FinishReason: "tool_calls"},
		{Content: []ModelPart{{Type: PartText, Text: "done"}}},
	}}
	tools := make([]Tool, 0, 2)
	for _, name := range []string{"a", "b"} {
		tools = append(tools, callbackTool{name: name, input: struct{}{}, invoke: func(ctx *gin.Context, _ any) (any, error) {
			if value, ok := Get(ctx, "id"); !ok || value != 42 {
				return nil, fmt.Errorf("Tool %s cannot read preparation data", name)
			}
			ctx.Set("local-"+name, true)
			if err := Set(ctx, name, "result-"+name); err != nil {
				return nil, err
			}
			arrived <- struct{}{}
			select {
			case <-release:
			case <-time.After(5 * time.Second):
				return nil, errors.New("parallel Tool execution did not complete")
			}
			other := "a"
			if name == "a" {
				other = "b"
			}
			if value, ok := Get(ctx, other); !ok || value != "result-"+other {
				return nil, errors.New("shared Tool write is not visible")
			}
			if _, ok := ctx.Get("local-" + other); ok {
				return nil, errors.New("Gin Keys escaped the Tool copy")
			}
			return name, nil
		}})
	}
	rt, err := New(Options{
		Config: Config{Agents: map[string]AgentConfig{"test": {Flow: []NodeConfig{
			{Func: "prepare"}, {ModelName: "model", SystemPrompt: "Use the tools.", Tools: []string{"a", "b"}}, {Func: "finish"},
		}}}},
		Models: map[string]ModelInvoker{"model": invoker},
	})
	if err != nil {
		t.Fatal(err)
	}
	err = rt.Register(map[string]any{
		"prepare": func(ctx *gin.Context) (Request, error) {
			return Request{Messages: []Message{{Role: RoleUser, Content: []Part{{Type: PartText, Text: "hello"}}}}}, Set(ctx, "id", 42)
		},
		"finish": func(ctx *gin.Context, text string) (string, error) {
			for _, name := range []string{"a", "b"} {
				value, ok := Get(ctx, name)
				if !ok || value != "result-"+name {
					return "", fmt.Errorf("missing %s result", name)
				}
				if _, ok := ctx.Get("local-" + name); ok {
					return "", errors.New("Tool Gin Keys leaked into flow")
				}
			}
			return text, nil
		},
	}, tools, nil)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		<-arrived
		<-arrived
		close(release)
	}()
	result, err := rt.Run(&gin.Context{}, "test", nil)
	if err != nil || result != "done" {
		t.Fatalf("result=%v err=%v", result, err)
	}
}

func TestToolStopIsAnErrorEvenWhenToolRecovers(t *testing.T) {
	for _, recoverInside := range []bool{false, true} {
		t.Run(fmt.Sprintf("recover=%t", recoverInside), func(t *testing.T) {
			writer := &collectingTraceWriter{}
			invoker := &scriptedInvoker{responses: []ModelResponse{{
				ToolCalls: []ToolCall{{ID: "stop-1", Name: "bad_stop", Arguments: "{}"}}, FinishReason: "tool_calls",
			}}}
			rt, err := New(Options{
				Config: Config{Agents: map[string]AgentConfig{"test": {Flow: []NodeConfig{
					{ModelName: "model", SystemPrompt: "Use the tool.", Tools: []string{"bad_stop"}}, {Func: "never"},
				}}}},
				Models: map[string]ModelInvoker{"model": invoker}, TraceWriter: writer,
			})
			if err != nil {
				t.Fatal(err)
			}
			called := false
			var captured *gin.Context
			badTool := callbackTool{name: "bad_stop", input: struct{}{}, invoke: func(ctx *gin.Context, _ any) (any, error) {
				captured = ctx.Copy()
				if err := Set(ctx, "temporary", true); err != nil {
					return nil, err
				}
				if recoverInside {
					defer func() { _ = recover() }()
				}
				Stop(ctx)
				return "must not succeed", nil
			}}
			if err := rt.Register(map[string]any{
				"never": func(*gin.Context, string) { called = true },
			}, []Tool{badTool}, nil); err != nil {
				t.Fatal(err)
			}
			result, err := rt.Run(&gin.Context{}, "test", Request{Messages: []Message{{
				Role: RoleUser, Content: []Part{{Type: PartText, Text: "hello"}},
			}}})
			if err == nil || !strings.Contains(err.Error(), `Tool "bad_stop" must not call Stop`) || result != nil || called {
				t.Fatalf("result=%v err=%v downstream=%t", result, err, called)
			}
			if _, ok := Get(captured, "temporary"); ok {
				t.Fatal("failed Tool's copied context retained Run data")
			}
			Stop(captured)
			seen := map[string]bool{}
			for _, event := range writer.snapshot() {
				if event.Type == "run" || event.Type == "tool" {
					if event.Status != "error" {
						t.Fatalf("Tool Stop produced non-error event: %+v", event)
					}
					seen[event.Type] = true
				}
			}
			if !seen["run"] || !seen["tool"] {
				t.Fatalf("missing error traces: %v", seen)
			}
		})
	}
}

func TestBatchSharedDataReadonlyAndToolStopStaysPerItem(t *testing.T) {
	copies := make(chan *gin.Context, 2)
	tool := callbackTool{name: "context_probe", input: batchContextInput{}, invoke: func(ctx *gin.Context, input any) (any, error) {
		copies <- ctx.Copy()
		if value, ok := Get(ctx, "id"); !ok || value != 42 {
			return nil, errors.New("Batch could not read common data")
		}
		if err := Set(ctx, "id", 99); err == nil {
			return nil, errors.New("Batch accepted a write")
		}
		if input.(batchContextInput).Item == "bad" {
			Stop(ctx)
		}
		return "ok", nil
	}}
	rt, err := New(Options{
		Config: Config{Agents: map[string]AgentConfig{"test": {Flow: []NodeConfig{
			{Func: "prepare"},
			{ModelName: "model", SystemPrompt: "Process each item.", Tools: []string{"context_probe"}},
			{Func: "aggregate"},
		}}}},
		Models: map[string]ModelInvoker{"model": batchContextInvoker{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	err = rt.Register(map[string]any{
		"prepare": func(ctx *gin.Context) (Batch, error) {
			batch := Batch{Concurrency: 2}
			for _, text := range []string{"bad", "good"} {
				batch.Items = append(batch.Items, BatchItem{Messages: []Message{{
					Role: RoleUser, Content: []Part{{Type: PartText, Text: text}},
				}}})
			}
			return batch, Set(ctx, "id", 42)
		},
		"aggregate": func(ctx *gin.Context, result BatchResult[string]) (string, error) {
			if len(result.Items) != 2 || result.Items[0].Err == nil ||
				!strings.Contains(result.Items[0].Err.Error(), "must not call Stop") ||
				result.Items[1].Err != nil || result.Items[1].Value != "done:good" {
				return "", fmt.Errorf("unexpected Batch result: %+v", result)
			}
			if value, _ := Get(ctx, "id"); value != 42 {
				return "", errors.New("Batch modified original shared data")
			}
			for range 2 {
				if _, ok := Get(<-copies, "id"); ok {
					return "", errors.New("completed Batch retained readonly snapshot")
				}
			}
			return "aggregated", Set(ctx, "id", 43)
		},
	}, []Tool{tool}, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := rt.Run(&gin.Context{}, "test", nil)
	if err != nil || result != "aggregated" {
		t.Fatalf("result=%v err=%v", result, err)
	}
}

func TestStopPermissionDoesNotEscapeFunction(t *testing.T) {
	for _, useOldCopy := range []bool{false, true} {
		t.Run(fmt.Sprintf("oldCopy=%t", useOldCopy), func(t *testing.T) {
			var oldCopy *gin.Context
			model := callbackModel(func(ctx *gin.Context, _ ModelRequest) (ModelResponse, error) {
				if useOldCopy {
					ctx = oldCopy
				}
				Stop(ctx)
				return ModelResponse{Content: []ModelPart{{Type: PartText, Text: "must not succeed"}}}, nil
			})
			rt, err := New(Options{
				Config: Config{Agents: map[string]AgentConfig{"test": {Flow: []NodeConfig{
					{Func: "prepare"}, {ModelName: "model", SystemPrompt: "Respond."},
				}}}},
				Models: map[string]ModelInvoker{"model": model},
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := rt.Register(map[string]any{
				"prepare": func(ctx *gin.Context) Request {
					oldCopy = ctx.Copy()
					return Request{Messages: []Message{{Role: RoleUser, Content: []Part{{Type: PartText, Text: "hello"}}}}}
				},
			}, nil, nil); err != nil {
				t.Fatal(err)
			}
			result, err := rt.Run(&gin.Context{}, "test", nil)
			if err == nil || !strings.Contains(err.Error(), "Stop is only allowed") || result != nil {
				t.Fatalf("result=%v err=%v", result, err)
			}
		})
	}
}

func TestNativeSkillToolCannotStopFlow(t *testing.T) {
	for _, recoverInside := range []bool{false, true} {
		t.Run(fmt.Sprintf("recover=%t", recoverInside), func(t *testing.T) {
			invoker := &scriptedInvoker{responses: []ModelResponse{{
				ToolCalls: []ToolCall{{ID: "skill-1", Name: "skill", Arguments: `{"skill":"guide"}`}}, FinishReason: "tool_calls",
			}}}
			rt, err := New(Options{
				Config: Config{Agents: map[string]AgentConfig{"test": {Flow: []NodeConfig{{
					ModelName: "model", SystemPrompt: "Load guidance.", Skills: []string{"guidance"},
				}}}}},
				Models:        map[string]ModelInvoker{"model": invoker},
				SkillBackends: map[string]SkillBackend{"guidance": stoppingSkillBackend{recoverInside: recoverInside}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := rt.Register(nil, nil, nil); err != nil {
				t.Fatal(err)
			}
			result, err := rt.Run(&gin.Context{}, "test", Request{Messages: []Message{{
				Role: RoleUser, Content: []Part{{Type: PartText, Text: "hello"}},
			}}})
			if err == nil || !strings.Contains(err.Error(), `Tool "skill" must not call Stop`) || result != nil {
				t.Fatalf("result=%v err=%v", result, err)
			}
		})
	}
}
