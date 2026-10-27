package strategyrepo

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	strategyagent "github.com/lubanAgent/luban/agents/admissions/strategy"
	"github.com/lubanAgent/luban/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const engineName = "luban-v1"

// GormRepository persists admissions business state. It never creates tables.
// Existing schemas must be checked against the migration specification first.
type GormRepository struct {
	db          *gorm.DB
	now         func() time.Time
	lease       time.Duration
	maxAttempts int
}

func NewGormRepository(db *gorm.DB) (*GormRepository, error) {
	if db == nil {
		return nil, fmt.Errorf("admissions database must not be nil")
	}
	return &GormRepository{db: db, now: time.Now, lease: time.Minute, maxAttempts: 3}, nil
}

// ValidateSchema checks the required target columns without modifying tables.
// The new service must not start a task worker against an unmigrated old schema.
func (r *GormRepository) ValidateSchema() error {
	for _, model := range []any{&models.Strategy{}, &models.StrategyConversation{}, &models.StrategyMessage{}, &models.StrategyMessageTask{}, &models.StrategyGenTask{}, &models.AgentDraft{}, &models.AgentArtifact{}, &models.ConfigItem{}} {
		statement := &gorm.Statement{DB: r.db}
		if err := statement.Parse(model); err != nil {
			return fmt.Errorf("parse admissions schema model")
		}
		columns, err := r.db.Migrator().ColumnTypes(model)
		if err != nil {
			return fmt.Errorf("table %s is unavailable; schema must be verified before task execution", statement.Schema.Table)
		}
		exists := make(map[string]bool)
		for _, column := range columns {
			exists[column.Name()] = true
		}
		for _, field := range statement.Schema.Fields {
			if field.DBName != "" && !exists[field.DBName] {
				return fmt.Errorf("table %s lacks required column %s", statement.Schema.Table, field.DBName)
			}
		}
	}
	return nil
}

func taskTable(kind string) (string, error) {
	switch kind {
	case "chat":
		return (models.StrategyMessageTask{}).TableName(), nil
	case "generate":
		return (models.StrategyGenTask{}).TableName(), nil
	}
	return "", fmt.Errorf("invalid task kind")
}

type taskRow struct {
	ID                   int64
	ConversationID       int64 `gorm:"column:conversation_id"`
	StrategyID           int64 `gorm:"column:strategy_id"`
	MessageSeq           int64 `gorm:"column:message_seq"`
	Status               string
	AttemptToken         string `gorm:"column:attempt_token"`
	AttemptCount         int    `gorm:"column:attempt_count"`
	LeaseUntil           int64  `gorm:"column:lease_until"`
	RequirementsRevision int64  `gorm:"column:requirements_revision"`
	InputSnapshot        string `gorm:"column:input_snapshot"`
	SnapshotJSON         string `gorm:"column:snapshot_json"`
}

func (r *GormRepository) validTask(tx *gorm.DB, claim ClaimedTask) (taskRow, error) {
	table, err := taskTable(claim.Kind)
	if err != nil {
		return taskRow{}, err
	}
	var row taskRow
	err = tx.Table(table).Where("id = ? AND status = ? AND engine = ? AND attempt_token = ? AND lease_until > ?", claim.TaskID, "running", engineName, claim.AttemptToken, r.now().Unix()).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return taskRow{}, ErrLeaseLost
	}
	return row, err
}

