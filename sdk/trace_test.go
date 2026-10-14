package luban

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	internaltrace "github.com/lubanAgent/luban/sdk/internal/trace"
)

type collectingTraceWriter struct {
	mu     sync.Mutex
	events []TraceEvent
	panic  bool
}

func TestTraceBatchResultPreservesItemError(t *testing.T) {
	data := internaltrace.Snapshot(BatchResult[string]{Items: []BatchItemResult[string]{
		{Value: "first"},
		{Err: errors.New("second failed")},
	}})
	if !strings.Contains(string(data), `"Err":"second failed"`) ||
		!strings.Contains(string(data), `"Value":"first"`) {
		t.Fatalf("Batch item was lost: %s", data)
	}
}

func TestTraceModelUsageKeepsRealCountsAndOmission(t *testing.T) {
	withUsage := modelResponseToInternal(ModelResponse{
		Usage: &ModelUsage{InputTokens: 13, OutputTokens: 7},
	})
	if withUsage.Usage == nil || withUsage.Usage.InputTokens != 13 ||
		withUsage.Usage.OutputTokens != 7 {
		t.Fatalf("usage lost in SDK adapter: %+v", withUsage)
	}
	withoutUsage := internaltrace.Snapshot(modelResponseToInternal(ModelResponse{}))
	if strings.Contains(string(withoutUsage), `"usage"`) {
		t.Fatalf("missing usage should not look like zero tokens: %s", withoutUsage)
	}
}

