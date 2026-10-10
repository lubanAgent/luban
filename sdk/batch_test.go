package luban

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type batchTestInvoker struct {
	mu           sync.Mutex
	active       int
	maxActive    int
	calls        int
	failText     string
	delay        time.Duration
	structOutput bool
}

type batchContextInput struct {
	Item string `json:"item"`
}

type batchContextTool struct {
	mu      sync.Mutex
	visited map[string]bool
}

func (*batchContextTool) Name() string        { return "context_probe" }
func (*batchContextTool) Description() string { return "Check isolated item context." }
func (*batchContextTool) Input() any          { return batchContextInput{} }
func (tool *batchContextTool) Invoke(ctx *gin.Context, input any) (any, error) {
	value := input.(batchContextInput)
	if _, exists := ctx.Get("batch-item"); exists {
		return nil, fmt.Errorf("Context data from another item leaked")
	}
	ctx.Set("batch-item", value.Item)
	tool.mu.Lock()
	tool.visited[value.Item] = true
	tool.mu.Unlock()
	return value.Item, nil
}

type batchContextInvoker struct{}

func (batchContextInvoker) Name() string { return "batch-context" }
func (batchContextInvoker) Invoke(_ *gin.Context, request ModelRequest) (ModelResponse, error) {
	item := lastUserText(request)
	for _, message := range request.Messages {
		if message.Role == ModelRoleTool {
			return ModelResponse{Content: []ModelPart{{Type: PartText, Text: "done:" + item}}}, nil
		}
	}
	raw, err := json.Marshal(batchContextInput{Item: item})
	if err != nil {
		return ModelResponse{}, err
	}
	return ModelResponse{ToolCalls: []ToolCall{{
		ID: "probe-" + item, Name: "context_probe", Arguments: string(raw),
	}}, FinishReason: "tool_calls"}, nil
}

func (*batchTestInvoker) Name() string { return "batch-test" }

func (m *batchTestInvoker) Invoke(_ *gin.Context, request ModelRequest) (ModelResponse, error) {
	text := lastUserText(request)
	m.mu.Lock()
	m.calls++
	callID := m.calls
	m.active++
	if m.active > m.maxActive {
		m.maxActive = m.active
	}
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.active--
		m.mu.Unlock()
	}()

	if m.delay > 0 {
		time.Sleep(m.delay)
	}
	if text == m.failText {
		return ModelResponse{}, fmt.Errorf("item %q failed", text)
	}
	if m.structOutput {
		output := lessonReviewResult{Score: len(text), Summary: text, Problems: []string{}}
		raw, err := json.Marshal(output)
		if err != nil {
			return ModelResponse{}, err
		}
		return ModelResponse{ToolCalls: []ToolCall{{
			ID: fmt.Sprintf("output-%d", callID), Name: runtimeOutputToolName, Arguments: string(raw),
		}}, FinishReason: "tool_calls"}, nil
	}
	return ModelResponse{Content: []ModelPart{{Type: PartText, Text: "review:" + text}}}, nil
}

func (m *batchTestInvoker) stats() (calls, maxActive int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls, m.maxActive
}

