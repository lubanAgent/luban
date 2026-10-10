package luban

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type scriptedInvoker struct {
	mu        sync.Mutex
	responses []ModelResponse
	requests  []ModelRequest
	respond   func(ModelRequest) (ModelResponse, error)
	errAt     int
	err       error
}

func (*scriptedInvoker) Name() string { return "scripted" }

func (m *scriptedInvoker) Invoke(_ *gin.Context, request ModelRequest) (ModelResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = append(m.requests, request)
	call := len(m.requests)
	if m.err != nil && m.errAt == call {
		return ModelResponse{}, m.err
	}
	if m.respond != nil {
		return m.respond(request)
	}
	if call > len(m.responses) {
		return ModelResponse{}, errors.New("scripted model ran out of responses")
	}
	return m.responses[call-1], nil
}

func (m *scriptedInvoker) snapshot() []ModelRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]ModelRequest(nil), m.requests...)
}

type agentTestTool struct {
	mu       sync.Mutex
	received []any
}

type lookupInput struct {
	ID int64 `json:"id" jsonschema:"description=Record ID"`
}

func (t *agentTestTool) Name() string        { return "lookup" }
func (t *agentTestTool) Description() string { return "Look up a record." }
func (*agentTestTool) Input() any            { return lookupInput{} }
func (t *agentTestTool) Invoke(_ *gin.Context, input any) (any, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.received = append(t.received, input)
	typed, ok := input.(lookupInput)
	if !ok {
		return nil, errors.New("unexpected lookup input type")
	}
	return map[string]any{"id": typed.ID, "name": "Ada"}, nil
}

type lessonReviewResult struct {
	Score    int      `json:"score"`
	Summary  string   `json:"summary"`
	Problems []string `json:"problems"`
}

type agentSkill struct {
	Name        string
	Description string
	Content     string
}

type customAgentTool struct {
	name        string
	description string
	input       any
	output      any
	err         error
	panicValue  any
	started     chan struct{}
	release     <-chan struct{}
}

func (t *customAgentTool) Name() string        { return t.name }
func (t *customAgentTool) Description() string { return t.description }
func (t *customAgentTool) Input() any          { return t.input }
func (t *customAgentTool) Invoke(*gin.Context, any) (any, error) {
	if t.started != nil {
		t.started <- struct{}{}
	}
	if t.release != nil {
		<-t.release
	}
	if t.panicValue != nil {
		panic(t.panicValue)
	}
	return t.output, t.err
}

func TestRunAgentWithoutOutputReturnsRawText(t *testing.T) {
	invoker := &scriptedInvoker{responses: []ModelResponse{
		{Content: []ModelPart{{Type: PartText, Text: "  {\"answer\":1}\n"}}},
	}}
	rt := newAgentTestRuntime(t, NodeConfig{ModelName: "review", SystemPrompt: "Review carefully."}, invoker, nil)
	ctx := newGinContextForAgentTest(t)
	request := Request{Messages: []Message{{
		Role: RoleUser, Content: []Part{{Type: PartText, Text: "Review this."}},
	}}}

	result, err := rt.Run(ctx, "agent", request)
	if err != nil {
		t.Fatal(err)
	}
	if result != "  {\"answer\":1}\n" {
		t.Fatalf("result = %#v; raw assistant text should be unchanged", result)
	}
	requests := invoker.snapshot()
	if len(requests) != 1 || len(requests[0].Messages) != 2 ||
		requests[0].Messages[0].Role != ModelRoleSystem ||
		requests[0].Messages[0].Content[0].Text != "Review carefully." ||
		requests[0].Messages[1].Content[0].Text != "Review this." {
		t.Fatalf("system/user messages = %+v", requests)
	}
}

