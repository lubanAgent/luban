package admissions_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lubanAgent/luban/agents/admissions"
	strategyagent "github.com/lubanAgent/luban/agents/admissions/strategy"
	"github.com/lubanAgent/luban/models"
	"github.com/lubanAgent/luban/models/strategyrepo"
	luban "github.com/lubanAgent/luban/sdk"
	"github.com/lubanAgent/luban/server"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type workflowModel struct {
	t     *testing.T
	spec  strategyagent.StrategySpec
	calls int
}

func (m *workflowModel) Do(request *http.Request) (*http.Response, error) {
	raw, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	if bytes.Contains(raw, []byte("private-")) {
		m.t.Fatal("task attempt token leaked into the model request")
	}
	var body struct {
		Tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	m.calls++

	switch m.calls {
	case 1:
		names := make(map[string]bool, len(body.Tools))
		for _, tool := range body.Tools {
			names[tool.Function.Name] = true
		}
		for _, name := range []string{"queryBusinessData", "editStrategyDraft", "checkStrategyDraft", "simulateStudent", "skill", "__runtime_output"} {
			if !names[name] {
				m.t.Fatalf("Agent tools missing %q: %v", name, names)
			}
		}
		if !strings.Contains(string(raw), "收集姓名、手机号和所在区") {
			m.t.Fatal("chat request did not include the submitted user requirement")
		}
		return toolResponse("catalog", "queryBusinessData", `{"resource":"catalog"}`), nil
	case 2:
		last := body.Messages[len(body.Messages)-1]
		if last.Role != "tool" || !strings.Contains(last.Content, "districts") {
			m.t.Fatalf("catalog result was not returned to the model: %+v", last)
		}
		requirements := strategyagent.RequirementCard{
			Stage1Brief:       "收集姓名、手机号和所在区",
			Stage2Brief:       "数学成绩范围0到100分，按数学分计算",
			Stage3Brief:       "按数学分差冲稳保，每档推荐1所公办学校",
			OpenQuestions:     []string{},
			ExplicitRules:     []strategyagent.SourceRule{{Text: "每档推荐1所公办学校", SourceMessageSeq: 1}},
			CoveredMessageSeq: 1,
		}
		args, _ := json.Marshal(strategyagent.EditDraftInput{Requirements: &requirements})
		return toolResponse("requirements", "editStrategyDraft", string(args)), nil
	case 3:
		return toolResponse("requirements-check", "checkStrategyDraft", `{"expectedRevision":1}`), nil
	case 4:
		checked := lastToolResult[strategyagent.CheckResult](m.t, body.Messages)
		if !checked.Passed || checked.CheckID == "" {
			m.t.Fatalf("chat requirements were not approved: %+v", checked)
		}
		output, _ := json.Marshal(strategyagent.TurnResult{
			Outcome: "readyToConfirm", Reply: "需求已整理，请确认后生成配置。",
			DraftRevision: checked.Revision, CheckID: checked.CheckID,
		})
		return toolResponse("chat-output", "__runtime_output", string(output)), nil
	case 5:
		if !strings.Contains(string(raw), "每档推荐1所公办学校") {
			m.t.Fatal("generation did not receive the confirmed chat requirements")
		}
		for _, name := range []string{
			"requirementClarify", "formAndEligibility", "scoreCalculation",
			"schoolRecommendation", "resultDisplay", "strategyCheck",
		} {
			if !strings.Contains(string(raw), name) {
				m.t.Fatalf("generation request did not include Skill %q", name)
			}
		}
		return toolResponse("schools", "queryBusinessData", `{"resource":"schools","limit":100}`), nil
	case 6, 7, 8, 9:
		last := body.Messages[len(body.Messages)-1]
		if last.Role != "tool" {
			m.t.Fatalf("generation edit did not follow the previous tool result: %+v", last)
		}
		if m.calls == 6 && !strings.Contains(last.Content, "学校A") {
			m.t.Fatalf("generation did not receive real school data from its snapshot: %+v", last)
		}
		edit := []strategyagent.EditDraftInput{
			{Form: m.spec.Form},
			{Scoring: m.spec.Scoring},
			{Recommendation: m.spec.Recommendation},
			{Display: m.spec.Display},
		}[m.calls-6]
		edit.ExpectedRevision = int64(m.calls - 6)
		args, err := json.Marshal(edit)
		if err != nil {
			return nil, err
		}
		return toolResponse(fmt.Sprintf("config-edit-%d", m.calls), "editStrategyDraft", string(args)), nil
	case 10:
		return toolResponse("config-check", "checkStrategyDraft", `{"expectedRevision":4}`), nil
	case 11:
		checked := lastToolResult[strategyagent.CheckResult](m.t, body.Messages)
		if !checked.Passed || checked.CheckID == "" {
			m.t.Fatalf("generated configuration did not pass its check: %+v", checked)
		}
		return toolResponse("simulation", "simulateStudent", `{"expectedRevision":4,"fields":{"region":"a"},"scores":{"math":75}}`), nil
	case 12:
		simulation := lastToolResult[strategyagent.Simulation](m.t, body.Messages)
		if len(simulation.Issues) > 0 || len(simulation.Sections) != 1 || simulation.Sections[0].Score != 75 ||
			len(simulation.Sections[0].Tiers) != 1 || len(simulation.Sections[0].Tiers[0].Schools) != 1 ||
			simulation.Sections[0].Tiers[0].Schools[0].Name != "学校A" {
			m.t.Fatalf("business simulation did not confirm the generated rule: %+v", simulation)
		}
		checked := findPreviousCheckResult(m.t, body.Messages)
		last := body.Messages[len(body.Messages)-1]
		if last.Role != "tool" || !strings.Contains(last.Content, "学校A") {
			m.t.Fatalf("simulation result was not returned to the model: %+v", last)
		}
		output, _ := json.Marshal(strategyagent.TurnResult{
			Outcome: "generated", Reply: "配置已生成并通过检查。",
			DraftRevision: checked.Revision, CheckID: checked.CheckID,
		})
		return toolResponse("generated-output", "__runtime_output", string(output)), nil
	default:
		return nil, fmt.Errorf("unexpected model call %d", m.calls)
	}
}

func lastToolResult[T any](t *testing.T, messages []struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}) T {
	t.Helper()
	if len(messages) == 0 || messages[len(messages)-1].Role != "tool" {
		t.Fatalf("last message is not a tool result: %+v", messages)
	}
	var result T
	if err := json.Unmarshal([]byte(messages[len(messages)-1].Content), &result); err != nil {
		t.Fatalf("decode tool result %q: %v", messages[len(messages)-1].Content, err)
	}
	return result
}