func TestTraceRunCapturesModelUsage(t *testing.T) {
	writer := &collectingTraceWriter{}
	rt, err := New(Options{
		Config: Config{Agents: map[string]AgentConfig{"task": {Flow: []NodeConfig{{
			ModelName: "model", SystemPrompt: "Answer briefly.",
		}}}}},
		Models: map[string]ModelInvoker{"model": &scriptedInvoker{responses: []ModelResponse{{
			Content: []ModelPart{{Type: PartText, Text: "ok"}},
			Usage:   &ModelUsage{InputTokens: 23, OutputTokens: 4},
		}}}},
		TraceWriter: writer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Register(nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	_, err = rt.Run(newGinContextForAgentTest(t), "task", Request{
		Messages: []Message{{Role: RoleUser, Content: []Part{{Type: PartText, Text: "Hello."}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range writer.snapshot() {
		if event.Type != "model" {
			continue
		}
		var output struct {
			Usage *ModelUsage `json:"usage"`
		}
		if err := json.Unmarshal(event.Output, &output); err != nil {
			t.Fatal(err)
		}
		if output.Usage == nil || output.Usage.InputTokens != 23 || output.Usage.OutputTokens != 4 {
			t.Fatalf("model trace lost tokens: %s", event.Output)
		}
		return
	}
	t.Fatal("model trace event missing")
}

func TestTraceFunctionPanicClosesNodeWithError(t *testing.T) {
	writer := &collectingTraceWriter{}
	rt, err := New(Options{
		Config:      Config{Agents: map[string]AgentConfig{"task": {Flow: []NodeConfig{{Func: "panic"}}}}},
		TraceWriter: writer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Register(map[string]any{"panic": func(*gin.Context) { panic("business broke") }}, nil, nil); err != nil {
		t.Fatal(err)
	}
	_, err = rt.Run(newGinContextForAgentTest(t), "task", nil)
	if err == nil || !strings.Contains(err.Error(), "business broke") {
		t.Fatalf("panic not converted to Run error: %v", err)
	}
	events := writer.snapshot()
	if len(events) != 2 {
		t.Fatalf("panic spans: %+v", events)
	}
	for _, event := range events {
		if event.Status != "error" || !strings.Contains(event.Error, "business broke") {
			t.Fatalf("panic span falsely marked successful: %s %s", event.Status, event.Error)
		}
	}
}

func TestTraceBatchItemsStayUnderTheirOwnParents(t *testing.T) {
	writer := &collectingTraceWriter{}
	rt, err := New(Options{
		Config: Config{Agents: map[string]AgentConfig{"task": {Flow: []NodeConfig{{
			ModelName: "model", SystemPrompt: "Review each item.",
		}}}}},
		Models:      map[string]ModelInvoker{"model": &batchTestInvoker{failText: "bad"}},
		TraceWriter: writer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Register(nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	batch := Batch{Concurrency: 3}
	for _, value := range []string{"good-1", "bad", "good-2"} {
		batch.Items = append(batch.Items, BatchItem{Messages: []Message{{
			Role: RoleUser, Content: []Part{{Type: PartText, Text: value}},
		}}})
	}
	result, err := rt.Run(newGinContextForAgentTest(t), "task", batch)
	if err != nil {
		t.Fatal(err)
	}
	if result.(BatchResult[string]).Items[1].Err == nil {
		t.Fatal("failed item did not retain its error")
	}
	events := writer.snapshot()
	byID := make(map[string]TraceEvent)
	for _, event := range events {
		byID[event.SpanID] = event
	}
	itemCount, modelCount := 0, 0
	for _, event := range events {
		switch event.Type {
		case "batchItem":
			itemCount++
			if event.Name == "item[1]" && event.Status != "error" {
				t.Fatalf("failed item has status %s", event.Status)
			}
			parent := byID[event.ParentSpanID]
			if parent.Type != "agent" || parent.FlowIndex == nil || *parent.FlowIndex != 0 {
				t.Fatalf("item has wrong flow parent: %+v", event)
			}
		case "model":
			modelCount++
			agent := byID[event.ParentSpanID]
			item := byID[agent.ParentSpanID]
			if agent.Type != "agent" || item.Type != "batchItem" {
				t.Fatalf("model escaped item Agent: %+v", event)
			}
		}
	}
	if itemCount != 3 || modelCount != 3 {
		t.Fatalf("batch trace item=%d model=%d events=%d", itemCount, modelCount, len(events))
	}
}

func (w *collectingTraceWriter) WriteTrace(_ *gin.Context, event TraceEvent) {
	if w.panic {
		panic("log unavailable")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.events = append(w.events, event)
}

func (w *collectingTraceWriter) snapshot() []TraceEvent {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]TraceEvent(nil), w.events...)
}

func TestTraceCapturesFlowInputsOutputsAndStop(t *testing.T) {
	writer := &collectingTraceWriter{}
	rt, err := New(Options{Config: Config{Agents: map[string]AgentConfig{
		"task": {Flow: []NodeConfig{{Func: "prepare", Description: "准备数据"}, {Func: "stop"}, {Func: "never"}}},
	}}, TraceWriter: writer})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Register(map[string]any{
		"prepare": func(_ *gin.Context, value string) string { return "prepared:" + value },
		"stop": func(ctx *gin.Context, value string) string {
			Stop(ctx)
			return "stopped:" + value
		},
		"never": func(_ *gin.Context, value string) string { t.Fatal("Stop did not end flow"); return value },
	}, nil, nil); err != nil {
		t.Fatal(err)
	}
	ctx := newGinContextForAgentTest(t)
	out, err := rt.Run(ctx, "task", "hello")
	if err != nil || out != "stopped:prepared:hello" {
		t.Fatalf("out=%v err=%v", out, err)
	}
	events := writer.snapshot()
	if len(events) != 3 || RunID(ctx) == "" {
		t.Fatalf("events=%+v runID=%q", events, RunID(ctx))
	}
	var run, prepare, stop TraceEvent
	for _, event := range events {
		if event.RunID != RunID(ctx) {
			t.Fatalf("event has foreign run ID: %+v", event)
		}
		switch event.Type {
		case "run":
			run = event
		case "function":
			if event.Name == "prepare" {
				prepare = event
			} else {
				stop = event
			}
		}
	}
	if run.Status != "stopped" || stop.Status != "stopped" ||
		prepare.ParentSpanID != run.SpanID || stop.ParentSpanID != run.SpanID ||
		prepare.Description != "准备数据" ||
		string(prepare.Input) != `"hello"` || string(prepare.Output) != `"prepared:hello"` ||
		string(stop.Input) != `"prepared:hello"` || string(stop.Output) != `"stopped:prepared:hello"` {
		t.Fatalf("incorrect tree or values: %+v", events)
	}
}

func TestTraceModelSkillAndOutputToolContainFullValues(t *testing.T) {
	writer := &collectingTraceWriter{}
	invoker := &scriptedInvoker{responses: []ModelResponse{
		{ToolCalls: []ToolCall{{ID: "load-1", Name: "skill", Arguments: `{"skill":"guide"}`}}, FinishReason: "tool_calls"},
		{ToolCalls: []ToolCall{{ID: "out-1", Name: runtimeOutputToolName, Arguments: `{"score":91,"summary":"hello","problems":[]}`}}, FinishReason: "tool_calls"},
	}}
	rt, err := New(Options{
		Config: Config{Agents: map[string]AgentConfig{"task": {Flow: []NodeConfig{{
			ModelName: "model", SystemPrompt: "Complete system prompt.", Skills: []string{"guide"},
		}}}}},
		Models: map[string]ModelInvoker{"model": invoker}, TraceWriter: writer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Register(nil, nil, []Skill{{Name: "guide", Description: "guidance", Content: "Full Skill content here."}}); err != nil {
		t.Fatal(err)
	}
	ctx := newGinContextForAgentTest(t)
	_, err = rt.Run(ctx, "task", Request{
		Messages: []Message{{Role: RoleUser, Content: []Part{{Type: PartText, Text: "Complete user prompt."}}}},
		Output:   Output[lessonReviewResult](),
	})
	if err != nil {
		t.Fatal(err)
	}
	var models, skills, outputs int
	ids := make(map[string]TraceEvent)
	for _, event := range writer.snapshot() {
		ids[event.SpanID] = event
		if event.Type == "model" {
			models++
			if !strings.Contains(string(event.Input), "Complete system prompt.") ||
				!strings.Contains(string(event.Input), "Complete user prompt.") {
				t.Fatalf("model messages missing: %s", event.Input)
			}
		}
		if event.Type == "skill" {
			skills++
			if !strings.Contains(string(event.Output), "Full Skill content here.") {
				t.Fatalf("Skill body missing: %s", event.Output)
			}
		}
		if event.Name == runtimeOutputToolName && event.Type == "tool" {
			outputs++
			if !strings.Contains(string(event.Input), "score") || !strings.Contains(string(event.Output), "score") {
				t.Fatalf("OutputTool values missing: %+v", event)
			}
		}
	}
	if models != 2 || skills != 1 || outputs != 1 {
		t.Fatalf("model=%d skill=%d output=%d events=%+v", models, skills, outputs, writer.snapshot())
	}
	for _, event := range ids {
		if event.Type == "run" && !strings.Contains(string(event.Input), `"outputType":"luban.lessonReviewResult"`) {
			t.Fatalf("Run input lost its structured output declaration: %s", event.Input)
		}
		if event.Type != "run" {
			if _, ok := ids[event.ParentSpanID]; !ok {
				t.Fatalf("orphan Trace event: %+v", event)
			}
		}
		if event.Type == "model" || event.Type == "skill" || event.Type == "tool" {
			parent := ids[event.ParentSpanID]
			if parent.Type != "agent" || parent.AgentName != event.AgentName {
				t.Fatalf("inner event is outside its Agent: type=%s name=%s agent=%s parent=%s", event.Type, event.Name, event.AgentName, parent.Type)
			}
		}
	}
}

func TestTraceWriterPanicAndSerializationFailureDoNotChangeRun(t *testing.T) {
	writer := &collectingTraceWriter{panic: true}
	rt, err := New(Options{
		Config:      Config{Agents: map[string]AgentConfig{"task": {Flow: []NodeConfig{{Func: "step"}}}}},
		TraceWriter: writer,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := errors.New("business failed")
	if err := rt.Register(map[string]any{"step": func(_ *gin.Context, _ any) (any, error) {
		return nil, want
	}}, nil, nil); err != nil {
		t.Fatal(err)
	}
	ctx := newGinContextForAgentTest(t)
	_, err = rt.Run(ctx, "task", make(chan int))
	if !errors.Is(err, want) || RunID(ctx) == "" {
		t.Fatalf("writer changed business result or ID: %v, %q", err, RunID(ctx))
	}

	writer.panic = false
	_, _ = rt.Run(newGinContextForAgentTest(t), "task", make(chan int))
	found := false
	for _, event := range writer.snapshot() {
		if strings.Contains(string(event.Input), "_traceUnavailable") {
			var raw map[string]any
			if json.Unmarshal(event.Input, &raw) != nil {
				t.Fatalf("invalid unavailable marker: %s", event.Input)
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("missing serialization failure marker: %v", writer.snapshot())
	}
}

func TestTraceConcurrentRunsHaveDistinctIDs(t *testing.T) {
	writer := &collectingTraceWriter{}
	rt, err := New(Options{Config: Config{Agents: map[string]AgentConfig{
		"task": {Flow: []NodeConfig{{Func: "echo"}}},
	}}, TraceWriter: writer})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Register(map[string]any{"echo": func(_ *gin.Context, v string) string { return v }}, nil, nil); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(10)
	for i := 0; i < 10; i++ {
		go func(i int) {
			defer wg.Done()
			ctx := newGinContextForAgentTest(t)
			if _, runErr := rt.Run(ctx, "task", fmt.Sprintf("item-%d", i)); runErr != nil {
				t.Errorf("Run: %v", runErr)
			}
		}(i)
	}
	wg.Wait()
	groups := make(map[string]int)
	for _, event := range writer.snapshot() {
		groups[event.RunID]++
	}
	if len(groups) != 10 {
		t.Fatalf("distinct Run IDs=%d, want 10: %+v", len(groups), groups)
	}
	for id, n := range groups {
		if n != 2 {
			t.Fatalf("run %s has %d spans, want 2", id, n)
		}
	}
}

func TestTraceParentAndChildModelScopeAndMissingModel(t *testing.T) {
	writer := &collectingTraceWriter{}
	parent := &scriptedInvoker{responses: []ModelResponse{{
		ToolCalls: []ToolCall{{ID: "transfer-1", Name: "transfer_to_agent",
			Arguments: `{"agent_name":"luban_flow_0_sub_0"}`}}, FinishReason: "tool_calls",
	}}}
	child := &scriptedInvoker{responses: []ModelResponse{{
		Content: []ModelPart{{Type: PartText, Text: "Child answered."}},
	}}}
	rt, err := New(Options{
		Config: Config{Agents: map[string]AgentConfig{"task": {Flow: []NodeConfig{{
			ModelName: "parent", SystemPrompt: "Select a child.", Description: "选择业务角色",
			SubAgents: []SubAgentConfig{{ModelName: "child", SystemPrompt: "Answer.", Description: "处理咨询"}},
		}}}}},
		Models: map[string]ModelInvoker{"parent": parent, "child": child}, TraceWriter: writer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Register(nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	ctx := newGinContextForAgentTest(t)
	result, err := rt.Run(ctx, "task", Request{Messages: []Message{{
		Role: RoleUser, Content: []Part{{Type: PartText, Text: "Ask."}},
	}}})
	if err != nil || result != "Child answered." {
		t.Fatalf("result=%#v error=%v", result, err)
	}
	var models []TraceEvent
	for _, event := range writer.snapshot() {
		if event.Type == "model" {
			models = append(models, event)
		}
	}
	if len(models) != 2 || models[0].AgentName != "luban_flow_0" ||
		models[1].AgentName != "luban_flow_0_sub_0" {
		t.Fatalf("parent/child model attribution: %+v", models)
	}
	byID := make(map[string]TraceEvent)
	agents := make(map[string]TraceEvent)
	for _, event := range writer.snapshot() {
		byID[event.SpanID] = event
		if event.Type == "agent" && event.AgentName != "" {
			agents[event.AgentName] = event
		}
	}
	parentAgent, parentOK := agents["luban_flow_0"]
	childAgent, childOK := agents["luban_flow_0_sub_0"]
	if !parentOK || !childOK {
		t.Fatalf("missing actual parent/child Agent spans: %+v", writer.snapshot())
	}
	flow := byID[parentAgent.ParentSpanID]
	if flow.Type != "agent" || flow.FlowIndex == nil || *flow.FlowIndex != 0 ||
		flow.Description != "选择业务角色" || parentAgent.Description != "选择业务角色" ||
		childAgent.Description != "处理咨询" ||
		childAgent.ParentSpanID != flow.SpanID ||
		childAgent.TransferFromSpanID != parentAgent.SpanID ||
		models[0].ParentSpanID != parentAgent.SpanID ||
		models[1].ParentSpanID != childAgent.SpanID {
		t.Fatalf("incorrect Agent tree: %+v", writer.snapshot())
	}

	// An appKey/configuration error still belongs to a new Run ID.
	bad := newGinContextForAgentTest(t)
	if _, err := rt.Run(bad, "missing", nil); err == nil || RunID(bad) == "" || RunID(bad) == RunID(ctx) {
		t.Fatalf("missing configuration must have its own Run ID: %v %q", err, RunID(bad))
	}
	badStart := newGinContextForAgentTest(t)
	if _, err := rt.RunFrom(badStart, "task", "", nil); err == nil || RunID(badStart) == "" {
		t.Fatalf("invalid RunFrom must still have a Run ID: %v %q", err, RunID(badStart))
	}
}
