package eino

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/gin-gonic/gin"
)

type PartType string

const (
	PartText  PartType = "text"
	PartImage PartType = "image"
	PartVideo PartType = "video"
	PartAudio PartType = "audio"
)

// Part is one provider-neutral content item used at the Eino boundary.
type Part struct {
	Type PartType
	Text string
	URL  string
}

type ModelPart = Part
type ModelRole = string

const (
	ModelRoleSystem    ModelRole = "system"
	ModelRoleUser      ModelRole = "user"
	ModelRoleAssistant ModelRole = "assistant"
	ModelRoleTool      ModelRole = "tool"
)

// ToolCall preserves Eino's tool call identifier and arguments.
type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}

// Message is an internal provider-neutral conversation message.
type Message struct {
	Role       string
	Content    []Part
	ToolCalls  []ToolCall
	ToolCallID string
	ToolName   string
}

type ModelMessage = Message

// ToolDefinition carries the Eino-generated JSON Schema.
type ToolDefinition struct {
	Name        string
	Description string
	Parameters  json.RawMessage
}

// ModelRequest is the internal request passed to a business model adapter.
type ModelRequest struct {
	ModelName   string
	Messages    []Message
	Tools       []ToolDefinition
	Temperature *float64
}

// ModelResponse is one provider-neutral model response.
type ModelResponse struct {
	Content      []Part
	ToolCalls    []ToolCall
	FinishReason string
	Usage        *ModelUsage `json:"usage,omitempty"`
}

// ModelUsage 只保存业务适配器实际返回的用量。
type ModelUsage struct {
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
}

// ModelInvoker performs one model request. The host package adapts its public
// ModelInvoker contract to this internal protocol.
type ModelInvoker interface {
	Invoke(*gin.Context, ModelRequest) (ModelResponse, error)
}

type ginContextKey struct{}

// WithGinContext binds the caller's Gin context to the standard context used
// by Eino. It returns an error rather than creating an empty fallback.
func WithGinContext(parent context.Context, ginContext *gin.Context) (context.Context, error) {
	if parent == nil {
		return nil, fmt.Errorf("luban: standard context must not be nil")
	}
	if ginContext == nil {
		return nil, fmt.Errorf("luban: Gin context must not be nil")
	}
	return context.WithValue(parent, ginContextKey{}, ginContext), nil
}

// GinContextFrom returns the Gin context previously bound with
// WithGinContext.
func GinContextFrom(ctx context.Context) (*gin.Context, error) {
	if ctx == nil {
		return nil, fmt.Errorf("luban: standard context must not be nil")
	}
	value := ctx.Value(ginContextKey{})
	ginContext, ok := value.(*gin.Context)
	if !ok || ginContext == nil {
		return nil, fmt.Errorf("luban: Gin context binding was lost")
	}
	return ginContext, nil
}

func ginContextFrom(ctx context.Context) (*gin.Context, error) {
	return GinContextFrom(ctx)
}

func isNilInterface(value any) bool {
	if value == nil {
		return true
	}
	ref := reflect.ValueOf(value)
	switch ref.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return ref.IsNil()
	default:
		return false
	}
}

func cloneFloat64(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