func (r *GormRepository) Load(ctx context.Context, claim ClaimedTask) (TaskMaterial, error) {
	if claim.AttemptToken == "" {
		return TaskMaterial{}, ErrLeaseLost
	}
	var material TaskMaterial
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row, err := r.validTask(tx, claim)
		if err != nil {
			return err
		}
		var conversation models.StrategyConversation
		if err := tx.First(&conversation, "id = ?", row.ConversationID).Error; err != nil {
			return err
		}
		var strategy models.Strategy
		if err := tx.First(&strategy, "id = ?", row.StrategyID).Error; err != nil {
			return err
		}
		if conversation.StrategyID != strategy.ID {
			return fmt.Errorf("task/conversation strategy mismatch")
		}
		material = TaskMaterial{Claim: claim, ConversationID: conversation.ID, StrategyID: strategy.ID, SchoolID: strategy.SchoolID, RequirementsRevision: conversation.RequirementsRevision, MessageSeq: row.MessageSeq}
		var draft models.AgentDraft
		if err := tx.Where("conversation_id = ?", conversation.ID).Take(&draft).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			material.Draft = Draft{ID: fmt.Sprintf("conversation_%d", conversation.ID), SchemaVersion: 1}
		} else if json.Unmarshal([]byte(draft.DraftJSON), &material.Draft) != nil {
			return fmt.Errorf("invalid persisted Agent draft")
		}
		if claim.Kind == "generate" {
			if conversation.CurrentPhase != "generating" || row.RequirementsRevision != conversation.RequirementsRevision {
				return ErrLeaseLost
			}
			if json.Unmarshal([]byte(row.InputSnapshot), &material.Draft.Requirements) != nil {
				return fmt.Errorf("invalid confirmed requirements snapshot")
			}
			material.Draft.Spec = StrategySpec{}
			material.Draft.Revision = 0
			material.MessageSeq = material.Draft.Requirements.CoveredMessageSeq
		}
		material.BaseRequirementsHash, err = hashJSON(material.Draft.Requirements)
		if err != nil {
			return err
		}
		var history []models.StrategyMessage
		if err := tx.Where("conversation_id = ? AND seq <= ?", conversation.ID, material.MessageSeq).Order("seq ASC, id ASC").Find(&history).Error; err != nil {
			return err
		}
		for _, message := range history {
			if oneOf(message.Role, "user", "assistant") {
				material.History = append(material.History, HistoryMessage{Seq: message.Seq, Role: message.Role, Content: message.Content})
			}
		}
		if row.SnapshotJSON != "" {
			if json.Unmarshal([]byte(row.SnapshotJSON), &material.Snapshot) != nil {
				return fmt.Errorf("invalid persisted school snapshot")
			}
		} else {
			var items []models.ConfigItem
			if err := tx.Where("school_id = ? AND status = 1", strategy.SchoolID).Order("id ASC").Find(&items).Error; err != nil {
				return err
			}
			snapshot, err := strategyagent.BuildSnapshot(items)
			if err != nil {
				return err
			}
			material.Snapshot = snapshot
			raw, err := json.Marshal(snapshot)
			if err != nil {
				return err
			}
			table, _ := taskTable(claim.Kind)
			updated := tx.Table(table).Where("id = ? AND attempt_token = ? AND status = ? AND lease_until > ? AND snapshot_json = ?", claim.TaskID, claim.AttemptToken, "running", r.now().Unix(), "").Update("snapshot_json", string(raw))
			if updated.Error != nil {
				return updated.Error
			}
			if updated.RowsAffected != 1 {
				return ErrLeaseLost
			}
		}
		return nil
	})
	return material, err
}

