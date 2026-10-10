package luban

import (
	"encoding/json"

	"github.com/gin-gonic/gin"
)

// ModelInvoker performs one model HTTP request. The business module owns the
// gateway client, authentication, retries, and provider-specific protocol.
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
}

// ToolCall is one tool requested by the model.
type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}
