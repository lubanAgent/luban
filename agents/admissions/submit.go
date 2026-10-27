package admissions

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lubanAgent/luban/models/strategyrepo"
)

// Submission is a business command: one request either adds a message or
// confirms the current requirement version. Neither action runs the Agent in
// the HTTP request; both enqueue a durable task for the admissions worker.
type Submission struct {
	Action                       string `json:"action"`
	SchoolID                     int64  `json:"schoolId"`
	ConversationID               int64  `json:"conversationId"`
	Name                         string `json:"name"`
	Content                      string `json:"content"`
	RequestID                    string `json:"requestId"`
	ExpectedRequirementsRevision int64  `json:"expectedRequirementsRevision"`
	ExpectedMessageSeq           int64  `json:"expectedMessageSeq"`
}

type SubmissionResult struct {
	ConversationID int64  `json:"conversationId"`
	TaskID         int64  `json:"taskId"`
	Status         string `json:"status"`
}

// Submit applies the admissions-specific rules; database writes and version
// checks remain atomic in the repository.
func Submit(ctx *gin.Context, repository *strategyrepo.GormRepository, input Submission) (SubmissionResult, error) {
	if input.SchoolID <= 0 {
		return SubmissionResult{}, fmt.Errorf("schoolId is required")
	}
	switch input.Action {
	case "message":
		if input.Content == "" || input.ExpectedRequirementsRevision != 0 || input.ExpectedMessageSeq != 0 {
			return SubmissionResult{}, fmt.Errorf("invalid message input")
		}
		if input.ConversationID == 0 {
			name := input.Name
			if name == "" {
				name = "志愿填报"
			}
			conversation, task, err := repository.CreateAndSubmitMessage(ctx.Request.Context(), input.SchoolID, name, input.Content, input.RequestID)
			if err != nil {
				return SubmissionResult{}, err
			}
			return SubmissionResult{ConversationID: conversation.ID, TaskID: task.ID, Status: task.Status}, nil
		}
		if input.ConversationID < 0 || input.Name != "" {
			return SubmissionResult{}, fmt.Errorf("invalid conversationId or name")
		}
		task, err := repository.SubmitMessage(ctx.Request.Context(), input.SchoolID, input.ConversationID, input.Content, input.RequestID)
		if err != nil {
			return SubmissionResult{}, err
		}
		return SubmissionResult{ConversationID: input.ConversationID, TaskID: task.ID, Status: task.Status}, nil
	case "confirm":
		if input.ConversationID <= 0 || input.ExpectedRequirementsRevision <= 0 || input.ExpectedMessageSeq <= 0 ||
			input.Content != "" || input.Name != "" || input.RequestID != "" {
			return SubmissionResult{}, fmt.Errorf("invalid confirmation input")
		}
		// The identity comes from trusted middleware, never from JSON.
		task, err := repository.Confirm(ctx.Request.Context(), input.SchoolID, input.ConversationID,
			input.ExpectedRequirementsRevision, input.ExpectedMessageSeq, ctx.GetInt64("operatorId"))
		if err != nil {
			return SubmissionResult{}, err
		}
		return SubmissionResult{ConversationID: input.ConversationID, TaskID: task.ID, Status: task.Status}, nil
	default:
		return SubmissionResult{}, fmt.Errorf("action must be message or confirm")
	}
}

// SubmitTo captures the admissions repository for the generic HTTP submission
// adapter. The Worker, rather than this callback, later runs the Agent.
func SubmitTo(repository *strategyrepo.GormRepository) func(*gin.Context, Submission) (SubmissionResult, error) {
	return func(ctx *gin.Context, input Submission) (SubmissionResult, error) {
		return Submit(ctx, repository, input)
	}
}

// RenderSubmission preserves the admissions business response protocol.
func RenderSubmission(ctx *gin.Context, output any, err error) {
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"errNo": 1, "errMsg": "Business operation failed; check task status or service logs", "data": gin.H{}})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"errNo": 0, "errMsg": "", "data": output})
}
