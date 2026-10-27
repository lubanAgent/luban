package admissions_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
	"github.com/lubanAgent/luban/agents/admissions"
	"github.com/lubanAgent/luban/agents/admissions/strategy"
	"github.com/lubanAgent/luban/server"
)

type memoryRepository struct {
	material strategyagent.TaskMaterial
	commits  int
	artifact *strategyagent.Artifact
	draft    strategyagent.Draft
}

func (m *memoryRepository) Load(_ context.Context, claim strategyagent.ClaimedTask) (strategyagent.TaskMaterial, error) {
	if claim.AttemptToken != "private-claim-token" {
		return strategyagent.TaskMaterial{}, strategyagent.ErrLeaseLost
	}
	material := m.material
	material.Claim = claim
	return material, nil
}
func (m *memoryRepository) Commit(_ context.Context, material strategyagent.TaskMaterial, draft strategyagent.Draft, artifact *strategyagent.Artifact, result strategyagent.TurnResult) (strategyagent.Reply, error) {
	if material.Claim.AttemptToken != "private-claim-token" {
		return strategyagent.Reply{}, strategyagent.ErrLeaseLost
	}
	m.commits++
	m.artifact = artifact
	m.draft = draft
	return strategyagent.Reply{TaskID: material.Claim.TaskID, Outcome: result.Outcome, Reply: result.Reply, RequirementsRevision: 1, ProcessedMessageSeq: 1}, nil
}
func (*memoryRepository) Claim(context.Context, string) (strategyagent.ClaimedTask, bool, error) {
	return strategyagent.ClaimedTask{}, false, nil
}
func (*memoryRepository) Renew(context.Context, strategyagent.ClaimedTask) error       { return nil }
func (*memoryRepository) Fail(context.Context, strategyagent.ClaimedTask, error) error { return nil }
func (*memoryRepository) Reap(context.Context) error                                   { return nil }

type chatModel struct {
	t             *testing.T
	calls         int
	stale         bool
	finalRevision int64
}