func (r *GormRepository) Commit(ctx context.Context, material TaskMaterial, draft Draft, artifact *Artifact, result TurnResult) (Reply, error) {
	var reply Reply
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var conversation models.StrategyConversation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&conversation, "id = ?", material.ConversationID).Error; err != nil {
			return err
		}
		row, err := r.validTask(tx, material.Claim)
		if err != nil {
			return err
		}
		if row.ConversationID != conversation.ID || row.StrategyID != material.StrategyID || conversation.RequirementsRevision != material.RequirementsRevision {
			return ErrLeaseLost
		}
		if row.StrategyID != material.StrategyID {
			return ErrLeaseLost
		}
		now := r.now().Unix()
		table, _ := taskTable(material.Claim.Kind)
		taskUpdates := map[string]any{"status": "success", "attempt_token": "", "lease_until": 0, "finish_time": now, "update_time": now}
		convUpdates := map[string]any{"update_time": now}
		reply = Reply{TaskID: material.Claim.TaskID, Outcome: result.Outcome, Reply: result.Reply, RequirementsRevision: conversation.RequirementsRevision, ProcessedMessageSeq: conversation.ProcessedMessageSeq}
		if material.Claim.Kind == "chat" {
			if !oneOf(result.Outcome, "reply", "readyToConfirm") || row.MessageSeq <= conversation.ProcessedMessageSeq {
				return fmt.Errorf("invalid chat submission")
			}
			reply.RequirementsRevision++
			reply.ProcessedMessageSeq = row.MessageSeq
			phase := "collecting"
			if result.Outcome == "readyToConfirm" && conversation.NextMessageSeq == row.MessageSeq {
				phase = "awaiting_confirm"
			}
			summary, err := json.Marshal(draft.Requirements)
			if err != nil {
				return err
			}
			convUpdates["requirements_revision"], convUpdates["processed_message_seq"], convUpdates["current_phase"], convUpdates["collected_summary"] = reply.RequirementsRevision, row.MessageSeq, phase, string(summary)
			message := models.StrategyMessage{ConversationID: conversation.ID, Seq: row.MessageSeq, Role: "assistant", AgentType: "MAIN", Content: result.Reply, Metadata: "{}", CreateTime: r.now().UnixMilli()}
			if err := tx.Create(&message).Error; err != nil {
				return err
			}
			taskUpdates["assistant_message_id"] = message.ID
			mode := "clarify"
			if phase == "awaiting_confirm" {
				mode = "confirm"
			}
			taskUpdates["mode"] = mode
		} else {
			if conversation.CurrentPhase != "generating" || row.RequirementsRevision != conversation.RequirementsRevision {
				return ErrLeaseLost
			}
			requirementsHash, err := hashJSON(draft.Requirements)
			if err != nil || requirementsHash != material.BaseRequirementsHash {
				return fmt.Errorf("confirmed requirements changed during generation")
			}
			switch result.Outcome {
			case "generated":
				if artifact == nil || artifact.SnapshotHash != material.Snapshot.Hash {
					return fmt.Errorf("missing checked artifact")
				}
				raw, err := json.Marshal(artifact)
				if err != nil {
					return err
				}
				snapshot, err := json.Marshal(material.Snapshot)
				if err != nil {
					return err
				}
				hash, err := hashJSON(artifact)
				if err != nil {
					return err
				}
				record := models.AgentArtifact{TaskID: material.Claim.TaskID, StrategyID: material.StrategyID, ConversationID: conversation.ID, Hash: hash, ArtifactJSON: string(raw), SnapshotJSON: string(snapshot), Status: "ready", CreateTime: now}
				if err := tx.Create(&record).Error; err != nil {
					return err
				}
				reply.ArtifactID = record.ID
				convUpdates["preview_artifact_id"], convUpdates["current_phase"] = record.ID, "review"
				taskUpdates["result_artifact_id"], taskUpdates["current_stage"], taskUpdates["progress"] = record.ID, "ready", 100
			case "needsInput":
				convUpdates["current_phase"] = "collecting"
				taskUpdates["status"], taskUpdates["last_error"] = "cancelled", "needs_input"
			default:
				return fmt.Errorf("invalid generation outcome")
			}
		}
		rawDraft, err := json.Marshal(draft)
		if err != nil {
			return err
		}
		record := models.AgentDraft{ConversationID: conversation.ID, Revision: draft.Revision, DraftJSON: string(rawDraft), UpdateTime: now}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "conversation_id"}}, DoUpdates: clause.AssignmentColumns([]string{"revision", "draft_json", "update_time"})}).Create(&record).Error; err != nil {
			return err
		}
		if err := tx.Model(&conversation).Updates(convUpdates).Error; err != nil {
			return err
		}
		if material.Claim.Kind == "chat" {
			raw, err := json.Marshal(reply)
			if err != nil {
				return err
			}
			taskUpdates["result_json"] = string(raw)
		}
		updated := tx.Table(table).Where("id = ? AND status = ? AND attempt_token = ? AND lease_until > ?", material.Claim.TaskID, "running", material.Claim.AttemptToken, r.now().Unix()).Updates(taskUpdates)
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrLeaseLost
		}
		return nil
	})
	return reply, err
}

func (r *GormRepository) Claim(ctx context.Context, kind string) (claim ClaimedTask, found bool, resultErr error) {
	table, err := taskTable(kind)
	if err != nil {
		return claim, false, err
	}
	resultErr = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// A nonlocking candidate read is followed by a conversation lock and CAS.
		// All writers lock conversation before tasks, avoiding opposite lock orders.
		var candidates []taskRow
		if err := tx.Table(table).Where("status = ? AND engine = ? AND next_retry_at <= ?", "pending", engineName, r.now().Unix()).Order("id ASC").Limit(32).Find(&candidates).Error; err != nil {
			return err
		}
		for _, candidate := range candidates {
			var conversation models.StrategyConversation
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&conversation, "id = ?", candidate.ConversationID).Error; err != nil {
				return err
			}
			if kind == "chat" {
				if conversation.CurrentPhase == "generating" {
					continue
				}
				var blockers int64
				if err := tx.Table(table).Where("conversation_id = ? AND engine = ? AND (status = ? OR (message_seq < ? AND status IN ?))", conversation.ID, engineName, "running", candidate.MessageSeq, []string{"pending", "failed"}).Count(&blockers).Error; err != nil {
					return err
				}
				if blockers > 0 {
					continue
				}
			} else if conversation.CurrentPhase != "generating" {
				continue
			}
			bytes := make([]byte, 24)
			if _, err := rand.Read(bytes); err != nil {
				return err
			}
			token := hex.EncodeToString(bytes)
			updated := tx.Table(table).Where("id = ? AND status = ? AND engine = ? AND next_retry_at <= ?", candidate.ID, "pending", engineName, r.now().Unix()).Updates(map[string]any{"status": "running", "attempt_token": token, "attempt_count": gorm.Expr("attempt_count + 1"), "lease_until": r.now().Add(r.lease).Unix(), "start_time": r.now().Unix(), "update_time": r.now().Unix()})
			if updated.Error != nil {
				return updated.Error
			}
			if updated.RowsAffected == 1 {
				claim = ClaimedTask{Kind: kind, TaskID: candidate.ID, AttemptToken: token}
				found = true
				return nil
			}
		}
		return nil
	})
	return
}

