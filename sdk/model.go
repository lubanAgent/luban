package luban

import (
	"encoding/json"

	"github.com/gin-gonic/gin"
)

// ModelInvoker performs one model request. The service can use a configured
// default HTTP client or inject an existing business gateway implementation.
type ModelInvoker interface {
	Name() string
	Invoke(ctx *gin.Context, req ModelRequest) (ModelResponse, error)
}

// ModelRequest is the provider-neutral representation of one model request.
type ModelRequest struct {
	ModelName   string
	Messages    []ModelMessage
	Tools       []ToolDefinition
	Temperature *float64
}

// ModelMessage preserves message content and tool-call relationships.
type ModelMessage struct {
	Role       ModelRole
	Content    []ModelPart
	ToolCalls  []ToolCall
	ToolCallID string
	ToolName   string
}

// ModelRole identifies a provider-neutral conversation role.
type ModelRole string

const (
	ModelRoleSystem    ModelRole = "system"    // ModelRoleSystem marks system instructions.
	ModelRoleUser      ModelRole = "user"      // ModelRoleUser marks user input.
	ModelRoleAssistant ModelRole = "assistant" // ModelRoleAssistant marks model output.
	ModelRoleTool      ModelRole = "tool"      // ModelRoleTool marks a tool result.
)

// ModelPart is one text or URL-backed media item in a model message.
type ModelPart struct {
	Type PartType
	Text string
	URL  string
}

// ToolDefinition describes a tool and its JSON Schema parameters.
type ToolDefinition struct {
	Name        string
	Description string
	Parameters  json.RawMessage
}

// ModelResponse is one provider-neutral model response.
type ModelResponse struct {
	Content      []ModelPart
	ToolCalls    []ToolCall
	FinishReason string
	// Usage 是网关返回的本次请求用量；未提供时留 nil，不估算 Token。
	Usage *ModelUsage `json:"usage,omitempty"`
}

// ModelUsage 表示一次模型请求的真实用量，不包含网关内部重试的累计消耗。
// InputTokens 包括网关统计的全部输入（含命中缓存的部分）。
type ModelUsage struct {
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
}

// ToolCall is one tool requested by the model.
type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}