func findPreviousCheckResult(t *testing.T, messages []struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}) strategyagent.CheckResult {
	t.Helper()
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "tool" {
			continue
		}
		var result strategyagent.CheckResult
		if json.Unmarshal([]byte(messages[i].Content), &result) == nil && result.Passed && result.CheckID != "" {
			return result
		}
	}
	t.Fatal("no successful draft check was present in model history")
	return strategyagent.CheckResult{}
}

func TestSingleAgentChatConfirmationGeneratesConfiguration(t *testing.T) {
	spec, _ := strategyagentTestDomain()
	spec.Recommendation.Paths[0].ScoreColumn = "admission_score"
	model := &workflowModel{t: t, spec: spec}
	trace := &bytes.Buffer{}
	app, err := server.New(server.Options{Assets: assets(t), HTTPDoer: model, Stdout: trace, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	dbPath := filepath.Join(t.TempDir(), "admissions-agent.sqlite")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&models.Strategy{},
		&models.StrategyConversation{},
		&models.StrategyMessage{},
		&models.StrategyMessageTask{},
		&models.StrategyGenTask{},
		&models.AgentDraft{},
		&models.AgentArtifact{},
		&models.ConfigItem{},
	); err != nil {
		t.Fatal(err)
	}
	rawDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rawDB.Close() })
	repository, err := strategyrepo.NewGormRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&[]models.ConfigItem{
		{SchoolID: 17, ConfigType: "district", Name: "A区", Status: 1},
		{
			SchoolID: 17, ConfigType: "high_school", Name: "学校A", District: "A区", Status: 1,
			DataJSON: `{"type":"public","score":80,"admission_score":80}`,
		},
	}).Error; err != nil {
		t.Fatal(err)
	}

	runClaim := func(claim strategyagent.ClaimedTask) error {
		return strategyagent.RunTask(context.Background(), repository, claim, func(ctx context.Context, task strategyagent.ClaimedTask) error {
			ginContext, _ := gin.CreateTestContext(httptest.NewRecorder())
			if err := luban.BindContext(ginContext, ctx); err != nil {
				return err
			}
			admissions.BindTaskRepository(ginContext, repository)
			_, err := app.Runtime().Run(ginContext, "admissions", task)
			return err
		}, time.Minute)
	}

	conversation, err := repository.CreateConversation(context.Background(), 17, "志愿策略")
	if err != nil {
		t.Fatal(err)
	}
	chatTask, err := repository.SubmitMessage(context.Background(), 17, conversation.ID,
		"收集姓名、手机号和所在区；数学成绩0到100分，按数学分差冲稳保，每档推荐1所公办学校。", "chat-request-1")
	if err != nil {
		t.Fatal(err)
	}
	chatClaim, found, err := repository.Claim(context.Background(), "chat")
	if err != nil || !found || chatClaim.TaskID != chatTask.ID {
		t.Fatalf("chat claim = %+v, found=%v, err=%v", chatClaim, found, err)
	}
	if err := runClaim(chatClaim); err != nil {
		t.Fatalf("chat Agent run: %v", err)
	}
	chatResult, err := repository.MessageResult(context.Background(), 17, chatTask.ID)
	if err != nil || chatResult.Status != "success" || chatResult.Mode != "confirm" {
		t.Fatalf("chat result = %+v, err=%v", chatResult, err)
	}

	generationTask, err := repository.Confirm(context.Background(), 17, conversation.ID, 1, 1, 0)
	if err != nil || generationTask.Status != "pending" {
		t.Fatalf("confirm result = %+v, err=%v", generationTask, err)
	}
	generationClaim, found, err := repository.Claim(context.Background(), "generate")
	if err != nil || !found || generationClaim.TaskID != generationTask.ID {
		t.Fatalf("generation claim = %+v, found=%v, err=%v", generationClaim, found, err)
	}
	if err := runClaim(generationClaim); err != nil {
		t.Fatalf("generation Agent run: %v", err)
	}

	generationResult, err := repository.GenerationResult(context.Background(), 17, generationTask.ID)
	if err != nil || generationResult.Status != "success" || generationResult.ResultArtifactID == 0 {
		t.Fatalf("generation result = %+v, err=%v", generationResult, err)
	}
	var saved models.AgentArtifact
	if err := db.First(&saved, generationResult.ResultArtifactID).Error; err != nil {
		t.Fatalf("load generated configuration: %v", err)
	}
	var artifact strategyagent.Artifact
	if err := json.Unmarshal([]byte(saved.ArtifactJSON), &artifact); err != nil {
		t.Fatalf("decode generated configuration: %v", err)
	}
	if artifact.Spec.Form == nil || artifact.Spec.Scoring == nil || artifact.Spec.Recommendation == nil ||
		artifact.Spec.Display == nil || len(artifact.Lookups) != 1 || len(artifact.Lookups[0].Entries) != 101 {
		t.Fatalf("generated configuration is incomplete: %+v", artifact)
	}
	if len(artifact.Lookups[0].Entries[75].Tiers[0].Schools) != 1 ||
		artifact.Lookups[0].Entries[75].Tiers[0].Schools[0].Name != "学校A" {
		t.Fatalf("generated lookup did not include the configured school: %+v", artifact.Lookups[0].Entries[75])
	}
	if model.calls != 12 {
		t.Fatalf("model calls = %d, want catalog, requirements, check, output, schools, four config edits, check, simulation, output", model.calls)
	}
	if bytes.Contains(trace.Bytes(), []byte("private-")) || bytes.Contains(trace.Bytes(), []byte("fake-key")) {
		t.Fatal("task credential leaked into Trace")
	}
}
