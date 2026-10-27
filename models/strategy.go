// Package models contains service-owned database records shared by Agents.
// Table names and existing columns follow the admissions reference module.
package models

type Strategy struct {
	ID                  int64  `gorm:"column:id;primaryKey" json:"id"`
	SchoolID            int64  `gorm:"column:school_id" json:"schoolId"`
	Name                string `gorm:"column:name" json:"name"`
	Version             uint32 `gorm:"column:version" json:"version"`
	Status              int8   `gorm:"column:status" json:"status"`
	ConversationID      int64  `gorm:"column:conversation_id" json:"conversationId"`
	SnapshotID          int64  `gorm:"column:snapshot_id" json:"snapshotId"`
	PublishedArtifactID int64  `gorm:"column:published_artifact_id" json:"publishedArtifactId"`
	PublishTime         int64  `gorm:"column:publish_time" json:"publishTime"`
	CreateTime          int64  `gorm:"column:create_time" json:"createTime"`
	UpdateTime          int64  `gorm:"column:update_time" json:"updateTime"`
}

func (Strategy) TableName() string { return "tblStrategy" }

type StrategyConversation struct {
	ID                   int64  `gorm:"column:id;primaryKey" json:"id"`
	StrategyID           int64  `gorm:"column:strategy_id" json:"strategyId"`
	CurrentPhase         string `gorm:"column:current_phase" json:"currentPhase"`
	CollectedSummary     string `gorm:"column:collected_summary" json:"collectedSummary"`
	RequirementsRevision int64  `gorm:"column:requirements_revision" json:"requirementsRevision"`
	ProcessedMessageSeq  int64  `gorm:"column:processed_message_seq" json:"processedMessageSeq"`
	NextMessageSeq       int64  `gorm:"column:next_message_seq" json:"nextMessageSeq"`
	PreviewArtifactID    int64  `gorm:"column:preview_artifact_id" json:"previewArtifactId"`
	ConfirmTime          int64  `gorm:"column:confirm_time" json:"confirmTime"`
	ConfirmBy            int64  `gorm:"column:confirm_by" json:"confirmBy"`
	CreateTime           int64  `gorm:"column:create_time" json:"createTime"`
	UpdateTime           int64  `gorm:"column:update_time" json:"updateTime"`
}

func (StrategyConversation) TableName() string { return "tblStrategyConversation" }

type StrategyMessage struct {
	ID              int64   `gorm:"column:id;primaryKey" json:"id"`
	ConversationID  int64   `gorm:"column:conversation_id" json:"conversationId"`
	Seq             int64   `gorm:"column:seq" json:"seq"`
	Role            string  `gorm:"column:role" json:"role"`
	AgentType       string  `gorm:"column:agent_type" json:"agentType"`
	Content         string  `gorm:"column:content" json:"content"`
	Metadata        string  `gorm:"column:metadata" json:"metadata"`
	ClientRequestID *string `gorm:"column:client_request_id" json:"clientRequestId,omitempty"`
	CreateTime      int64   `gorm:"column:create_time" json:"createTime"`
}

func (StrategyMessage) TableName() string { return "tblStrategyMessage" }

// TaskExecution holds the new execution columns shared by the two existing
// task tables. It does not select a table or perform automatic migrations.
type TaskExecution struct {
	AttemptToken         string `gorm:"column:attempt_token" json:"-"`
	AttemptCount         int    `gorm:"column:attempt_count" json:"attemptCount"`
	LeaseUntil           int64  `gorm:"column:lease_until" json:"-"`
	NextRetryAt          int64  `gorm:"column:next_retry_at" json:"-"`
	Engine               string `gorm:"column:engine" json:"engine"`
	SnapshotJSON         string `gorm:"column:snapshot_json" json:"-"`
	RequirementsRevision int64  `gorm:"column:requirements_revision" json:"requirementsRevision"`
	ResultArtifactID     int64  `gorm:"column:result_artifact_id" json:"artifactId"`
}

