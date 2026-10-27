package strategyrepo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	strategyagent "github.com/lubanAgent/luban/agents/admissions/strategy"
	"github.com/lubanAgent/luban/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SubmitMessage saves a message and its pending task in the same transaction.
// schoolID is the caller's authorized tenant boundary, not a model parameter.
func (r *GormRepository) SubmitMessage(ctx context.Context, schoolID, conversationID int64, content, requestID string) (models.StrategyMessageTask, error) {
	var task models.StrategyMessageTask
	if schoolID <= 0 || conversationID <= 0 || content == "" || len(content) > 64<<10 || len(requestID) > 128 {
		return task, fmt.Errorf("invalid message input")
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		task, err = r.submitMessage(tx, schoolID, conversationID, content, requestID)
		return err
	})
	return task, err
}

// CreateAndSubmitMessage makes a first message and its new conversation one
// transaction, so an invalid or failed task insert does not leave an empty
// strategy behind.
func (r *GormRepository) CreateAndSubmitMessage(ctx context.Context, schoolID int64, name, content, requestID string) (models.StrategyConversation, models.StrategyMessageTask, error) {
	var conversation models.StrategyConversation
	var task models.StrategyMessageTask
	if schoolID <= 0 || name == "" || len(name) > 128 || content == "" || len(content) > 64<<10 || len(requestID) > 128 {
		return conversation, task, fmt.Errorf("invalid first message input")
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		conversation, err = r.createConversation(tx, schoolID, name)
		if err != nil {
			return err
		}
		task, err = r.submitMessage(tx, schoolID, conversation.ID, content, requestID)
		return err
	})
	return conversation, task, err
}

func (r *GormRepository) submitMessage(tx *gorm.DB, schoolID, conversationID int64, content, requestID string) (models.StrategyMessageTask, error) {
	var task models.StrategyMessageTask
	conversation, _, err := r.lockConversation(tx, schoolID, conversationID)
	if err != nil {
		return task, err
	}
	if conversation.CurrentPhase == "generating" {
		return task, fmt.Errorf("conversation is generating")
	}
	if requestID != "" {
		var existing models.StrategyMessage
		err := tx.Where("conversation_id = ? AND role = ? AND client_request_id = ?", conversationID, "user", requestID).Take(&existing).Error
		if err == nil {
			err = tx.Where("user_message_id = ? AND engine = ?", existing.ID, engineName).Take(&task).Error
			return task, err
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return task, err
		}
	}
	var clientID *string
	if requestID != "" {
		clientID = &requestID
	}
	seq := conversation.NextMessageSeq + 1
	message := models.StrategyMessage{ConversationID: conversationID, Seq: seq, Role: "user", Content: content, ClientRequestID: clientID, Metadata: "{}", CreateTime: r.now().UnixMilli()}
	if err := tx.Create(&message).Error; err != nil {
		return task, err
	}
	task = models.StrategyMessageTask{ConversationID: conversationID, StrategyID: conversation.StrategyID, UserMessageID: message.ID, MessageSeq: seq, Status: "pending", CreateTime: r.now().Unix(), UpdateTime: r.now().Unix(), TaskExecution: models.TaskExecution{Engine: engineName}}
	if err := tx.Create(&task).Error; err != nil {
		return task, err
	}
	err = tx.Model(&conversation).Updates(map[string]any{"next_message_seq": seq, "current_phase": "collecting", "update_time": r.now().Unix()}).Error
	return task, err
}

