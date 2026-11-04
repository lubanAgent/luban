package luban

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
)

type emptyResult struct{ Value int }
type namedList []emptyResult
type namedMap map[string]emptyResult

func emptyRuntime(t *testing.T, flow []NodeConfig, funcs map[string]any, model *scriptedInvoker, writer *collectingTraceWriter) *Runtime {
	t.Helper()
	rt, err := New(Options{
		Config: Config{Agents: map[string]AgentConfig{"test": {Flow: flow}}},
		Models: map[string]ModelInvoker{"model": model}, TraceWriter: writer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Register(funcs, nil, nil); err != nil {
		t.Fatal(err)
	}
	return rt
}

func TestEmptyBatchContinuesAndLaterModelExecutes(t *testing.T) {
	for _, items := range [][]BatchItem{nil, {}} {
		t.Run("empty batch", func(t *testing.T) {
			model := &scriptedInvoker{responses: []ModelResponse{{Content: []ModelPart{{Type: PartText, Text: "next"}}}}}
			writer := &collectingTraceWriter{}
			rt := emptyRuntime(t, []NodeConfig{
				{Func: "prepare"}, {ModelName: "model", SystemPrompt: "Repair."},
				{Func: "next"}, {ModelName: "model", SystemPrompt: "Suggest."},
			}, map[string]any{
				"prepare": func(*gin.Context) Batch {
					return Batch{Items: items, Output: Output[emptyResult]()}
				},
				"next": func(_ *gin.Context, got BatchResult[emptyResult]) Request {
					if got.Items == nil || len(got.Items) != 0 {
						t.Errorf("empty typed result: %#v", got)
					}
					return Request{Messages: []Message{{Role: RoleUser, Content: []Part{{Type: PartText, Text: "suggest"}}}}}
				},
			}, model, writer)
			got, err := rt.Run(&gin.Context{}, "test", nil)
			if err != nil || got != "next" || len(model.snapshot()) != 1 {
				t.Fatalf("result=%#v err=%v modelCalls=%d", got, err, len(model.snapshot()))
			}
			var skipped int
			for _, event := range writer.snapshot() {
				if event.Type == "agent" && event.FlowIndex != nil && *event.FlowIndex == 1 {
					if event.Status != "skipped" || event.SkipReason != "empty_input" {
						t.Errorf("skipped flow span: %+v", event)
					}
					skipped++
				}
				if event.Type == "model" && event.FlowIndex != nil && *event.FlowIndex == 1 {
					t.Errorf("phantom model: %+v", event)
				}
			}
			if skipped != 1 {
				t.Fatalf("skipped=%d", skipped)
			}
		})
	}
}

func TestEmptyRequestPreservesExactZeroType(t *testing.T) {
	tests := []struct {
		name string
		spec *OutputSpec
		use  any
	}{
		{"struct", Output[emptyResult](), func(_ *gin.Context, got emptyResult) emptyResult {
			if got != (emptyResult{}) {
				t.Errorf("struct=%#v", got)
			}
			return got
		}},
		{"pointer", Output[*emptyResult](), func(_ *gin.Context, got *emptyResult) *emptyResult {
			if got != nil {
				t.Errorf("pointer=%#v", got)
			}
			return got
		}},
		{"slice", Output[namedList](), func(_ *gin.Context, got namedList) namedList {
			if got != nil {
				t.Errorf("slice=%#v", got)
			}
			return got
		}},
		{"map", Output[namedMap](), func(_ *gin.Context, got namedMap) namedMap {
			if got != nil {
				t.Errorf("map=%#v", got)
			}
			return got
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for _, messages := range [][]Message{nil, {}} {
				model := &scriptedInvoker{}
				rt := emptyRuntime(t, []NodeConfig{
					{Func: "prepare"}, {ModelName: "model", SystemPrompt: "Review."}, {Func: "consume"},
				}, map[string]any{
					"prepare": func(*gin.Context) Request { return Request{Messages: messages, Output: tc.spec} },
					"consume": tc.use,
				}, model, nil)
				got, err := rt.Run(&gin.Context{}, "test", nil)
				if err != nil || reflect.TypeOf(got) != tc.spec.goType || len(model.snapshot()) != 0 {
					t.Fatalf("result=%#v (%T), err=%v, modelCalls=%d", got, got, err, len(model.snapshot()))
				}
			}
			// A final model node returns precisely the same typed zero value.
			model := &scriptedInvoker{}
			rt := emptyRuntime(t, []NodeConfig{{ModelName: "model", SystemPrompt: "Review."}}, nil, model, nil)
			got, err := rt.Run(&gin.Context{}, "test", Request{Output: tc.spec})
			if err != nil || reflect.TypeOf(got) != tc.spec.goType {
				t.Fatalf("final result=%#v (%T), err=%v", got, got, err)
			}
		})
	}
}

func TestEmptyInputRejectsInvalidShapeWithoutModelCall(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   any
		want error
	}{
		{"missing Request Output", Request{}, nil},
		{"invalid Request Output", Request{Output: &OutputSpec{}}, nil},
		{"missing Batch Output", Batch{}, ErrEmptyBatch},
		{"invalid Batch Output", Batch{Output: &OutputSpec{}}, ErrEmptyBatch},
		{"negative empty Batch concurrency", Batch{Concurrency: -1, Output: Output[emptyResult]()}, nil},
		{"nil pointer", (*Request)(nil), nil},
		{"wrong input", "not a Request", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := &scriptedInvoker{}
			rt := emptyRuntime(t, []NodeConfig{{ModelName: "model", SystemPrompt: "Review."}}, nil, model, nil)
			out, err := rt.Run(&gin.Context{}, "test", tc.in)
			if err == nil || out != nil || len(model.snapshot()) != 0 || (tc.want != nil && !errors.Is(err, tc.want)) {
				t.Fatalf("result=%#v err=%v calls=%d", out, err, len(model.snapshot()))
			}
		})
	}
	model := &scriptedInvoker{}
	rt := emptyRuntime(t, []NodeConfig{{Func: "prepare"}, {ModelName: "model", SystemPrompt: "Review."}, {Func: "consume"}},
		map[string]any{
			"prepare": func(*gin.Context) Request { return Request{Output: Output[emptyResult]()} },
			"consume": func(*gin.Context, string) {},
		}, model, nil)
	if _, err := rt.Run(&gin.Context{}, "test", nil); err == nil || !strings.Contains(err.Error(), "not assignable") {
		t.Fatalf("mismatched consumer accepted: %v", err)
	}
	if len(model.snapshot()) != 0 {
		t.Fatal("mismatched consumer invoked model")
	}
}