func (r *GormRepository) Renew(ctx context.Context, claim ClaimedTask) error {
	table, err := taskTable(claim.Kind)
	if err != nil {
		return err
	}
	updated := r.db.WithContext(ctx).Table(table).Where("id = ? AND status = ? AND engine = ? AND attempt_token = ? AND lease_until > ?", claim.TaskID, "running", engineName, claim.AttemptToken, r.now().Unix()).Updates(map[string]any{"lease_until": r.now().Add(r.lease).Unix(), "update_time": r.now().Unix()})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 {
		return ErrLeaseLost
	}
	return nil
}

func (r *GormRepository) Fail(ctx context.Context, claim ClaimedTask, cause error) error {
	return r.failTask(ctx, claim, cause, false)
}

func (r *GormRepository) failTask(ctx context.Context, claim ClaimedTask, cause error, expired bool) error {
	table, err := taskTable(claim.Kind)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var observed taskRow
		if err := tx.Table(table).Where("id = ?", claim.TaskID).Take(&observed).Error; err != nil {
			return err
		}
		var conversation models.StrategyConversation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&conversation, "id = ?", observed.ConversationID).Error; err != nil {
			return err
		}
		query := tx.Table(table).Where("id = ? AND status = ? AND engine = ? AND attempt_token = ?", claim.TaskID, "running", engineName, claim.AttemptToken)
		if expired {
			query = query.Where("lease_until <= ?", r.now().Unix())
		} else {
			query = query.Where("lease_until > ?", r.now().Unix())
		}
		status := "pending"
		if observed.AttemptCount >= r.maxAttempts {
			status = "failed"
		}
		delay := int64(5)
		if observed.AttemptCount >= 2 {
			delay = 30
		}
		// Keep raw adapter errors out of task rows: they may contain credentials.
		reason := "agent_execution_failed"
		if expired {
			reason = "lease_expired"
		}
		if errors.Is(cause, ErrLeaseLost) {
			reason = "lease_lost"
		}
		updated := query.Updates(map[string]any{"status": status, "attempt_token": "", "lease_until": 0, "next_retry_at": r.now().Unix() + delay, "last_error": reason, "update_time": r.now().Unix()})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected == 0 {
			// A successful Finish wins even if cancellation happened afterwards.
			var current taskRow
			if err := tx.Table(table).Where("id = ?", claim.TaskID).Take(&current).Error; err != nil {
				return err
			}
			if current.Status == "success" {
				return nil
			}
			return ErrLeaseLost
		}
		if status == "failed" {
			return tx.Model(&conversation).Updates(map[string]any{"current_phase": "failed", "update_time": r.now().Unix()}).Error
		}
		return nil
	})
}

func (r *GormRepository) Reap(ctx context.Context) error {
	for _, kind := range []string{"chat", "generate"} {
		table, _ := taskTable(kind)
		var expired []taskRow
		if err := r.db.WithContext(ctx).Table(table).Where("status = ? AND engine = ? AND lease_until <= ?", "running", engineName, r.now().Unix()).Limit(100).Find(&expired).Error; err != nil {
			return err
		}
		for _, row := range expired {
			if err := r.failTask(ctx, ClaimedTask{Kind: kind, TaskID: row.ID, AttemptToken: row.AttemptToken}, ErrLeaseLost, true); err != nil && !errors.Is(err, ErrLeaseLost) {
				return err
			}
		}
	}
	return nil
}