func TestRunAgentPreservesRequestTextAndMediaOrder(t *testing.T) {
	invoker := &scriptedInvoker{responses: []ModelResponse{{
		Content: []ModelPart{{Type: PartText, Text: "done"}},
	}}}
	rt := newAgentTestRuntime(t, NodeConfig{
		ModelName: "review", SystemPrompt: "Review the media.",
	}, invoker, nil)
	request := Request{Messages: []Message{{
		Role: RoleUser,
		Content: []Part{
			{Type: PartText, Text: "before"},
			{Type: PartImage, URL: "https://example.test/image.png"},
			{Type: PartVideo, URL: "https://example.test/video.mp4"},
			{Type: PartAudio, URL: "https://example.test/audio.wav"},
			{Type: PartText, Text: "after"},
		},
	}}}

	result, err := rt.Run(newGinContextForAgentTest(t), "agent", request)
	if err != nil {
		t.Fatal(err)
	}
	if result != "done" {
		t.Fatalf("Run result = %#v", result)
	}
	requests := invoker.snapshot()
	if len(requests) != 1 {
		t.Fatalf("model request count = %d, want one", len(requests))
	}
	if len(requests[0].Messages) != 2 {
		t.Fatalf("model message count = %d, want system and user", len(requests[0].Messages))
	}
	want := []ModelPart{
		{Type: PartText, Text: "before"},
		{Type: PartImage, URL: "https://example.test/image.png"},
		{Type: PartVideo, URL: "https://example.test/video.mp4"},
		{Type: PartAudio, URL: "https://example.test/audio.wav"},
		{Type: PartText, Text: "after"},
	}
	if !reflect.DeepEqual(requests[0].Messages[1].Content, want) {
		t.Fatalf("request media order/content = %+v, want %+v", requests[0].Messages[1].Content, want)
	}
}

func TestRunAgentOutputToolReturnsConcreteStruct(t *testing.T) {
	expected := lessonReviewResult{Score: 92, Summary: "Clear lesson.", Problems: []string{"audio was quiet"}}
	encoded, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	invoker := &scriptedInvoker{responses: []ModelResponse{
		{ToolCalls: []ToolCall{{
			ID: "output-1", Name: runtimeOutputToolName, Arguments: string(encoded),
		}}, FinishReason: "tool_calls"},
	}}
	rt := newAgentTestRuntime(t, NodeConfig{ModelName: "review", SystemPrompt: "Review the lesson."}, invoker, nil)
	ctx := newGinContextForAgentTest(t)
	request := Request{
		Messages: []Message{{Role: RoleUser, Content: []Part{{Type: PartVideo, URL: "https://example.test/lesson.mp4"}}}},
		Output:   Output[lessonReviewResult](),
	}

	result, err := rt.Run(ctx, "agent", request)
	if err != nil {
		t.Fatal(err)
	}
	if actual, ok := result.(lessonReviewResult); !ok || !reflect.DeepEqual(actual, expected) {
		t.Fatalf("result = %#v (%T), want %#v", result, result, expected)
	}
	requests := invoker.snapshot()
	if len(requests) != 1 || len(requests[0].Tools) != 1 ||
		requests[0].Tools[0].Name != runtimeOutputToolName {
		t.Fatalf("OutputTool was not registered with the model: %+v", requests)
	}
	if len(requests[0].Tools[0].Parameters) == 0 {
		t.Fatal("OutputTool JSON Schema was empty")
	}
	var outputSchema map[string]any
	if err := json.Unmarshal(requests[0].Tools[0].Parameters, &outputSchema); err != nil {
		t.Fatalf("OutputTool schema is not valid JSON: %v", err)
	}
	outputProperties, ok := outputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("OutputTool schema has no properties: %s", requests[0].Tools[0].Parameters)
	}
	for _, field := range []string{"score", "summary", "problems"} {
		if _, exists := outputProperties[field]; !exists {
			t.Fatalf("OutputTool schema omitted field %q: %s", field, requests[0].Tools[0].Parameters)
		}
	}
	if len(requests[0].Messages) != 2 ||
		requests[0].Messages[1].Content[0] != (ModelPart{Type: PartVideo, URL: "https://example.test/lesson.mp4"}) {
		t.Fatalf("video request content not preserved: %+v", requests[0].Messages)
	}
}

func TestRunAgentOutputToolReturnsEinoSelectedDuplicateSubmission(t *testing.T) {
	first := lessonReviewResult{Score: 51, Summary: "first", Problems: []string{}}
	last := lessonReviewResult{Score: 87, Summary: "last", Problems: []string{}}
	firstJSON, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	lastJSON, err := json.Marshal(last)
	if err != nil {
		t.Fatal(err)
	}
	invoker := &scriptedInvoker{responses: []ModelResponse{{
		ToolCalls: []ToolCall{
			{ID: "output-first", Name: runtimeOutputToolName, Arguments: string(firstJSON)},
			{ID: "output-last", Name: runtimeOutputToolName, Arguments: string(lastJSON)},
		},
		FinishReason: "tool_calls",
	}}}
	rt := newAgentTestRuntime(t, NodeConfig{ModelName: "review", SystemPrompt: "Review."}, invoker, nil)
	result, err := rt.Run(newGinContextForAgentTest(t), "agent", Request{
		Messages: []Message{{Role: RoleUser, Content: []Part{{Type: PartText, Text: "Review."}}}},
		Output:   Output[lessonReviewResult](),
	})
	if err != nil {
		t.Fatal(err)
	}
	if actual, ok := result.(lessonReviewResult); !ok || !reflect.DeepEqual(actual, last) {
		t.Fatalf("result = %#v (%T), want the value selected by Eino %#v", result, result, last)
	}
}

