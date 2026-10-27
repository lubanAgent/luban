package strategyrepo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	strategyagent "github.com/lubanAgent/luban/agents/admissions/strategy"
	"github.com/lubanAgent/luban/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func testRepository(t *testing.T) *GormRepository {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "admissions.sqlite")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
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
	repository, err := NewGormRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	repository.now = func() time.Time { return now }
	t.Cleanup(func() {
		if raw, err := db.DB(); err == nil {
			_ = raw.Close()
		}
	})
	return repository
}

func TestConversationMessageTaskClaimsLoadsAndCommits(t *testing.T) {
	repository := testRepository(t)
	if err := repository.ValidateSchema(); err != nil {
		t.Fatal(err)
	}
	conversation, err := repository.CreateConversation(context.Background(), 17, "春季策略")
	if err != nil {
		t.Fatal(err)
	}
	task, err := repository.SubmitMessage(context.Background(), 17, conversation.ID, "按数学成绩推荐", "request-1")
	if err != nil {
		t.Fatal(err)
	}
	claim, found, err := repository.Claim(context.Background(), "chat")
	if err != nil || !found || claim.TaskID != task.ID || claim.AttemptToken == "" {
		t.Fatalf("claim = %+v, found=%v, err=%v", claim, found, err)
	}
	material, err := repository.Load(context.Background(), claim)
	if err != nil {
		t.Fatal(err)
	}
	if material.MessageSeq != 1 || material.ConversationID != conversation.ID || len(material.History) != 1 {
		t.Fatalf("task material = %+v", material)
	}
	reply, err := repository.Commit(
		context.Background(),
		material,
		material.Draft,
		nil,
		TurnResult{Outcome: "reply", Reply: "请补充成绩范围。", DraftRevision: material.Draft.Revision},
	)
	if err != nil || reply.TaskID != task.ID || reply.ProcessedMessageSeq != 1 {
		t.Fatalf("commit = %+v, %v", reply, err)
	}
	stored, err := repository.MessageResult(context.Background(), 17, task.ID)
	if err != nil || stored.Status != "success" || stored.AssistantMessageID == 0 {
		t.Fatalf("stored task = %+v, %v", stored, err)
	}
	if err := repository.Fail(context.Background(), claim, errors.New("late cancellation")); err != nil {
		t.Fatalf("successful Commit must win late Fail: %v", err)
	}
}