func (m *chatModel) Do(r *http.Request) (*http.Response, error) {
	raw, _ := io.ReadAll(r.Body)
	if bytes.Contains(raw, []byte("private-claim-token")) {
		m.t.Fatal("claim token leaked into model request")
	}
	var request struct {
		Tools []struct {
			Function struct {
				Name       string          `json:"name"`
				Parameters json.RawMessage `json:"parameters"`
			} `json:"function"`
		} `json:"tools"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		return nil, err
	}
	m.calls++
	if m.calls == 1 {
		if len(request.Tools) != 6 {
			m.t.Fatalf("tools = %d, want four business + skill + OutputTool", len(request.Tools))
		}
		for _, tool := range request.Tools {
			if tool.Function.Name == "editStrategyDraft" && (!bytes.Contains(tool.Function.Parameters, []byte(`"$defs"`)) || len(tool.Function.Parameters) > 100000) {
				m.t.Fatalf("recursive Tool Schema = %d bytes", len(tool.Function.Parameters))
			}
		}
		if !strings.Contains(request.Messages[1].Content, "顺话原则") || !strings.Contains(request.Messages[1].Content, "当前任务的检查与修复") {
			m.t.Fatal("complete chat knowledge was not sent")
		}
		return toolResponse("edit", "editStrategyDraft", `{"expectedRevision":0,"requirements":{"stage1Brief":"选择地区","stage2Brief":"数学分0到100","stage3Brief":"推荐公办学校","openQuestions":[],"explicitRules":[{"text":"按数学分推荐公办学校","sourceMessageSeq":1}],"coveredMessageSeq":1}}`), nil
	}
	if m.calls == 2 {
		return toolResponse("check", "checkStrategyDraft", `{"expectedRevision":1}`), nil
	}
	if m.calls == 3 {
		last := request.Messages[len(request.Messages)-1]
		var checked strategyagent.CheckResult
		if json.Unmarshal([]byte(last.Content), &checked) != nil || !checked.Passed || checked.CheckID == "" {
			m.t.Fatalf("check result = %s", last.Content)
		}
		if m.stale {
			checked.CheckID = "forged"
		}
		body, _ := json.Marshal(strategyagent.TurnResult{Outcome: "readyToConfirm", Reply: "需求已整理，请确认。", DraftRevision: 1, CheckID: checked.CheckID})
		return toolResponse("output", "__runtime_output", string(body)), nil
	}
	return nil, fmt.Errorf("unexpected extra model call")
}

func toolResponse(id, name, args string) *http.Response {
	raw, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": nil, "tool_calls": []any{map[string]any{"id": id, "type": "function", "function": map[string]any{"name": name, "arguments": args}}}}, "finish_reason": "tool_calls"}}})
	return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(raw))}
}

func assets(t *testing.T) fstest.MapFS {
	t.Helper()
	result := fstest.MapFS{"conf/luban.yaml": {Data: []byte("llm:\n  admissions:\n    - baseUrl: https://fake.test/v1\n      apiKey: fake-key\n")}}
	for _, name := range []string{"agent.json", "skills/requirementClarify/SKILL.md", "skills/formAndEligibility/SKILL.md", "skills/scoreCalculation/SKILL.md", "skills/schoolRecommendation/SKILL.md", "skills/resultDisplay/SKILL.md", "skills/strategyCheck/SKILL.md"} {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		result["agents/admissions/"+name] = &fstest.MapFile{Data: raw}
	}
	return result
}

func TestSingleAgentChatRunsToolsCheckAndConcreteOutput(t *testing.T) {
	for _, stale := range []bool{false, true} {
		t.Run(fmt.Sprintf("stale=%v", stale), func(t *testing.T) {
			model := &chatModel{t: t, stale: stale}
			trace := &bytes.Buffer{}
			app, err := server.New(server.Options{Assets: assets(t), HTTPDoer: model, Stdout: trace, Stderr: io.Discard})
			if err != nil {
				t.Fatal(err)
			}
			defer app.Close()
			repository := &memoryRepository{material: strategyagent.TaskMaterial{ConversationID: 1, StrategyID: 1, SchoolID: 1, MessageSeq: 1, History: []strategyagent.HistoryMessage{{Seq: 1, Role: "user", Content: "按数学分推荐公办学校"}}, Draft: strategyagent.Draft{ID: "draft-1", SchemaVersion: 1}, Snapshot: strategyagent.Snapshot{ID: "snapshot-1"}}}
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			admissions.BindTaskRepository(ctx, repository)
			output, err := app.Runtime().Run(ctx, "admissions", strategyagent.ClaimedTask{Kind: "chat", TaskID: 1, AttemptToken: "private-claim-token"})
			if stale {
				if err == nil || repository.commits != 0 {
					t.Fatalf("forged check committed: %+v, %v", output, err)
				}
			} else {
				result, ok := output.(strategyagent.Reply)
				if err != nil || !ok || result.Outcome != "readyToConfirm" || repository.commits != 1 {
					t.Fatalf("single Agent result = %+v, %v", output, err)
				}
			}
			if model.calls != 3 {
				t.Fatalf("model rounds = %d", model.calls)
			}
			if strings.Contains(trace.String(), "private-claim-token") || strings.Contains(trace.String(), "fake-key") {
				t.Fatal("secret leaked into Trace")
			}
		})
	}
}

func TestDirectHTTPCannotForgeClaimedTask(t *testing.T) {
	app, err := server.New(server.Options{Assets: assets(t), HTTPDoer: &chatModel{t: t}, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	_, err = app.Runtime().Run(ctx, "admissions", strategyagent.ClaimedTask{Kind: "chat", TaskID: 1})
	if err == nil || !strings.Contains(err.Error(), "authorized task executor") {
		t.Fatalf("untrusted task accepted: %v", err)
	}
}

type generationModel struct {
	t     *testing.T
	calls int
}

func (m *generationModel) Do(r *http.Request) (*http.Response, error) {
	raw, _ := io.ReadAll(r.Body)
	if bytes.Contains(raw, []byte("private-claim-token")) {
		m.t.Fatal("claim token leaked into generation request")
	}
	var request struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		return nil, err
	}
	m.calls++
	if m.calls == 1 {
		return toolResponse("check", "checkStrategyDraft", `{"expectedRevision":1}`), nil
	}
	if m.calls == 2 {
		last := request.Messages[len(request.Messages)-1]
		if last.Role != "tool" {
			m.t.Fatalf("check output role = %q", last.Role)
		}
		var checked strategyagent.CheckResult
		if err := json.Unmarshal([]byte(last.Content), &checked); err != nil || !checked.Passed || checked.CheckID == "" {
			m.t.Fatalf("generation check result = %s, %v", last.Content, err)
		}
		body, _ := json.Marshal(strategyagent.TurnResult{
			Outcome: "generated", Reply: "策略已生成。", DraftRevision: 1, CheckID: checked.CheckID,
		})
		return toolResponse("output", "__runtime_output", string(body)), nil
	}
	return nil, fmt.Errorf("unexpected generation model round %d", m.calls)
}

func TestSingleAgentGeneratesCheckedArtifact(t *testing.T) {
	model := &generationModel{t: t}
	trace := &bytes.Buffer{}
	app, err := server.New(server.Options{Assets: assets(t), HTTPDoer: model, Stdout: trace, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	spec, snapshot := strategyagentTestDomain()
	repository := &memoryRepository{material: strategyagent.TaskMaterial{
		ConversationID:       7,
		StrategyID:           9,
		SchoolID:             17,
		RequirementsRevision: 1,
		MessageSeq:           1,
		Draft: strategyagent.Draft{
			ID: "draft-7", Revision: 1, SchemaVersion: 1, Requirements: strategyagentRequirement(), Spec: spec,
		},
		Snapshot: snapshot,
	}}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	admissions.BindTaskRepository(ctx, repository)
	output, err := app.Runtime().Run(ctx, "admissions", strategyagent.ClaimedTask{
		Kind: "generate", TaskID: 88, AttemptToken: "private-claim-token",
	})
	result, ok := output.(strategyagent.Reply)
	if err != nil || !ok || result.Outcome != "generated" || repository.commits != 1 {
		t.Fatalf("generated result = %+v, %v; commits=%d", output, err, repository.commits)
	}
	if repository.artifact == nil || repository.artifact.SnapshotHash == "" || len(repository.artifact.Lookups) != 1 {
		t.Fatalf("Agent did not pass a snapshot-bound checked artifact to Finish")
	}
	if model.calls != 2 {
		t.Fatalf("model rounds = %d", model.calls)
	}
	if bytes.Contains(trace.Bytes(), []byte("private-claim-token")) || bytes.Contains(trace.Bytes(), []byte("fake-key")) {
		t.Fatal("generation credential leaked into Trace")
	}
}

type configurationGenerationModel struct {
	t     *testing.T
	spec  strategyagent.StrategySpec
	calls int
}

func (m *configurationGenerationModel) Do(r *http.Request) (*http.Response, error) {
	raw, _ := io.ReadAll(r.Body)
	if bytes.Contains(raw, []byte("private-claim-token")) {
		m.t.Fatal("claim token leaked into generation request")
	}
	var request struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		return nil, err
	}
	m.calls++

	edits := []strategyagent.EditDraftInput{
		{Form: m.spec.Form},
		{Scoring: m.spec.Scoring},
		{Recommendation: m.spec.Recommendation},
		{Display: m.spec.Display},
	}
	if m.calls <= len(edits) {
		edit := edits[m.calls-1]
		edit.ExpectedRevision = int64(m.calls - 1)
		args, err := json.Marshal(edit)
		if err != nil {
			return nil, err
		}
		return toolResponse(fmt.Sprintf("edit-%d", m.calls), "editStrategyDraft", string(args)), nil
	}
	if m.calls == len(edits)+1 {
		return toolResponse("check", "checkStrategyDraft", `{"expectedRevision":4}`), nil
	}
	if m.calls == len(edits)+2 {
		last := request.Messages[len(request.Messages)-1]
		if last.Role != "tool" {
			m.t.Fatalf("check output role = %q", last.Role)
		}
		var checked strategyagent.CheckResult
		if err := json.Unmarshal([]byte(last.Content), &checked); err != nil || !checked.Passed || checked.CheckID == "" || checked.ArtifactHash == "" {
			m.t.Fatalf("generation check result = %s", last.Content)
		}
		body, _ := json.Marshal(strategyagent.TurnResult{
			Outcome: "generated", Reply: "策略配置已生成。", DraftRevision: checked.Revision, CheckID: checked.CheckID,
		})
		return toolResponse("output", "__runtime_output", string(body)), nil
	}
	return nil, fmt.Errorf("unexpected configuration generation model round %d", m.calls)
}

func TestSingleAgentBuildsConfigurationFromConfirmedRequirements(t *testing.T) {
	spec, snapshot := strategyagentTestDomain()
	model := &configurationGenerationModel{t: t, spec: spec}
	trace := &bytes.Buffer{}
	app, err := server.New(server.Options{Assets: assets(t), HTTPDoer: model, Stdout: trace, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	repository := &memoryRepository{material: strategyagent.TaskMaterial{
		ConversationID:       7,
		StrategyID:           9,
		SchoolID:             17,
		RequirementsRevision: 1,
		MessageSeq:           1,
		Draft: strategyagent.Draft{
			ID: "draft-7", Revision: 0, SchemaVersion: 1, Requirements: strategyagentRequirement(),
		},
		Snapshot: snapshot,
	}}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	admissions.BindTaskRepository(ctx, repository)
	output, err := app.Runtime().Run(ctx, "admissions", strategyagent.ClaimedTask{
		Kind: "generate", TaskID: 88, AttemptToken: "private-claim-token",
	})
	result, ok := output.(strategyagent.Reply)
	if err != nil || !ok || result.Outcome != "generated" || repository.commits != 1 {
		t.Fatalf("generated result = %+v, %v; commits=%d", output, err, repository.commits)
	}
	if model.calls != 6 {
		t.Fatalf("model rounds = %d, want four config edits, one check and one final submission", model.calls)
	}
	if repository.draft.Revision != 4 || repository.draft.Spec.Form == nil || repository.draft.Spec.Scoring == nil ||
		repository.draft.Spec.Recommendation == nil || repository.draft.Spec.Display == nil {
		t.Fatalf("Agent did not build all configuration sections: %+v", repository.draft)
	}
	artifact := repository.artifact
	if artifact == nil || artifact.SnapshotHash == "" || len(artifact.Lookups) != 1 || len(artifact.Lookups[0].Entries) != 101 {
		t.Fatalf("Agent did not submit the compiled configuration and school lookup: %+v", artifact)
	}
	if len(artifact.Intake) == 0 || len(artifact.Eligibility) == 0 || len(artifact.ScoreInput) == 0 ||
		len(artifact.ScoreCalc) == 0 || len(artifact.DecisionTree) == 0 || len(artifact.ReportDisplay) == 0 {
		t.Fatalf("compiled configuration is incomplete: %+v", artifact)
	}
	if artifact.Lookups[0].Entries[75].Key != 75 || len(artifact.Lookups[0].Entries[75].Tiers[0].Schools) != 1 ||
		artifact.Lookups[0].Entries[75].Tiers[0].Schools[0].Name != "学校A" ||
		len(artifact.Lookups[0].Entries[69].Tiers[0].Schools) != 0 {
		t.Fatalf("compiled school lookup does not match the configured score range: %+v", artifact.Lookups[0].Entries)
	}
	if bytes.Contains(trace.Bytes(), []byte("private-claim-token")) || bytes.Contains(trace.Bytes(), []byte("fake-key")) {
		t.Fatal("generation credential leaked into Trace")
	}
}

func strategyagentTestDomain() (strategyagent.StrategySpec, strategyagent.Snapshot) {
	form := &strategyagent.FormSpec{}
	form.Intake.Fields = []strategyagent.Field{{
		Key: "region", Label: "地区", Type: "enum", Required: true,
		OptionsEnum: []strategyagent.EnumOption{{ID: "a", Label: "A区"}, {ID: "b", Label: "B区"}},
	}}
	form.Eligibility.DefaultAction = "pass"
	scoring := &strategyagent.ScoringSpec{ScoreCalc: []strategyagent.Formula{{
		Name: "default", Type: "weighted", Inputs: []strategyagent.FormulaInput{{Field: "math"}}, OutputKey: "user_score",
	}}}
	scoring.ScoreInput.Fields = []strategyagent.Field{{
		Key: "math", Label: "数学", Type: "number", Required: true, Min: func() *float64 { value := 0.0; return &value }(), Max: func() *float64 { value := 100.0; return &value }(),
	}}
	recommendation := &strategyagent.RecommendationSpec{
		DecisionTree: strategyagent.DecisionTree{RootNodeID: "calc", Nodes: map[string]strategyagent.DecisionNode{
			"calc":      {Type: "calc_score", Name: "算分", ScoreCalcRef: "default", NodeIDs: []string{"recommend"}},
			"recommend": {Type: "recommend", Name: "第三批次", RenderType: "batchRecommend"},
		}},
		Paths: []strategyagent.RecommendationPath{{
			RecommendNodeID: "recommend", Mode: "match", MatchKey: "score", ScoreColumn: "score",
			SchoolScope: &strategyagent.Condition{Op: "eq", Field: "type", Value: json.RawMessage(`"public"`)},
			Tiers:       []strategyagent.Tier{{Name: "冲", Range: []float64{0, 10}, Count: 1, Sort: "asc"}},
		}},
	}
	display := &strategyagent.DisplaySpec{Sections: []strategyagent.DisplaySection{{
		Type: "batchRecommend", Title: "第三批次", SortOrder: 1, RecommendNodeID: "recommend",
		LookupTableRef: "lookup_recommend", RenderType: "batchRecommend",
	}}}
	snapshot := strategyagent.Snapshot{
		ID: "snapshot-1",
		Schools: []strategyagent.School{{ID: 1, Name: "学校A", Fields: map[string]strategyagent.Literal{
			"type": json.RawMessage(`"public"`), "score": json.RawMessage(`80`),
		}}},
		SchoolFields: map[string]string{"type": "string", "score": "number"},
		ScoreColumns: []string{"score"},
	}
	return strategyagent.StrategySpec{Form: form, Scoring: scoring, Recommendation: recommendation, Display: display}, snapshot
}

func strategyagentRequirement() strategyagent.RequirementCard {
	return strategyagent.RequirementCard{
		Stage1Brief: "选择地区", Stage2Brief: "数学0到100分，直接按数学分算", Stage3Brief: "公办学校冲稳保",
		CoveredMessageSeq: 1, ExplicitRules: []strategyagent.SourceRule{{Text: "按数学分算", SourceMessageSeq: 1}},
	}
}
