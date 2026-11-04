package tracegorm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	luban "github.com/lubanAgent/luban/sdk"
)

type modelFunc func(*gin.Context, luban.ModelRequest) (luban.ModelResponse, error)

func (modelFunc) Name() string { return "offline" }
func (f modelFunc) Invoke(ctx *gin.Context, req luban.ModelRequest) (luban.ModelResponse, error) {
	return f(ctx, req)
}

type lookupInput struct {
	ID int `json:"id"`
}
type lookupTool struct{}

func (lookupTool) Name() string        { return "lookup" }
func (lookupTool) Description() string { return "Look up a fake record" }
func (lookupTool) Input() any          { return lookupInput{} }
func (lookupTool) Invoke(_ *gin.Context, input any) (any, error) {
	value, ok := input.(lookupInput)
	if !ok {
		return nil, fmt.Errorf("unexpected input %T", input)
	}
	return value, nil
}

type review struct {
	Summary string `json:"summary"`
}

func request(text string) luban.Request {
	return luban.Request{Messages: []luban.Message{{
		Role: luban.RoleUser, Content: []luban.Part{{Type: luban.PartText, Text: text}},
	}}}
}

func toolsResponse(calls ...luban.ToolCall) luban.ModelResponse {
	return luban.ModelResponse{ToolCalls: calls, FinishReason: "tool_calls", Usage: &luban.ModelUsage{InputTokens: 12, OutputTokens: 3}}
}