func TestNonEmptyRequestAndBatchItemNeverSkip(t *testing.T) {
	model := &scriptedInvoker{responses: []ModelResponse{{Content: []ModelPart{{Type: PartText, Text: "ok"}}}}}
	rt := emptyRuntime(t, []NodeConfig{{ModelName: "model", SystemPrompt: "Review."}}, nil, model, nil)
	out, err := rt.Run(&gin.Context{}, "test", Request{Messages: []Message{{Role: RoleUser, Content: []Part{{Type: PartText, Text: ""}}}}})
	if err != nil || out != "ok" || len(model.snapshot()) != 1 {
		t.Fatalf("empty text incorrectly skipped: %#v %v %d", out, err, len(model.snapshot()))
	}
	_, err = rt.Run(&gin.Context{}, "test", Batch{Items: []BatchItem{{Messages: nil}}})
	if errors.Is(err, ErrEmptyBatch) || len(model.snapshot()) != 2 {
		t.Fatalf("single Batch item silently skipped: err=%v modelCalls=%d", err, len(model.snapshot()))
	}
}

func TestEmptyRequestCancellationWinsAndRunFromStillStartsAtFunction(t *testing.T) {
	model := &scriptedInvoker{}
	var called bool
	rt := emptyRuntime(t, []NodeConfig{
		{Func: "prepare"}, {ModelName: "model", SystemPrompt: "Review."}, {Func: "consume"},
	}, map[string]any{
		"prepare": func(*gin.Context) Request { return Request{Output: Output[emptyResult]()} },
		"consume": func(*gin.Context, emptyResult) string { called = true; return "done" },
	}, model, nil)
	parent, cancel := context.WithCancel(context.Background())
	ctx := &gin.Context{}
	if err := BindContext(ctx, parent); err != nil {
		t.Fatal(err)
	}
	cancel()
	if out, err := rt.Run(ctx, "test", nil); !errors.Is(err, context.Canceled) || out != nil || called {
		t.Fatalf("cancelled flow: %#v %v called=%t", out, err, called)
	}
	if out, err := rt.RunFrom(&gin.Context{}, "test", "prepare", nil); err != nil || out != "done" || !called {
		t.Fatalf("RunFrom: %#v %v called=%t", out, err, called)
	}
	if len(model.snapshot()) != 0 {
		t.Fatal("skipped model was invoked")
	}
}