func TestRunBatchUsesConcurrencyLimitAndPreservesItemOrder(t *testing.T) {
	invoker := &batchTestInvoker{delay: 10 * time.Millisecond}
	rt, err := New(Options{
		Config: Config{Agents: map[string]AgentConfig{"agent": {Flow: []NodeConfig{{
			ModelName:    "review",
			SystemPrompt: "Review the item.",
		}}}}},
		Models: map[string]ModelInvoker{"review": invoker},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Register(nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	batch := Batch{Concurrency: 4}
	for i := 0; i < 20; i++ {
		batch.Items = append(batch.Items, BatchItem{Messages: []Message{{
			Role: RoleUser, Content: []Part{{Type: PartText, Text: fmt.Sprintf("item-%02d", i)}},
		}}})
	}
	result, err := rt.Run(newBatchGinContext(t), "agent", batch)
	if err != nil {
		t.Fatal(err)
	}
	batchResult, ok := result.(BatchResult[string])
	if !ok {
		t.Fatalf("result type = %T, want BatchResult[string]", result)
	}
	if len(batchResult.Items) != len(batch.Items) {
		t.Fatalf("result count = %d, want %d", len(batchResult.Items), len(batch.Items))
	}
	for i, item := range batchResult.Items {
		want := fmt.Sprintf("review:item-%02d", i)
		if item.Err != nil || item.Value != want {
			t.Fatalf("item[%d] = %+v, want value %q and nil error", i, item, want)
		}
	}
	calls, maxActive := invoker.stats()
	if calls != len(batch.Items) {
		t.Fatalf("model calls = %d, want %d", calls, len(batch.Items))
	}
	if maxActive < 2 || maxActive > batch.Concurrency {
		t.Fatalf("maximum concurrent model calls = %d, want between 2 and %d", maxActive, batch.Concurrency)
	}
}

func TestRunBatchRetainsItemErrorsAndUsesConcreteOutputType(t *testing.T) {
	invoker := &batchTestInvoker{failText: "bad-item", structOutput: true}
	rt, err := New(Options{
		Config: Config{Agents: map[string]AgentConfig{"agent": {Flow: []NodeConfig{{
			ModelName:    "review",
			SystemPrompt: "Review the item.",
		}}}}},
		Models: map[string]ModelInvoker{"review": invoker},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Register(nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	batch := Batch{
		Concurrency: 2,
		Output:      Output[lessonReviewResult](),
		Items: []BatchItem{
			{Messages: []Message{{Role: RoleUser, Content: []Part{{Type: PartText, Text: "good-one"}}}}},
			{Messages: []Message{{Role: RoleUser, Content: []Part{{Type: PartText, Text: "bad-item"}}}}},
			{Messages: []Message{{Role: RoleUser, Content: []Part{{Type: PartText, Text: "good-three"}}}}},
		},
	}
	result, err := rt.Run(newBatchGinContext(t), "agent", batch)
	if err != nil {
		t.Fatal(err)
	}
	batchResult, ok := result.(BatchResult[lessonReviewResult])
	if !ok {
		t.Fatalf("result type = %T, want BatchResult[lessonReviewResult]", result)
	}
	if len(batchResult.Items) != 3 {
		t.Fatalf("item count = %d", len(batchResult.Items))
	}
	if batchResult.Items[0].Err != nil || batchResult.Items[0].Value.Summary != "good-one" {
		t.Fatalf("first item = %+v", batchResult.Items[0])
	}
	if batchResult.Items[1].Err == nil || !strings.Contains(batchResult.Items[1].Err.Error(), "bad-item") {
		t.Fatalf("failed item did not retain its error: %+v", batchResult.Items[1])
	}
	if batchResult.Items[2].Err != nil || batchResult.Items[2].Value.Summary != "good-three" {
		t.Fatalf("third item = %+v", batchResult.Items[2])
	}
}

func TestRunBatchRejectsEmptyAndNegativeConcurrency(t *testing.T) {
	invoker := &batchTestInvoker{}
	rt, err := New(Options{
		Config: Config{Agents: map[string]AgentConfig{"agent": {Flow: []NodeConfig{{
			ModelName: "review", SystemPrompt: "Review.",
		}}}}},
		Models: map[string]ModelInvoker{"review": invoker},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Register(nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	_, err = rt.Run(newBatchGinContext(t), "agent", Batch{})
	if !errors.Is(err, ErrEmptyBatch) {
		t.Fatalf("empty batch error = %v, want ErrEmptyBatch", err)
	}
	if calls, _ := invoker.stats(); calls != 0 {
		t.Fatalf("empty batch made %d model calls", calls)
	}

	_, err = rt.Run(newBatchGinContext(t), "agent", Batch{
		Concurrency: -1,
		Items: []BatchItem{{Messages: []Message{{
			Role: RoleUser, Content: []Part{{Type: PartText, Text: "item"}},
		}}}},
	})
	if err == nil {
		t.Fatal("negative concurrency should fail")
	}
}

func TestRunBatchDefaultsConcurrencyToTen(t *testing.T) {
	invoker := &batchTestInvoker{delay: 10 * time.Millisecond}
	rt, err := New(Options{
		Config: Config{Agents: map[string]AgentConfig{"agent": {Flow: []NodeConfig{{
			ModelName: "review", SystemPrompt: "Review the item.",
		}}}}},
		Models: map[string]ModelInvoker{"review": invoker},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Register(nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	batch := Batch{}
	for index := 0; index < 24; index++ {
		item := fmt.Sprintf("item-%02d", index)
		batch.Items = append(batch.Items, BatchItem{Messages: []Message{{
			Role: RoleUser, Content: []Part{{Type: PartText, Text: item}},
		}}})
	}
	if _, err := rt.Run(newBatchGinContext(t), "agent", batch); err != nil {
		t.Fatal(err)
	}
	calls, maximum := invoker.stats()
	if calls != len(batch.Items) || maximum < 2 || maximum > 10 {
		t.Fatalf("model calls=%d max active=%d; want %d calls and concurrency in [2,10]", calls, maximum, len(batch.Items))
	}
}

func TestRunBatchIsolatesItemContexts(t *testing.T) {
	tool := &batchContextTool{visited: make(map[string]bool)}
	rt, err := New(Options{
		Config: Config{Agents: map[string]AgentConfig{"agent": {Flow: []NodeConfig{{
			ModelName: "review", SystemPrompt: "Check each item.", Tools: []string{"context_probe"},
		}}}}},
		Models: map[string]ModelInvoker{"review": batchContextInvoker{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Register(nil, []Tool{tool}, nil); err != nil {
		t.Fatal(err)
	}
	batch := Batch{}
	for index := 0; index < 24; index++ {
		item := fmt.Sprintf("item-%02d", index)
		batch.Items = append(batch.Items, BatchItem{Messages: []Message{{
			Role: RoleUser, Content: []Part{{Type: PartText, Text: item}},
		}}})
	}
	result, err := rt.Run(newBatchGinContext(t), "agent", batch)
	if err != nil {
		t.Fatal(err)
	}
	batchResult, ok := result.(BatchResult[string])
	if !ok || len(batchResult.Items) != len(batch.Items) {
		t.Fatalf("batch result = %#v (%T)", result, result)
	}
	for index, item := range batchResult.Items {
		if item.Err != nil || item.Value != fmt.Sprintf("done:item-%02d", index) {
			t.Fatalf("item[%d] = %+v", index, item)
		}
	}
	if len(tool.visited) != len(batch.Items) {
		t.Fatalf("Tool visited %d item contexts, want %d", len(tool.visited), len(batch.Items))
	}
}

func lastUserText(request ModelRequest) string {
	for index := len(request.Messages) - 1; index >= 0; index-- {
		message := request.Messages[index]
		if message.Role != ModelRoleUser {
			continue
		}
		var text strings.Builder
		for _, part := range message.Content {
			if part.Type == PartText {
				text.WriteString(part.Text)
			}
		}
		return text.String()
	}
	return ""
}

func newBatchGinContext(t *testing.T) *gin.Context {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, "/", nil)
	if err != nil {
		t.Fatal(err)
	}
	return &gin.Context{Request: request}
}