func TestExpiredLeaseCannotCommitAndCanBeReclaimed(t *testing.T) {
	repository := testRepository(t)
	conversation, err := repository.CreateConversation(context.Background(), 17, "春季策略")
	if err != nil {
		t.Fatal(err)
	}
	task, err := repository.SubmitMessage(context.Background(), 17, conversation.ID, "第一轮需求", "")
	if err != nil {
		t.Fatal(err)
	}
	first, found, err := repository.Claim(context.Background(), "chat")
	if err != nil || !found {
		t.Fatalf("first claim: %v %v", found, err)
	}
	material, err := repository.Load(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	repository.now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	if err := repository.Reap(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Commit(context.Background(), material, material.Draft, nil, TurnResult{Outcome: "reply", Reply: "迟到结果"}); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("expired attempt committed: %v", err)
	}
	repository.now = func() time.Time { return time.Now().Add(3 * time.Minute) }
	second, found, err := repository.Claim(context.Background(), "chat")
	if err != nil || !found || second.TaskID != task.ID || second.AttemptToken == first.AttemptToken {
		t.Fatalf("reclaim = %+v, found=%v, err=%v", second, found, err)
	}
}

func TestGenerationCommitStoresImmutableArtifact(t *testing.T) {
	repository := testRepository(t)
	conversation, err := repository.CreateConversation(context.Background(), 17, "春季策略")
	if err != nil {
		t.Fatal(err)
	}
	var strategy models.Strategy
	if err := repository.db.First(&strategy, conversation.StrategyID).Error; err != nil {
		t.Fatal(err)
	}
	requirements := strategyagent.RequirementCard{
		Stage1Brief: "选择地区", Stage2Brief: "数学0到100分，直接按数学分算",
		Stage3Brief: "公办学校冲稳保", CoveredMessageSeq: 1,
		ExplicitRules: []strategyagent.SourceRule{{Text: "按数学分算", SourceMessageSeq: 1}},
	}
	requirements.CoveredMessageSeq = 1
	rawRequirements, _ := json.Marshal(requirements)
	if err := repository.db.Model(&models.StrategyConversation{}).Where("id = ?", conversation.ID).Updates(map[string]any{
		"current_phase": "generating", "requirements_revision": 1, "processed_message_seq": 1, "next_message_seq": 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.db.Create(&models.AgentDraft{ConversationID: conversation.ID, Revision: 2, DraftJSON: `{"draftId":"draft","revision":2,"schemaVersion":1}`, UpdateTime: repository.now().Unix()}).Error; err != nil {
		t.Fatal(err)
	}
	task := models.StrategyGenTask{
		StrategyID: strategy.ID, ConversationID: conversation.ID, TriggerReason: "confirmed",
		InputSnapshot: string(rawRequirements), Status: "pending", CreateTime: repository.now().Unix(),
		UpdateTime: repository.now().Unix(), TaskExecution: models.TaskExecution{Engine: engineName, RequirementsRevision: 1},
	}
	if err := repository.db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	claim, found, err := repository.Claim(context.Background(), "generate")
	if err != nil || !found {
		t.Fatalf("claim = %v, %v", found, err)
	}
	material, err := repository.Load(context.Background(), claim)
	if err != nil {
		t.Fatal(err)
	}
	if material.RequirementsRevision != 1 || material.Draft.Requirements.CoveredMessageSeq != 1 {
		t.Fatalf("generation material = %+v", material)
	}
	artifact := &Artifact{SnapshotHash: material.Snapshot.Hash, Spec: StrategySpec{
		Form: &strategyagent.FormSpec{}, Scoring: &strategyagent.ScoringSpec{},
		Recommendation: &strategyagent.RecommendationSpec{}, Display: &strategyagent.DisplaySpec{},
	}}
	if _, err := repository.Commit(context.Background(), material, material.Draft, artifact, TurnResult{Outcome: "generated", Reply: "已完成", DraftRevision: material.Draft.Revision}); err != nil {
		t.Fatal(err)
	}
	result, err := repository.GenerationResult(context.Background(), 17, task.ID)
	if err != nil || result.Status != "success" || result.ResultArtifactID == 0 {
		t.Fatalf("generation result = %+v, %v", result, err)
	}
	var saved models.AgentArtifact
	if err := repository.db.First(&saved, result.ResultArtifactID).Error; err != nil {
		t.Fatalf("load saved artifact: %v", err)
	}
	if saved.Status != "ready" || saved.Hash == "" || saved.SnapshotJSON == "" {
		t.Fatalf("generated artifact was not saved: %+v", saved)
	}
}

type taskRunnerRepo struct {
	renewErr error
	renewed  chan struct{}
	failed   chan error
}

func (*taskRunnerRepo) Load(context.Context, ClaimedTask) (TaskMaterial, error) {
	return TaskMaterial{}, fmt.Errorf("unused")
}
func (*taskRunnerRepo) Commit(context.Context, TaskMaterial, Draft, *Artifact, TurnResult) (Reply, error) {
	return Reply{}, fmt.Errorf("unused")
}
func (*taskRunnerRepo) Claim(context.Context, string) (ClaimedTask, bool, error) {
	return ClaimedTask{}, false, fmt.Errorf("unused")
}
func (r *taskRunnerRepo) Renew(context.Context, ClaimedTask) error {
	if r.renewErr != nil {
		close(r.renewed)
		return r.renewErr
	}
	return nil
}
func (r *taskRunnerRepo) Fail(_ context.Context, _ ClaimedTask, cause error) error {
	r.failed <- cause
	return nil
}
func (*taskRunnerRepo) Reap(context.Context) error { return nil }

func TestRunTaskRenewalFailureCancelsAndFinalizes(t *testing.T) {
	renewFailure := errors.New("renewal lost")
	repository := &taskRunnerRepo{renewErr: renewFailure, renewed: make(chan struct{}), failed: make(chan error, 1)}
	executed := make(chan struct{})
	err := strategyagent.RunTask(context.Background(), repository, ClaimedTask{Kind: "chat", TaskID: 1}, func(ctx context.Context, _ ClaimedTask) error {
		close(executed)
		<-ctx.Done()
		return nil // Simulate a callback that returns without checking cancel.
	}, time.Millisecond)
	if !errors.Is(err, renewFailure) || !errors.Is(err, context.Canceled) {
		t.Fatalf("RunTask error = %v", err)
	}
	select {
	case <-executed:
	default:
		t.Fatal("Agent callback did not start")
	}
	select {
	case failure := <-repository.failed:
		if !errors.Is(failure, renewFailure) {
			t.Fatalf("Fail cause = %v", failure)
		}
	case <-time.After(time.Second):
		t.Fatal("task failure was not finalized")
	}
}

func TestRunTaskConvertsPanicAndFinalizes(t *testing.T) {
	repository := &taskRunnerRepo{failed: make(chan error, 1)}
	err := strategyagent.RunTask(context.Background(), repository, ClaimedTask{Kind: "chat", TaskID: 1}, func(context.Context, ClaimedTask) error {
		panic("secret or unstable panic value")
	}, time.Second)
	if err == nil || errors.Is(err, context.Canceled) {
		t.Fatalf("panic result = %v", err)
	}
	if got := <-repository.failed; got == nil || got.Error() != "admissions task callback panicked" {
		t.Fatalf("panic finalization cause = %v", got)
	}
}