// Confirm freezes a reviewed requirement version and creates one generation
// task. Repeating the same version returns the existing task, never a new one.
func (r *GormRepository) Confirm(ctx context.Context, schoolID, conversationID, revision, messageSeq, confirmedBy int64) (models.StrategyGenTask, error) {
	var task models.StrategyGenTask
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		conversation, _, err := r.lockConversation(tx, schoolID, conversationID)
		if err != nil {
			return err
		}
		if revision <= 0 || conversation.RequirementsRevision != revision || conversation.ProcessedMessageSeq != messageSeq || conversation.NextMessageSeq != messageSeq {
			return fmt.Errorf("requirements_version_changed")
		}
		err = tx.Where("conversation_id = ? AND requirements_revision = ? AND engine = ?", conversationID, revision, engineName).Take(&task).Error
		if err == nil {
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if conversation.CurrentPhase != "awaiting_confirm" {
			return fmt.Errorf("conversation is not awaiting confirmation")
		}
		var pending int64
		if err := tx.Model(&models.StrategyMessageTask{}).Where("conversation_id = ? AND engine = ? AND status IN ?", conversationID, engineName, []string{"pending", "running", "failed"}).Count(&pending).Error; err != nil {
			return err
		}
		if pending > 0 {
			return fmt.Errorf("conversation has unfinished messages")
		}
		var draft models.AgentDraft
		if err := tx.Where("conversation_id = ?", conversationID).Take(&draft).Error; err != nil {
			return err
		}
		var current Draft
		if json.Unmarshal([]byte(draft.DraftJSON), &current) != nil {
			return fmt.Errorf("invalid saved draft")
		}
		if issues := strategyagent.CheckRequirements(current.Requirements, messageSeq); len(issues) > 0 {
			return fmt.Errorf("requirements not complete")
		}
		raw, err := json.Marshal(current.Requirements)
		if err != nil {
			return err
		}
		task = models.StrategyGenTask{StrategyID: conversation.StrategyID, ConversationID: conversationID, TriggerReason: "confirmed", InputSnapshot: string(raw), Status: "pending", CreateTime: r.now().Unix(), UpdateTime: r.now().Unix(), TaskExecution: models.TaskExecution{Engine: engineName, RequirementsRevision: revision}}
		if err := tx.Create(&task).Error; err != nil {
			return err
		}
		return tx.Model(&conversation).Updates(map[string]any{"current_phase": "generating", "confirm_by": confirmedBy, "confirm_time": r.now().Unix(), "update_time": r.now().Unix()}).Error
	})
	return task, err
}

func (r *GormRepository) lockConversation(tx *gorm.DB, schoolID, conversationID int64) (models.StrategyConversation, models.Strategy, error) {
	var conversation models.StrategyConversation
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&conversation, "id = ?", conversationID).Error; err != nil {
		return conversation, models.Strategy{}, err
	}
	var strategy models.Strategy
	if err := tx.Where("id = ? AND school_id = ?", conversation.StrategyID, schoolID).Take(&strategy).Error; err != nil {
		return conversation, strategy, err
	}
	return conversation, strategy, nil
}

func (r *GormRepository) CreateConversation(ctx context.Context, schoolID int64, name string) (models.StrategyConversation, error) {
	var conversation models.StrategyConversation
	if schoolID <= 0 || name == "" || len(name) > 128 {
		return conversation, fmt.Errorf("schoolId and strategy name are required")
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		conversation, err = r.createConversation(tx, schoolID, name)
		return err
	})
	return conversation, err
}

func (r *GormRepository) createConversation(tx *gorm.DB, schoolID int64, name string) (models.StrategyConversation, error) {
	strategy := models.Strategy{SchoolID: schoolID, Name: name, Version: 1, Status: 1, CreateTime: r.now().Unix(), UpdateTime: r.now().Unix()}
	if err := tx.Create(&strategy).Error; err != nil {
		return models.StrategyConversation{}, err
	}
	conversation := models.StrategyConversation{StrategyID: strategy.ID, CurrentPhase: "collecting", CreateTime: r.now().Unix(), UpdateTime: r.now().Unix()}
	if err := tx.Create(&conversation).Error; err != nil {
		return models.StrategyConversation{}, err
	}
	return conversation, tx.Model(&strategy).Update("conversation_id", conversation.ID).Error
}

func (r *GormRepository) MessageResult(ctx context.Context, schoolID, taskID int64) (models.StrategyMessageTask, error) {
	var task models.StrategyMessageTask
	err := r.db.WithContext(ctx).Table("tblStrategyMessageTask AS task").Select("task.*").Joins("JOIN tblStrategy AS strategy ON strategy.id = task.strategy_id").Where("task.id = ? AND strategy.school_id = ? AND task.engine = ?", taskID, schoolID, engineName).Take(&task).Error
	return task, err
}

func (r *GormRepository) GenerationResult(ctx context.Context, schoolID, taskID int64) (models.StrategyGenTask, error) {
	var task models.StrategyGenTask
	err := r.db.WithContext(ctx).Table("tblStrategyGenTask AS task").Select("task.*").Joins("JOIN tblStrategy AS strategy ON strategy.id = task.strategy_id").Where("task.id = ? AND strategy.school_id = ? AND task.engine = ?", taskID, schoolID, engineName).Take(&task).Error
	return task, err
}