func TestBusinessFunctionAgentBusinessFunctionChainTransfersConcreteOutput(t *testing.T) {
	expected := lessonReviewResult{Score: 96, Summary: "Excellent.", Problems: []string{}}
	encoded, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	invoker := &scriptedInvoker{responses: []ModelResponse{{
		ToolCalls: []ToolCall{{
			ID: "output-chain", Name: runtimeOutputToolName, Arguments: string(encoded),
		}},
		FinishReason: "tool_calls",
	}}}
	saved := false
	rt, err := New(Options{
		Config: Config{Agents: map[string]AgentConfig{"agent": {Flow: []NodeConfig{
			{Func: "Prepare"},
			{ModelName: "review", SystemPrompt: "Review the lesson."},
			{Func: "Save"},
		}}}},
		Models: map[string]ModelInvoker{"review": invoker},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Register(map[string]any{
		"Prepare": func(_ *gin.Context, lessonID int64) Request {
			return Request{
				Messages: []Message{{Role: RoleUser, Content: []Part{{Type: PartText, Text: fmt.Sprintf("Review lesson %d.", lessonID)}}}},
				Output:   Output[lessonReviewResult](),
			}
		},
		"Save": func(_ *gin.Context, result lessonReviewResult) string {
			saved = reflect.DeepEqual(result, expected)
			return result.Summary
		},
	}, nil, nil); err != nil {
		t.Fatal(err)
	}
	result, err := rt.Run(newGinContextForAgentTest(t), "agent", int64(17))
	if err != nil {
		t.Fatal(err)
	}
	if !saved || result != "Excellent." {
		t.Fatalf("saved=%t result=%#v", saved, result)
	}
}

func TestRunAgentCallsBusinessToolThenReturnsOutput(t *testing.T) {
	output := lessonReviewResult{Score: 88, Summary: "Good explanation.", Problems: []string{}}
	encoded, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	invoker := &scriptedInvoker{responses: []ModelResponse{
		{ToolCalls: []ToolCall{{ID: "lookup-1", Name: "lookup", Arguments: `{"id":42}`}}, FinishReason: "tool_calls"},
		{ToolCalls: []ToolCall{{ID: "output-2", Name: runtimeOutputToolName, Arguments: string(encoded)}}, FinishReason: "tool_calls"},
	}}
	businessTool := &agentTestTool{}
	rt := newAgentTestRuntime(
		t,
		NodeConfig{ModelName: "review", SystemPrompt: "Review the lesson.", Tools: []string{"lookup"}},
		invoker,
		[]Tool{businessTool},
	)

	ctx := newGinContextForAgentTest(t)
	request := Request{
		Messages: []Message{{Role: RoleUser, Content: []Part{{Type: PartText, Text: "Review course 42."}}}},
		Output:   Output[lessonReviewResult](),
	}
	result, err := rt.Run(ctx, "agent", request)
	if err != nil {
		t.Fatal(err)
	}
	if actual, ok := result.(lessonReviewResult); !ok || !reflect.DeepEqual(actual, output) {
		t.Fatalf("result = %#v (%T), want %#v", result, result, output)
	}
	requests := invoker.snapshot()
	if len(requests) != 2 {
		t.Fatalf("model requests = %d, want tool round then OutputTool round", len(requests))
	}
	if len(requests[0].Tools) != 2 || requests[0].Tools[0].Name != "lookup" ||
		requests[0].Tools[1].Name != runtimeOutputToolName {
		t.Fatalf("model tool definitions = %+v", requests[0].Tools)
	}
	if len(requests[0].Tools[0].Parameters) == 0 {
		t.Fatal("business Tool schema was not sent to the model")
	}
	var schema map[string]any
	if err := json.Unmarshal(requests[0].Tools[0].Parameters, &schema); err != nil {
		t.Fatalf("Tool parameters are not valid JSON Schema: %v", err)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("Tool schema has no properties: %s", requests[0].Tools[0].Parameters)
	}
	if _, ok := properties["id"]; !ok {
		t.Fatalf("Tool schema omitted input field id: %s", requests[0].Tools[0].Parameters)
	}
	if len(businessTool.received) != 1 || businessTool.received[0] != (lookupInput{ID: 42}) {
		t.Fatalf("business Tool received = %#v", businessTool.received)
	}
	if len(requests[1].Messages) < 4 {
		t.Fatalf("model history did not include assistant ToolCall and tool result: %+v", requests[1].Messages)
	}
	var sawCall, sawToolResult bool
	for _, message := range requests[1].Messages {
		for _, call := range message.ToolCalls {
			if call.ID == "lookup-1" {
				sawCall = true
			}
		}
		if message.Role == ModelRoleTool && message.ToolCallID == "lookup-1" && message.ToolName == "lookup" {
			sawToolResult = true
			if len(message.Content) != 1 {
				t.Fatalf("tool result content = %+v", message.Content)
			}
			var toolResult map[string]any
			if err := json.Unmarshal([]byte(message.Content[0].Text), &toolResult); err != nil {
				t.Fatalf("Tool result is not valid JSON: %v", err)
			}
			if toolResult["id"] != float64(42) || toolResult["name"] != "Ada" {
				t.Fatalf("Tool result = %+v", toolResult)
			}
		}
	}
	if !sawCall || !sawToolResult {
		t.Fatalf("assistant/tool call relationship lost: %+v", requests[1].Messages)
	}
}

func TestRegisteredToolMetadataRemainsStableForRuntime(t *testing.T) {
	tool := &customAgentTool{
		name: "lookup", description: "Original description.", input: struct{}{},
	}
	invoker := &scriptedInvoker{responses: []ModelResponse{
		{ToolCalls: []ToolCall{{ID: "lookup-1", Name: "lookup", Arguments: `{}`}}, FinishReason: "tool_calls"},
		{Content: []ModelPart{{Type: PartText, Text: "done"}}},
	}}
	rt := newAgentTestRuntime(t, NodeConfig{
		ModelName: "review", SystemPrompt: "Use lookup.", Tools: []string{"lookup"},
	}, invoker, []Tool{tool})
	tool.name = "renamed"
	tool.description = "Mutated after Register."

	result, err := rt.Run(newGinContextForAgentTest(t), "agent", Request{
		Messages: []Message{{Role: RoleUser, Content: []Part{{Type: PartText, Text: "Find it."}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result != "done" {
		t.Fatalf("Run result = %#v", result)
	}
	requests := invoker.snapshot()
	if len(requests) != 2 || len(requests[0].Tools) != 1 {
		t.Fatalf("unexpected model requests: %+v", requests)
	}
	if requests[0].Tools[0].Name != "lookup" || requests[0].Tools[0].Description != "Original description." {
		t.Fatalf("registered Tool metadata changed after Register: %+v", requests[0].Tools[0])
	}
}

func TestRunAgentMixedOutputAndBusinessToolErrorStillFails(t *testing.T) {
	rootErr := errors.New("business validation failed")
	result := lessonReviewResult{Score: 80, Summary: "partial", Problems: []string{}}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	invoker := &scriptedInvoker{responses: []ModelResponse{{
		ToolCalls: []ToolCall{
			{ID: "output-mix", Name: runtimeOutputToolName, Arguments: string(raw)},
			{ID: "validate-mix", Name: "validate", Arguments: `{}`},
		},
		FinishReason: "tool_calls",
	}}}
	validation := &customAgentTool{
		name: "validate", description: "Validate the request.", input: struct{}{}, err: rootErr,
	}
	rt := newAgentTestRuntime(t, NodeConfig{
		ModelName: "review", SystemPrompt: "Review.", Tools: []string{"validate"},
	}, invoker, []Tool{validation})

	value, err := rt.Run(newGinContextForAgentTest(t), "agent", Request{
		Messages: []Message{{Role: RoleUser, Content: []Part{{Type: PartText, Text: "Review."}}}},
		Output:   Output[lessonReviewResult](),
	})
	if value != nil || !errors.Is(err, rootErr) {
		t.Fatalf("value=%#v err=%v; business Tool failure must not be hidden by Output", value, err)
	}
}

func TestRunAgentWaitsForParallelToolWorkersAfterPanic(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	invoker := &scriptedInvoker{responses: []ModelResponse{{
		ToolCalls: []ToolCall{
			{ID: "panic-1", Name: "panic_tool", Arguments: `{}`},
			{ID: "slow-1", Name: "slow_tool", Arguments: `{}`},
		},
		FinishReason: "tool_calls",
	}}}
	panicTool := &customAgentTool{
		name: "panic_tool", description: "Panics.", input: struct{}{}, panicValue: "tool panic",
	}
	slowTool := &customAgentTool{
		name: "slow_tool", description: "Waits for a signal.", input: struct{}{},
		started: started, release: release,
	}
	rt := newAgentTestRuntime(t, NodeConfig{
		ModelName: "review", SystemPrompt: "Use the tools.",
		Tools: []string{"panic_tool", "slow_tool"},
	}, invoker, []Tool{panicTool, slowTool})

	runDone := make(chan error, 1)
	go func() {
		_, err := rt.Run(newGinContextForAgentTest(t), "agent", Request{
			Messages: []Message{{Role: RoleUser, Content: []Part{{Type: PartText, Text: "Run both."}}}},
		})
		runDone <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("parallel slow Tool did not start")
	}
	select {
	case err := <-runDone:
		close(release)
		t.Fatalf("Run returned before the other Tool worker completed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-runDone:
		if err == nil || !strings.Contains(err.Error(), "tool panic") {
			t.Fatalf("Run error = %v, want recovered Tool panic", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not finish after releasing the remaining Tool")
	}
}

func TestRunAgentRequiresOutputSubmissionAndPropagatesModelError(t *testing.T) {
	t.Run("invalid output spec", func(t *testing.T) {
		invoker := &scriptedInvoker{}
		rt := newAgentTestRuntime(t, NodeConfig{ModelName: "review", SystemPrompt: "Review."}, invoker, nil)
		result, err := rt.Run(newGinContextForAgentTest(t), "agent", Request{
			Messages: []Message{{Role: RoleUser, Content: []Part{{Type: PartText, Text: "Review."}}}},
			Output:   &OutputSpec{},
		})
		if result != nil || err == nil {
			t.Fatalf("result=%#v err=%v; zero OutputSpec should report an error", result, err)
		}
	})

	t.Run("missing output", func(t *testing.T) {
		invoker := &scriptedInvoker{responses: []ModelResponse{{Content: []ModelPart{{Type: PartText, Text: "I cannot format that."}}}}}
		rt := newAgentTestRuntime(t, NodeConfig{ModelName: "review", SystemPrompt: "Review."}, invoker, nil)
		result, err := rt.Run(newGinContextForAgentTest(t), "agent", Request{
			Messages: []Message{{Role: RoleUser, Content: []Part{{Type: PartText, Text: "Review."}}}},
			Output:   Output[lessonReviewResult](),
		})
		if result != nil || err == nil {
			t.Fatalf("result=%#v err=%v; missing OutputTool must fail", result, err)
		}
	})

	t.Run("malformed output arguments", func(t *testing.T) {
		invoker := &scriptedInvoker{responses: []ModelResponse{{
			ToolCalls: []ToolCall{{
				ID: "output-invalid", Name: runtimeOutputToolName,
				Arguments: `{"score":"not-an-integer"}`,
			}},
			FinishReason: "tool_calls",
		}}}
		rt := newAgentTestRuntime(t, NodeConfig{ModelName: "review", SystemPrompt: "Review."}, invoker, nil)
		result, err := rt.Run(newGinContextForAgentTest(t), "agent", Request{
			Messages: []Message{{Role: RoleUser, Content: []Part{{Type: PartText, Text: "Review."}}}},
			Output:   Output[lessonReviewResult](),
		})
		if result != nil || err == nil {
			t.Fatalf("result=%#v err=%v; malformed OutputTool arguments must fail", result, err)
		}
	})

	t.Run("model error", func(t *testing.T) {
		rootErr := errors.New("gateway unavailable")
		invoker := &scriptedInvoker{errAt: 1, err: rootErr}
		rt := newAgentTestRuntime(t, NodeConfig{ModelName: "review", SystemPrompt: "Review."}, invoker, nil)
		result, err := rt.Run(newGinContextForAgentTest(t), "agent", Request{
			Messages: []Message{{Role: RoleUser, Content: []Part{{Type: PartText, Text: "Review."}}}},
		})
		if result != nil || !errors.Is(err, rootErr) {
			t.Fatalf("result=%#v err=%v; model error chain should be preserved", result, err)
		}
	})
}

func TestRunAgentUsesConfiguredMaxLoopCount(t *testing.T) {
	invoker := &scriptedInvoker{respond: func(request ModelRequest) (ModelResponse, error) {
		return ModelResponse{ToolCalls: []ToolCall{{
			ID:   fmt.Sprintf("lookup-%d", len(request.Messages)),
			Name: "lookup", Arguments: `{"id":17}`,
		}}, FinishReason: "tool_calls"}, nil
	}}
	rt, err := New(Options{
		Config: Config{Agents: map[string]AgentConfig{"agent": {Flow: []NodeConfig{{
			ModelName:    "review",
			SystemPrompt: "Use the lookup tool, then continue.",
			Tools:        []string{"lookup"},
			MaxLoopCount: intPointer(1),
		}}}}},
		Models: map[string]ModelInvoker{"review": invoker},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Register(nil, []Tool{&agentTestTool{}}, nil); err != nil {
		t.Fatal(err)
	}
	_, err = rt.Run(newGinContextForAgentTest(t), "agent", Request{
		Messages: []Message{{Role: RoleUser, Content: []Part{{Type: PartText, Text: "Review 17."}}}},
	})
	if err == nil {
		t.Fatal("Agent should fail when it needs another model round after maxLoopCount")
	}
}

func TestRunAgentDefaultsMaxLoopCountToFive(t *testing.T) {
	invoker := &scriptedInvoker{respond: func(request ModelRequest) (ModelResponse, error) {
		return ModelResponse{ToolCalls: []ToolCall{{
			ID:   fmt.Sprintf("lookup-%d", len(request.Messages)),
			Name: "lookup", Arguments: `{"id":17}`,
		}}, FinishReason: "tool_calls"}, nil
	}}
	rt := newAgentTestRuntime(t, NodeConfig{
		ModelName: "review", SystemPrompt: "Use lookup.", Tools: []string{"lookup"},
	}, invoker, []Tool{&agentTestTool{}})
	_, err := rt.Run(newGinContextForAgentTest(t), "agent", Request{
		Messages: []Message{{Role: RoleUser, Content: []Part{{Type: PartText, Text: "Review 17."}}}},
	})
	if err == nil {
		t.Fatal("Agent should stop when its default five model rounds are exhausted")
	}
	if calls := len(invoker.snapshot()); calls != 5 {
		t.Fatalf("model calls = %d, want default maxLoopCount 5", calls)
	}
}

func TestParentAgentTransfersToChildAndUsesChildOutput(t *testing.T) {
	childOutput := lessonReviewResult{Score: 94, Summary: "Detailed review.", Problems: []string{"one pause was abrupt"}}
	childJSON, err := json.Marshal(childOutput)
	if err != nil {
		t.Fatal(err)
	}
	parentInvoker := &scriptedInvoker{respond: func(request ModelRequest) (ModelResponse, error) {
		if len(request.Tools) != 1 || request.Tools[0].Name != "transfer_to_agent" {
			return ModelResponse{}, errors.New("parent should only have transfer tool")
		}
		return ModelResponse{ToolCalls: []ToolCall{{
			ID: "transfer-1", Name: "transfer_to_agent",
			Arguments: `{"agent_name":"luban_flow_0_sub_0"}`,
		}}, FinishReason: "tool_calls"}, nil
	}}
	childInvoker := &scriptedInvoker{responses: []ModelResponse{{
		ToolCalls:    []ToolCall{{ID: "child-output", Name: runtimeOutputToolName, Arguments: string(childJSON)}},
		FinishReason: "tool_calls",
	}}}
	rt, err := New(Options{
		Config: Config{Agents: map[string]AgentConfig{"agent": {Flow: []NodeConfig{{
			ModelName:    "parent-model",
			SystemPrompt: "Choose a specialist.",
			SubAgents: []SubAgentConfig{{
				ModelName:    "child-model",
				SystemPrompt: "Review the lesson.",
			}},
		}}}}},
		Models: map[string]ModelInvoker{
			"parent-model": parentInvoker,
			"child-model":  childInvoker,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Register(nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	request := Request{
		Messages: []Message{{Role: RoleUser, Content: []Part{{Type: PartText, Text: "Review lesson 17."}}}},
		Output:   Output[lessonReviewResult](),
	}

	result, err := rt.Run(newGinContextForAgentTest(t), "agent", request)
	if err != nil {
		t.Fatal(err)
	}
	if actual, ok := result.(lessonReviewResult); !ok || !reflect.DeepEqual(actual, childOutput) {
		t.Fatalf("result = %#v (%T), want child result %#v", result, result, childOutput)
	}
	parentRequests := parentInvoker.snapshot()
	childRequests := childInvoker.snapshot()
	if len(parentRequests) != 1 || len(childRequests) != 1 {
		t.Fatalf("parent calls=%d child calls=%d", len(parentRequests), len(childRequests))
	}
	for _, definition := range parentRequests[0].Tools {
		if definition.Name == runtimeOutputToolName {
			t.Fatal("parent Agent must not receive OutputTool")
		}
	}
	if len(childRequests[0].Tools) != 1 || childRequests[0].Tools[0].Name != runtimeOutputToolName {
		t.Fatalf("child should receive the shared Output contract: %+v", childRequests[0].Tools)
	}
}

func TestParentAgentWithoutTransferPassesNilToNextFunction(t *testing.T) {
	parentInvoker := &scriptedInvoker{responses: []ModelResponse{{
		Content: []ModelPart{{Type: PartText, Text: "I should not answer the user."}},
	}}}
	gotNil := false
	rt, err := New(Options{
		Config: Config{Agents: map[string]AgentConfig{"agent": {Flow: []NodeConfig{
			{
				ModelName:    "parent-model",
				SystemPrompt: "Choose a specialist.",
				SubAgents: []SubAgentConfig{{
					ModelName:    "child-model",
					SystemPrompt: "Do the work.",
				}},
			},
			{Func: "Fallback"},
		}}}},
		Models: map[string]ModelInvoker{
			"parent-model": parentInvoker,
			"child-model":  &scriptedInvoker{responses: []ModelResponse{{Content: []ModelPart{{Type: PartText, Text: "child"}}}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Register(map[string]any{
		"Fallback": func(_ *gin.Context, value any) string {
			gotNil = value == nil
			return "fallback"
		},
	}, nil, nil); err != nil {
		t.Fatal(err)
	}
	result, err := rt.Run(newGinContextForAgentTest(t), "agent", Request{
		Messages: []Message{{Role: RoleUser, Content: []Part{{Type: PartText, Text: "Choose."}}}},
		Output:   Output[lessonReviewResult](),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !gotNil || result != "fallback" {
		t.Fatalf("gotNil=%t result=%#v", gotNil, result)
	}
	if len(parentInvoker.snapshot()) != 1 {
		t.Fatal("parent should run once")
	}
	for _, tool := range parentInvoker.snapshot()[0].Tools {
		if tool.Name == runtimeOutputToolName {
			t.Fatal("parent selector must not receive the OutputTool")
		}
	}
}

func TestParentAgentPropagatesChildFailure(t *testing.T) {
	childErr := errors.New("child model failed")
	parentInvoker := &scriptedInvoker{responses: []ModelResponse{{
		ToolCalls: []ToolCall{{
			ID: "transfer-1", Name: "transfer_to_agent",
			Arguments: `{"agent_name":"luban_flow_0_sub_0"}`,
		}},
		FinishReason: "tool_calls",
	}}}
	childInvoker := &scriptedInvoker{errAt: 1, err: childErr}
	rt, err := New(Options{
		Config: Config{Agents: map[string]AgentConfig{"agent": {Flow: []NodeConfig{{
			ModelName:    "parent",
			SystemPrompt: "Choose a specialist.",
			SubAgents: []SubAgentConfig{{
				ModelName: "child", SystemPrompt: "Complete the task.",
			}},
		}}}}},
		Models: map[string]ModelInvoker{"parent": parentInvoker, "child": childInvoker},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Register(nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	result, err := rt.Run(newGinContextForAgentTest(t), "agent", Request{
		Messages: []Message{{Role: RoleUser, Content: []Part{{Type: PartText, Text: "Choose."}}}},
	})
	if result != nil || !errors.Is(err, childErr) {
		t.Fatalf("result=%#v err=%v; child failure must propagate", result, err)
	}
}

func TestChildAgentCannotTransferBackToParent(t *testing.T) {
	parentInvoker := &scriptedInvoker{responses: []ModelResponse{{
		ToolCalls: []ToolCall{{
			ID: "transfer-1", Name: "transfer_to_agent",
			Arguments: `{"agent_name":"luban_flow_0_sub_0"}`,
		}},
		FinishReason: "tool_calls",
	}}}
	childInvoker := &scriptedInvoker{responses: []ModelResponse{{
		ToolCalls: []ToolCall{{
			ID: "transfer-back", Name: "transfer_to_agent",
			Arguments: `{"agent_name":"luban_flow_0"}`,
		}},
		FinishReason: "tool_calls",
	}}}
	rt, err := New(Options{
		Config: Config{Agents: map[string]AgentConfig{"agent": {Flow: []NodeConfig{{
			ModelName:    "parent",
			SystemPrompt: "Choose a specialist.",
			SubAgents: []SubAgentConfig{{
				ModelName: "child", SystemPrompt: "Complete the task.",
			}},
		}}}}},
		Models: map[string]ModelInvoker{"parent": parentInvoker, "child": childInvoker},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Register(nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	result, err := rt.Run(newGinContextForAgentTest(t), "agent", Request{
		Messages: []Message{{Role: RoleUser, Content: []Part{{Type: PartText, Text: "Choose."}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := result.(string); !ok {
		t.Fatalf("child result = %#v (%T), want the child's raw string result", result, result)
	}
	if calls := len(parentInvoker.snapshot()); calls != 1 {
		t.Fatalf("parent was invoked %d times; child transfer must not return to parent", calls)
	}
	childRequests := childInvoker.snapshot()
	if len(childRequests) != 1 {
		t.Fatalf("child was invoked %d times, want 1", len(childRequests))
	}
	for _, definition := range childRequests[0].Tools {
		if definition.Name == "transfer_to_agent" {
			t.Fatal("child Agent was given the transfer-to-parent Tool")
		}
	}
}

func TestRunAgentLoadsOnlyConfiguredSkillOnDemand(t *testing.T) {
	output := lessonReviewResult{Score: 90, Summary: "Applied guidance.", Problems: []string{}}
	encoded, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	invoker := &scriptedInvoker{responses: []ModelResponse{
		{ToolCalls: []ToolCall{{
			ID: "skill-1", Name: "skill", Arguments: `{"skill":"review-guide"}`,
		}}, FinishReason: "tool_calls"},
		{ToolCalls: []ToolCall{{
			ID: "output-1", Name: runtimeOutputToolName, Arguments: string(encoded),
		}}, FinishReason: "tool_calls"},
	}}
	rt, err := New(Options{
		Config: Config{Agents: map[string]AgentConfig{"agent": {Flow: []NodeConfig{{
			ModelName:    "review",
			SystemPrompt: "Review the lesson.",
			Skills:       []string{"review-guide"},
		}}}}},
		Models: map[string]ModelInvoker{"review": invoker},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Register(nil, nil, []Skill{{
		Name: "review-guide", Description: "Lesson review guidance.", Content: "Check pacing and clarity.",
	}}); err != nil {
		t.Fatal(err)
	}

	result, err := rt.Run(newGinContextForAgentTest(t), "agent", Request{
		Messages: []Message{{Role: RoleUser, Content: []Part{{Type: PartText, Text: "Review this lesson."}}}},
		Output:   Output[lessonReviewResult](),
	})
	if err != nil {
		t.Fatal(err)
	}
	if actual, ok := result.(lessonReviewResult); !ok || !reflect.DeepEqual(actual, output) {
		t.Fatalf("result = %#v (%T), want %#v", result, result, output)
	}
	requests := invoker.snapshot()
	if len(requests) != 2 {
		t.Fatalf("model requests = %d, want skill load and OutputTool", len(requests))
	}
	if len(requests[0].Tools) != 2 || requests[0].Tools[0].Name != runtimeOutputToolName ||
		requests[0].Tools[1].Name != "skill" {
		t.Fatalf("model did not receive OutputTool and Skill loader: %+v", requests[0].Tools)
	}
	var sawSkillBody bool
	for _, message := range requests[1].Messages {
		for _, part := range message.Content {
			if part.Type == PartText && part.Text == "Check pacing and clarity." {
				sawSkillBody = true
			}
		}
	}
	if !sawSkillBody {
		t.Fatalf("Skill body was not loaded into the next model request: %+v", requests[1].Messages)
	}
}

func newAgentTestRuntime(t *testing.T, node NodeConfig, invoker *scriptedInvoker, tools []Tool) *Runtime {
	t.Helper()
	rt, err := New(Options{
		Config: Config{Agents: map[string]AgentConfig{"agent": {Flow: []NodeConfig{node}}}},
		Models: map[string]ModelInvoker{"review": invoker},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Register(nil, tools, nil); err != nil {
		t.Fatal(err)
	}
	return rt
}

func newGinContextForAgentTest(t *testing.T) *gin.Context {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, "/", nil)
	if err != nil {
		t.Fatal(err)
	}
	return &gin.Context{Request: request}
}
