package router_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/lubanAgent/luban/agents/admissions"
	strategyagent "github.com/lubanAgent/luban/agents/admissions/strategy"
	"github.com/lubanAgent/luban/models"
	"github.com/lubanAgent/luban/models/strategyrepo"
	"github.com/lubanAgent/luban/router"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAdmissionsSingleRouteSubmitsMessagesAndConfirmation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.sqlite")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		raw, _ := db.DB()
		_ = raw.Close()
	})
	if err := db.AutoMigrate(
		&models.Strategy{}, &models.StrategyConversation{}, &models.StrategyMessage{},
		&models.StrategyMessageTask{}, &models.StrategyGenTask{}, &models.AgentDraft{},
	); err != nil {
		t.Fatal(err)
	}
	repository, err := strategyrepo.NewGormRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	router.RegisterSubmission(engine, "/admissions/mis/strategy/conversation/message", admissions.SubmitTo(repository), admissions.RenderSubmission)
	if routes := engine.Routes(); len(routes) != 1 || routes[0].Method != http.MethodPost ||
		routes[0].Path != "/admissions/mis/strategy/conversation/message" {
		t.Fatalf("unexpected admissions routes: %+v", routes)
	}

	submit := func(body string) (int, struct {
		ErrNo int `json:"errNo"`
		Data  struct {
			ConversationID int64  `json:"conversationId"`
			TaskID         int64  `json:"taskId"`
			Status         string `json:"status"`
		} `json:"data"`
	}) {
		t.Helper()
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/admissions/mis/strategy/conversation/message", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		engine.ServeHTTP(recorder, request)
		var response struct {
			ErrNo int `json:"errNo"`
			Data  struct {
				ConversationID int64  `json:"conversationId"`
				TaskID         int64  `json:"taskId"`
				Status         string `json:"status"`
			} `json:"data"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		return recorder.Code, response
	}

	code, first := submit(`{"action":"message","schoolId":17,"name":"志愿策略","content":"数学 75 分","requestId":"first"}`)
	if code != http.StatusOK || first.ErrNo != 0 || first.Data.ConversationID == 0 ||
		first.Data.TaskID == 0 || first.Data.Status != "pending" {
		t.Fatalf("first message status=%d response=%+v", code, first)
	}
	var count int64
	if err := db.Model(&models.StrategyConversation{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("first message conversations=%d err=%v", count, err)
	}
	pathBody := fmt.Sprintf(`{"action":"message","schoolId":17,"conversationId":%d,"content":"补充地区","requestId":"second"}`, first.Data.ConversationID)
	code, second := submit(pathBody)
	if code != http.StatusOK || second.Data.ConversationID != first.Data.ConversationID ||
		second.Data.TaskID == first.Data.TaskID {
		t.Fatalf("follow-up message status=%d response=%+v", code, second)
	}
	_, repeated := submit(pathBody)
	if repeated.Data.TaskID != second.Data.TaskID {
		t.Fatalf("message idempotency lost: %+v %+v", second, repeated)
	}
	// The same route must reject a stale confirmation without starting a task.
	code, stale := submit(fmt.Sprintf(`{"action":"confirm","schoolId":17,"conversationId":%d,"expectedRequirementsRevision":1,"expectedMessageSeq":1}`, first.Data.ConversationID))
	if code != http.StatusBadRequest || stale.ErrNo != 1 {
		t.Fatalf("stale confirmation status=%d response=%+v", code, stale)
	}
	if err := db.Model(&models.StrategyGenTask{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("stale confirmation tasks=%d err=%v", count, err)
	}

	card := strategyagent.RequirementCard{
		Stage1Brief: "收集地区", Stage2Brief: "数学分", Stage3Brief: "推荐学校",
		CoveredMessageSeq: 2, ExplicitRules: []strategyagent.SourceRule{{Text: "按数学分推荐", SourceMessageSeq: 1}},
	}
	draft, _ := json.Marshal(strategyagent.Draft{Requirements: card})
	if err := db.Create(&models.AgentDraft{ConversationID: first.Data.ConversationID, DraftJSON: string(draft)}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.StrategyConversation{}).Where("id = ?", first.Data.ConversationID).
		Updates(map[string]any{"current_phase": "awaiting_confirm", "requirements_revision": 1, "processed_message_seq": 2}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.StrategyMessageTask{}).Where("conversation_id = ?", first.Data.ConversationID).
		Update("status", "success").Error; err != nil {
		t.Fatal(err)
	}
	code, confirmed := submit(fmt.Sprintf(`{"action":"confirm","schoolId":17,"conversationId":%d,"expectedRequirementsRevision":1,"expectedMessageSeq":2}`, first.Data.ConversationID))
	if code != http.StatusOK || confirmed.ErrNo != 0 || confirmed.Data.TaskID == 0 ||
		confirmed.Data.Status != "pending" || confirmed.Data.ConversationID != first.Data.ConversationID {
		t.Fatalf("confirmation status=%d response=%+v", code, confirmed)
	}
}

func TestAdmissionsInvalidFirstMessageDoesNotCreateConversation(t *testing.T) {
	// The repository enforces the same limit in its transaction, including
	// calls that do not enter through the HTTP handler.
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.sqlite")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		raw, _ := db.DB()
		_ = raw.Close()
	})
	if err := db.AutoMigrate(&models.Strategy{}, &models.StrategyConversation{}, &models.StrategyMessage{}, &models.StrategyMessageTask{}); err != nil {
		t.Fatal(err)
	}
	repository, _ := strategyrepo.NewGormRepository(db)
	_, _, err = repository.CreateAndSubmitMessage(t.Context(), 17, "志愿策略", "hello", strings.Repeat("x", 129))
	if err == nil {
		t.Fatal("oversized requestId was accepted")
	}
	var count int64
	if err := db.Model(&models.Strategy{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("invalid message left an orphan strategy: %d (%v)", count, err)
	}
}