func TestConsecutiveEmptyNodesAndConcurrentRunsDoNotLeakSkip(t *testing.T) {
	model := &scriptedInvoker{respond: func(_ ModelRequest) (ModelResponse, error) {
		return ModelResponse{Content: []ModelPart{{Type: PartText, Text: "actual"}}}, nil
	}}
	rt := emptyRuntime(t, []NodeConfig{
		{Func: "prepare"}, {ModelName: "model", SystemPrompt: "Repair."},
		{Func: "next"}, {ModelName: "model", SystemPrompt: "Suggest."},
		{Func: "finish"},
	}, map[string]any{
		"prepare": func(_ *gin.Context, active bool) Request {
			if active {
				return Request{Messages: []Message{{Role: RoleUser, Content: []Part{{Type: PartText, Text: "work"}}}}}
			}
			return Request{Output: Output[emptyResult]()}
		},
		"next": func(_ *gin.Context, input any) Request {
			if input == (emptyResult{}) {
				return Request{Output: Output[emptyResult]()}
			}
			return Request{Messages: []Message{{Role: RoleUser, Content: []Part{{Type: PartText, Text: "more"}}}}}
		},
		"finish": func(_ *gin.Context, input any) string {
			if input == (emptyResult{}) {
				return "skipped twice"
			}
			return input.(string)
		},
	}, model, nil)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			active := i%2 == 0
			out, err := rt.Run(&gin.Context{}, "test", active)
			want := "skipped twice"
			if active {
				want = "actual"
			}
			if err != nil || out != want {
				t.Errorf("run %d: %#v %v", i, out, err)
			}
		}(i)
	}
	wg.Wait()
	if len(model.snapshot()) != 12 {
		t.Fatalf("expected 12 real model calls, got %d", len(model.snapshot()))
	}
}

// A real two-stage conditional Agent: a business function decides whether
// repairs exist, while the next function always runs and prepares suggestions.
func TestConditionalRepairAndSuggestionRunThroughRealRuntime(t *testing.T) {
	for _, hasRepair := range []bool{false, true} {
		t.Run(map[bool]string{false: "no repair", true: "repair exists"}[hasRepair], func(t *testing.T) {
			model := &scriptedInvoker{respond: func(req ModelRequest) (ModelResponse, error) {
				last := lastUserText(req)
				if last == "repair" {
					return ModelResponse{ToolCalls: []ToolCall{{
						ID: "repair-out", Name: runtimeOutputToolName, Arguments: `{"value":9}`,
					}}, FinishReason: "tool_calls"}, nil
				}
				return ModelResponse{ToolCalls: []ToolCall{{
					ID: "suggest-out", Name: runtimeOutputToolName, Arguments: `{"value":42}`,
				}}, FinishReason: "tool_calls"}, nil
			}}
			writer := &collectingTraceWriter{}
			rt := emptyRuntime(t, []NodeConfig{
				{Func: "prepareRepair"}, {ModelName: "model", SystemPrompt: "Fix."},
				{Func: "prepareSuggest"}, {ModelName: "model", SystemPrompt: "Suggest."},
				{Func: "finish"},
			}, map[string]any{
				"prepareRepair": func(_ *gin.Context, needsRepair bool) Batch {
					batch := Batch{Output: Output[emptyResult]()}
					if needsRepair {
						batch.Items = []BatchItem{{Messages: []Message{{
							Role: RoleUser, Content: []Part{{Type: PartText, Text: "repair"}},
						}}}}
					}
					return batch
				},
				"prepareSuggest": func(_ *gin.Context, repairs BatchResult[emptyResult]) Request {
					if hasRepair && (len(repairs.Items) != 1 || repairs.Items[0].Value.Value != 9) {
						t.Errorf("repair result lost: %+v", repairs)
					}
					if !hasRepair && len(repairs.Items) != 0 {
						t.Errorf("unexpected repair: %+v", repairs)
					}
					req := Request{Messages: []Message{{
						Role: RoleUser, Content: []Part{{Type: PartText, Text: "suggest"}},
					}}, Output: Output[emptyResult]()}
					return req
				},
				"finish": func(_ *gin.Context, suggested emptyResult) int { return suggested.Value },
			}, model, writer)
			result, err := rt.Run(&gin.Context{}, "test", hasRepair)
			expectedCalls := 1
			if hasRepair {
				expectedCalls = 2
			}
			// Only model invocations carrying the first prompt count; each
			// structured output is submitted by the model's first response.
			if err != nil || result != 42 || len(model.snapshot()) != expectedCalls {
				t.Fatalf("result=%#v err=%v modelCalls=%d", result, err, len(model.snapshot()))
			}
			if hasRepair {
				for _, event := range writer.snapshot() {
					if event.Status == "skipped" {
						t.Fatalf("real repair was skipped: %+v", event)
					}
				}
			}
		})
	}
}