type StrategyMessageTask struct {
	ID                 int64  `gorm:"column:id;primaryKey" json:"taskId"`
	ConversationID     int64  `gorm:"column:conversation_id" json:"conversationId"`
	StrategyID         int64  `gorm:"column:strategy_id" json:"strategyId"`
	UserMessageID      int64  `gorm:"column:user_message_id" json:"userMessageId"`
	MessageSeq         int64  `gorm:"column:message_seq" json:"messageSeq"`
	Status             string `gorm:"column:status" json:"status"`
	Mode               string `gorm:"column:mode" json:"mode"`
	AssistantMessageID int64  `gorm:"column:assistant_message_id" json:"assistantMessageId"`
	ResultJSON         string `gorm:"column:result_json" json:"-"`
	LastError          string `gorm:"column:last_error" json:"lastError"`
	StartTime          int64  `gorm:"column:start_time" json:"startTime"`
	FinishTime         int64  `gorm:"column:finish_time" json:"finishTime"`
	CreateTime         int64  `gorm:"column:create_time" json:"createTime"`
	UpdateTime         int64  `gorm:"column:update_time" json:"updateTime"`
	TaskExecution      `gorm:"embedded"`
}

func (StrategyMessageTask) TableName() string { return "tblStrategyMessageTask" }

type StrategyGenTask struct {
	ID             int64  `gorm:"column:id;primaryKey" json:"taskId"`
	StrategyID     int64  `gorm:"column:strategy_id" json:"strategyId"`
	ConversationID int64  `gorm:"column:conversation_id" json:"conversationId"`
	TriggerReason  string `gorm:"column:trigger_reason" json:"triggerReason"`
	InputSnapshot  string `gorm:"column:input_snapshot" json:"-"`
	Status         string `gorm:"column:status" json:"status"`
	CurrentStage   string `gorm:"column:current_stage" json:"currentStage"`
	Progress       int8   `gorm:"column:progress" json:"progress"`
	LastError      string `gorm:"column:last_error" json:"lastError"`
	StartTime      int64  `gorm:"column:start_time" json:"startTime"`
	FinishTime     int64  `gorm:"column:finish_time" json:"finishTime"`
	CreateTime     int64  `gorm:"column:create_time" json:"createTime"`
	UpdateTime     int64  `gorm:"column:update_time" json:"updateTime"`
	TaskExecution  `gorm:"embedded"`
}

func (StrategyGenTask) TableName() string { return "tblStrategyGenTask" }

type AgentDraft struct {
	ID             int64  `gorm:"column:id;primaryKey"`
	ConversationID int64  `gorm:"column:conversation_id;uniqueIndex"`
	Revision       int64  `gorm:"column:revision"`
	DraftJSON      string `gorm:"column:draft_json"`
	UpdateTime     int64  `gorm:"column:update_time"`
}

func (AgentDraft) TableName() string { return "tblStrategyAgentDraft" }

type AgentArtifact struct {
	ID             int64  `gorm:"column:id;primaryKey" json:"artifactId"`
	TaskID         int64  `gorm:"column:task_id;uniqueIndex" json:"taskId"`
	StrategyID     int64  `gorm:"column:strategy_id" json:"strategyId"`
	ConversationID int64  `gorm:"column:conversation_id" json:"conversationId"`
	Hash           string `gorm:"column:hash" json:"hash"`
	ArtifactJSON   string `gorm:"column:artifact_json" json:"-"`
	SnapshotJSON   string `gorm:"column:snapshot_json" json:"-"`
	Status         string `gorm:"column:status" json:"status"`
	CreateTime     int64  `gorm:"column:create_time" json:"createTime"`
}

func (AgentArtifact) TableName() string { return "tblStrategyAgentArtifact" }

type ConfigItem struct {
	ID         int64  `gorm:"column:id;primaryKey" json:"id"`
	SchoolID   int64  `gorm:"column:school_id" json:"schoolId"`
	ConfigType string `gorm:"column:config_type" json:"configType"`
	Name       string `gorm:"column:name" json:"name"`
	District   string `gorm:"column:district" json:"district"`
	DataJSON   string `gorm:"column:data_json" json:"dataJson"`
	Status     int8   `gorm:"column:status" json:"status"`
}

func (ConfigItem) TableName() string { return "tblConfigItem" }