func TestRuntimeToolSkillStructuredOutputDatabaseAndViewer(t *testing.T) {
	db := testDB(t, true)
	w := testWriter(t, db, nil)
	fake := modelFunc(func(_ *gin.Context, req luban.ModelRequest) (luban.ModelResponse, error) {
		seen := make(map[string]bool)
		for _, message := range req.Messages {
			if message.Role == luban.ModelRoleTool {
				seen[message.ToolName] = true
			}
		}
		switch {
		case seen["skill"]:
			return toolsResponse(luban.ToolCall{ID: "output", Name: "__runtime_output", Arguments: `{"summary":"审查完成😀"}`}), nil
		case seen["lookup"]:
			return toolsResponse(luban.ToolCall{ID: "skill1", Name: "skill", Arguments: `{"skill":"guide"}`}), nil
		default:
			return toolsResponse(
				luban.ToolCall{ID: "lookup1", Name: "lookup", Arguments: `{"id":1}`},
				luban.ToolCall{ID: "lookup2", Name: "lookup", Arguments: `{"id":2}`},
			), nil
		}
	})
	rt, err := luban.New(luban.Options{
		Config: luban.Config{Agents: map[string]luban.AgentConfig{"review": {Flow: []luban.NodeConfig{
			{Func: "prepare", Description: "准备材料"},
			{ModelName: "offline", SystemPrompt: "完整提示词", Tools: []string{"lookup"}, Skills: []string{"guide"}},
			{Func: "finish"},
		}}}},
		Models: map[string]luban.ModelInvoker{"offline": fake}, TraceWriter: w,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Register(map[string]any{
		"prepare": func(_ *gin.Context, text string) luban.Request {
			req := request(text)
			req.Output = luban.Output[review]()
			return req
		},
		"finish": func(_ *gin.Context, result review) review { return result },
	}, []luban.Tool{lookupTool{}}, []luban.Skill{{Name: "guide", Description: "参考规范", Content: "完整技能正文😀"}}); err != nil {
		t.Fatal(err)
	}
	ctx := &gin.Context{}
	output, err := rt.Run(ctx, "review", "待审材料")
	if err != nil || !reflect.DeepEqual(output, review{Summary: "审查完成😀"}) {
		t.Fatalf("output=%#v err=%v", output, err)
	}
	if err := closeWriter(t, w); err != nil {
		t.Fatal(err)
	}
	events := readEvents(t, db)
	counts, byID := map[string]int{}, map[string]luban.TraceEvent{}
	var raw bytes.Buffer
	for _, e := range events {
		if e.RunID != luban.RunID(ctx) {
			t.Fatalf("foreign event: %+v", e)
		}
		counts[e.Type]++
		byID[e.SpanID] = e
		_ = json.NewEncoder(&raw).Encode(e)
		if e.Type == "skill" && !strings.Contains(string(e.Output), "完整技能正文") {
			t.Fatalf("Skill body lost: %s", e.Output)
		}
		if e.Type == "model" && (!strings.Contains(string(e.Input), "完整提示词") ||
			!strings.Contains(string(e.Output), `"inputTokens":12`)) {
			t.Fatalf("model values lost: %s %s", e.Input, e.Output)
		}
	}
	if counts["model"] != 3 || counts["tool"] != 3 || counts["skill"] != 1 ||
		counts["run"] != 1 || counts["function"] != 2 || len(events) != int(w.Stats().Persisted) {
		t.Fatalf("wrong captured events: %v %+v", counts, w.Stats())
	}
	for _, e := range events {
		if e.Type != "run" {
			if _, exists := byID[e.ParentSpanID]; !exists {
				t.Fatalf("orphan event: %+v", e)
			}
		}
	}
	t.Run("database export parsed by real viewer", func(t *testing.T) {
		if _, err := exec.LookPath("node"); err != nil {
			t.Skip("node is required for the offline viewer check")
		}
		// Read raw stored JSON, rather than using re-encoded Go objects.
		var rows []spanRow
		if err := db.Table(SpanTable(luban.RunID(ctx))).Where("run_id = ?", luban.RunID(ctx)).
			Order("started_at, span_id").Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		raw.Reset()
		for _, row := range rows {
			raw.WriteString(row.EventJSON + "\n")
		}
		parser, _ := filepath.Abs("../trace-viewer/trace-data.js")
		script := `const fs = require("node:fs");
const {parseLogs, buildRun} = require(process.argv[1]);
const parsed = parseLogs(fs.readFileSync(0, "utf8"));
if (parsed.skipped || parsed.groups.size !== 1) throw Error("export rejected");
const view = buildRun([...parsed.groups.values()][0]);
if (view.missingCount || !view.run) throw Error("broken event tree");
process.stdout.write(String(parsed.accepted));`
		command := exec.Command("node", "-e", script, parser)
		command.Stdin = &raw
		got, err := command.CombinedOutput()
		if err != nil || string(got) != fmt.Sprint(len(events)) {
			t.Fatalf("viewer: %s %v", got, err)
		}
	})
}

func TestConcurrentRuntimeBatchRunIsolation(t *testing.T) {
	db := testDB(t, true)
	w := testWriter(t, db, nil)
	fake := modelFunc(func(_ *gin.Context, req luban.ModelRequest) (luban.ModelResponse, error) {
		text := req.Messages[len(req.Messages)-1].Content[0].Text
		if text == "bad" {
			return luban.ModelResponse{}, errors.New("bad item")
		}
		return luban.ModelResponse{Content: []luban.ModelPart{{Type: luban.PartText, Text: text}}}, nil
	})
	rt, err := luban.New(luban.Options{
		Config: luban.Config{Agents: map[string]luban.AgentConfig{"batch": {Flow: []luban.NodeConfig{{
			ModelName: "offline", SystemPrompt: "Echo.",
		}}}}},
		Models: map[string]luban.ModelInvoker{"offline": fake}, TraceWriter: w,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Register(nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			batch := luban.Batch{Concurrency: 3}
			for _, text := range []string{"first", "bad", "last"} {
				batch.Items = append(batch.Items, luban.BatchItem{Messages: request(text).Messages})
			}
			result, err := rt.Run(&gin.Context{}, "batch", batch)
			if err != nil {
				t.Error(err)
				return
			}
			items := result.(luban.BatchResult[string]).Items
			if len(items) != 3 || items[0].Value != "first" || items[1].Err == nil || items[2].Value != "last" {
				t.Errorf("batch changed: %+v", items)
			}
		}()
	}
	wg.Wait()
	if err := closeWriter(t, w); err != nil {
		t.Fatal(err)
	}
	groups := make(map[string]map[string]luban.TraceEvent)
	for _, e := range readEvents(t, db) {
		if groups[e.RunID] == nil {
			groups[e.RunID] = make(map[string]luban.TraceEvent)
		}
		groups[e.RunID][e.SpanID] = e
	}
	if len(groups) != 6 {
		t.Fatalf("Run IDs not isolated: %d", len(groups))
	}
	for _, events := range groups {
		items := 0
		for _, e := range events {
			if e.Type == "batchItem" {
				items++
			}
			if e.Type != "run" {
				if _, ok := events[e.ParentSpanID]; !ok {
					t.Fatalf("cross-run parent: %+v", e)
				}
			}
		}
		if items != 3 {
			t.Fatalf("batch items=%d", items)
		}
	}
}

func TestRuntimeErrorStopPanicAndCancellationStillPersist(t *testing.T) {
	db := testDB(t, true)
	startup, cancelStartup := context.WithCancel(context.Background())
	w, err := New(startup, db, testOptions(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	cancelStartup() // successful New must no longer depend on startup context
	wantErr := errors.New("business failed")
	cancelRequest := func() {}
	rt, err := luban.New(luban.Options{
		Config: luban.Config{Agents: map[string]luban.AgentConfig{
			"error":  {Flow: []luban.NodeConfig{{Func: "error"}}},
			"panic":  {Flow: []luban.NodeConfig{{Func: "panic"}}},
			"stop":   {Flow: []luban.NodeConfig{{Func: "stop"}, {Func: "never"}}},
			"cancel": {Flow: []luban.NodeConfig{{Func: "cancel"}}},
		}}, TraceWriter: w,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Register(map[string]any{
		"error":  func(*gin.Context) error { return wantErr },
		"panic":  func(*gin.Context) { panic("business panic") },
		"stop":   func(ctx *gin.Context) string { luban.Stop(ctx); return "stopped value" },
		"never":  func(*gin.Context) { t.Error("executed after Stop") },
		"cancel": func(*gin.Context) { cancelRequest() },
	}, nil, nil); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"error", "panic", "stop", "cancel"} {
		taskCtx, cancel := context.WithCancel(context.Background())
		cancelRequest = cancel
		ctx := &gin.Context{}
		if err := luban.BindContext(ctx, taskCtx); err != nil {
			t.Fatal(err)
		}
		result, err := rt.Run(ctx, key, nil)
		cancel()
		switch key {
		case "stop":
			if err != nil || result != "stopped value" {
				t.Fatalf("Stop: %v %v", result, err)
			}
		case "error":
			if !errors.Is(err, wantErr) || result != nil {
				t.Fatalf("error: %v %v", result, err)
			}
		case "cancel":
			if !errors.Is(err, context.Canceled) || result != nil {
				t.Fatalf("cancel: %v %v", result, err)
			}
		case "panic":
			if err == nil || result != nil {
				t.Fatalf("panic: %v %v", result, err)
			}
		}
	}
	if err := closeWriter(t, w); err != nil {
		t.Fatal(err)
	}
	var summaries []runRow
	if err := db.Find(&summaries).Error; err != nil || len(summaries) != 4 {
		t.Fatalf("terminal runs missing: %v %v", summaries, err)
	}
	for _, r := range summaries {
		want := "error"
		if r.AgentKey == "stop" {
			want = "stopped"
		}
		if r.Status != want {
			t.Fatalf("wrong terminal state: %+v", r)
		}
	}
}

func TestRuntimeParentTransferAndNoSelection(t *testing.T) {
	for _, transfer := range []bool{false, true} {
		t.Run(fmt.Sprint(transfer), func(t *testing.T) {
			db := testDB(t, true)
			w := testWriter(t, db, nil)
			parent := modelFunc(func(*gin.Context, luban.ModelRequest) (luban.ModelResponse, error) {
				if transfer {
					return toolsResponse(luban.ToolCall{ID: "transfer", Name: "transfer_to_agent", Arguments: `{"agent_name":"luban_flow_0_sub_0"}`}), nil
				}
				return luban.ModelResponse{Content: []luban.ModelPart{{Type: luban.PartText, Text: "no selection"}}}, nil
			})
			child := modelFunc(func(*gin.Context, luban.ModelRequest) (luban.ModelResponse, error) {
				return luban.ModelResponse{Content: []luban.ModelPart{{Type: luban.PartText, Text: "child answer"}}}, nil
			})
			rt, err := luban.New(luban.Options{
				Config: luban.Config{Agents: map[string]luban.AgentConfig{"parent": {Flow: []luban.NodeConfig{{
					ModelName: "parent", SystemPrompt: "Choose.",
					SubAgents: []luban.SubAgentConfig{{ModelName: "child", SystemPrompt: "Answer.", Description: "child"}},
				}}}}},
				Models: map[string]luban.ModelInvoker{"parent": parent, "child": child}, TraceWriter: w,
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := rt.Register(nil, nil, nil); err != nil {
				t.Fatal(err)
			}
			output, err := rt.Run(&gin.Context{}, "parent", request("hello"))
			if err != nil || (!transfer && output != nil) || (transfer && output != "child answer") {
				t.Fatalf("parent result %v %v", output, err)
			}
			if err := closeWriter(t, w); err != nil {
				t.Fatal(err)
			}
			events := readEvents(t, db)
			byID := make(map[string]luban.TraceEvent)
			transfers := 0
			for _, e := range events {
				byID[e.SpanID] = e
			}
			for _, e := range events {
				if e.TransferFromSpanID != "" {
					transfers++
					from, ok := byID[e.TransferFromSpanID]
					if !ok || from.ParentSpanID != e.ParentSpanID {
						t.Fatalf("transfer link lost: %+v", e)
					}
				}
			}
			if transfer && transfers != 1 {
				t.Fatalf("transfers=%d", transfers)
			}
		})
	}
}

func TestDatabaseFailureDoesNotChangeRuntimeResult(t *testing.T) {
	w := startWriter(storeFunc(func(context.Context, []entry) error { return errors.New("DB unavailable") }),
		testOptions(t, func(o *Options) { o.FlushInterval = time.Hour }))
	rt, err := luban.New(luban.Options{
		Config:      luban.Config{Agents: map[string]luban.AgentConfig{"echo": {Flow: []luban.NodeConfig{{Func: "echo"}}}}},
		TraceWriter: w,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Register(map[string]any{"echo": func(_ *gin.Context, value string) string { return value }}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if result, err := rt.Run(&gin.Context{}, "echo", "success"); err != nil || result != "success" {
		t.Fatalf("Trace failure changed business outcome: %v %v", result, err)
	}
	if err := closeWriter(t, w); !errors.Is(err, ErrPersistence) {
		t.Fatalf("persistence failure hidden: %v", err)
	}
}

func TestSkippedFlowEventPersistsWithoutModelOrToolSpans(t *testing.T) {
	db := testDB(t, true)
	w := testWriter(t, db, nil)
	rt, err := luban.New(luban.Options{
		Config: luban.Config{Agents: map[string]luban.AgentConfig{"empty": {Flow: []luban.NodeConfig{
			{Func: "prepare"}, {ModelName: "offline", SystemPrompt: "No call."}, {Func: "finish"},
		}}}},
		Models: map[string]luban.ModelInvoker{"offline": modelFunc(func(*gin.Context, luban.ModelRequest) (luban.ModelResponse, error) {
			t.Error("model invoked for empty input")
			return luban.ModelResponse{}, nil
		})},
		TraceWriter: w,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Register(map[string]any{
		"prepare": func(*gin.Context) luban.Request {
			return luban.Request{Output: luban.Output[review]()}
		},
		"finish": func(_ *gin.Context, value review) review { return value },
	}, nil, nil); err != nil {
		t.Fatal(err)
	}
	result, err := rt.Run(&gin.Context{}, "empty", nil)
	if err != nil || result != (review{}) {
		t.Fatalf("empty result=%#v err=%v", result, err)
	}
	if err := closeWriter(t, w); err != nil {
		t.Fatal(err)
	}
	stored := readShardedEvents(t, db)
	var skipped int
	for _, row := range stored {
		var event luban.TraceEvent
		if err := json.Unmarshal([]byte(row.EventJSON), &event); err != nil {
			t.Fatal(err)
		}
		if event.Type == "model" || event.Type == "tool" || event.Type == "skill" {
			t.Fatalf("phantom model/tool/skill event: %+v", event)
		}
		if event.Status == "skipped" {
			skipped++
			if event.SkipReason != "empty_input" || event.Type != "agent" || event.FlowIndex == nil || *event.FlowIndex != 1 {
				t.Fatalf("invalid skip metadata: %+v", event)
			}
		}
	}
	if skipped != 1 || len(stored) != 4 {
		t.Fatalf("skipped=%d spans=%d", skipped, len(stored))
	}
}