func TestNonemptyOutputToolFailuresAreNotSkipped(t *testing.T) {
	for _, response := range []ModelResponse{
		{Content: []ModelPart{{Type: PartText, Text: `{"value":1}`}}},
		{ToolCalls: []ToolCall{{ID: "bad", Name: runtimeOutputToolName, Arguments: `{"value":`}}, FinishReason: "tool_calls"},
	} {
		model := &scriptedInvoker{responses: []ModelResponse{response}}
		writer := &collectingTraceWriter{}
		rt := emptyRuntime(t, []NodeConfig{{ModelName: "model", SystemPrompt: "Review."}}, nil, model, writer)
		input := Request{Messages: []Message{{Role: RoleUser, Content: []Part{{Type: PartText, Text: "work"}}}}, Output: Output[emptyResult]()}
		out, err := rt.Run(&gin.Context{}, "test", input)
		if err == nil || out != nil || len(model.snapshot()) == 0 {
			t.Fatalf("invalid OutputTool became skip: out=%#v err=%v", out, err)
		}
		if model.snapshot()[0].ToolChoice != ToolChoiceRequired {
			t.Fatalf("missing required tool choice: %+v", model.snapshot()[0])
		}
		for _, event := range writer.snapshot() {
			if event.Status == "skipped" {
				t.Fatalf("failed model marked skipped: %+v", event)
			}
		}
	}
}

func TestEmptyInputDoesNotInitializeToolsSkillsOrSubAgents(t *testing.T) {
	tool := &agentTestTool{}
	parent, child := &scriptedInvoker{}, &scriptedInvoker{}
	rt, err := New(Options{
		Config: Config{Agents: map[string]AgentConfig{"test": {Flow: []NodeConfig{
			{Func: "prepare"}, {ModelName: "parent", SystemPrompt: "Select child.",
				Tools: []string{"lookup"}, Skills: []string{"guide"},
				SubAgents: []SubAgentConfig{{ModelName: "child", SystemPrompt: "Work."}}},
			{Func: "finish"},
		}}}},
		Models: map[string]ModelInvoker{"parent": parent, "child": child},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Register(map[string]any{
		"prepare": func(*gin.Context) Request { return Request{Output: Output[emptyResult]()} },
		"finish":  func(_ *gin.Context, got emptyResult) string { return "continued" },
	}, []Tool{tool}, []Skill{{Name: "guide", Description: "Guide", Content: "instructions"}}); err != nil {
		t.Fatal(err)
	}
	out, err := rt.Run(&gin.Context{}, "test", nil)
	if err != nil || out != "continued" || len(parent.snapshot()) != 0 || len(child.snapshot()) != 0 || len(tool.received) != 0 {
		t.Fatalf("skip side effects: %#v %v parent=%d child=%d tool=%d", out, err,
			len(parent.snapshot()), len(child.snapshot()), len(tool.received))
	}
}

func TestCancellationDuringPrepareIsNotReportedAsSkip(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	ctx := &gin.Context{}
	if err := BindContext(ctx, parent); err != nil {
		t.Fatal(err)
	}
	writer, model := &collectingTraceWriter{}, &scriptedInvoker{}
	rt := emptyRuntime(t, []NodeConfig{{Func: "prepare"}, {ModelName: "model", SystemPrompt: "Review."}},
		map[string]any{
			"prepare": func(*gin.Context) Request {
				cancel()
				return Request{Output: Output[emptyResult]()}
			},
		}, model, writer)
	out, err := rt.Run(ctx, "test", nil)
	if out != nil || !errors.Is(err, context.Canceled) || len(model.snapshot()) != 0 {
		t.Fatalf("cancellation became skip: %#v %v", out, err)
	}
	for _, event := range writer.snapshot() {
		if event.Status == "skipped" {
			t.Fatalf("cancelled node incorrectly skipped: %+v", event)
		}
	}
}
