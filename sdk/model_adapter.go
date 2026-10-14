package luban

import (
	"fmt"

	"github.com/gin-gonic/gin"
	internaleino "github.com/lubanAgent/luban/sdk/internal/eino"
)

// publicModelInvoker adapts the public provider-neutral protocol to the
// Eino-facing internal protocol without making internal packages import the
// public luban package.
type publicModelInvoker struct {
	invoker ModelInvoker
}

func (m publicModelInvoker) Invoke(ctx *gin.Context, request internaleino.ModelRequest) (internaleino.ModelResponse, error) {
	response, err := invokePublicModel(m.invoker, ctx, modelRequestFromInternal(request))
	if err != nil {
		return internaleino.ModelResponse{}, err
	}
	return modelResponseToInternal(response), nil
}

func invokePublicModel(invoker ModelInvoker, ctx *gin.Context, request ModelRequest) (response ModelResponse, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			response = ModelResponse{}
			err = fmt.Errorf("ModelInvoker panicked: %v", recovered)
		}
	}()
	return invoker.Invoke(ctx, request)
}

func modelRequestFromInternal(request internaleino.ModelRequest) ModelRequest {
	result := ModelRequest{
		ModelName: request.ModelName,
		Tools:     make([]ToolDefinition, 0, len(request.Tools)),
	}
	if request.Temperature != nil {
		temperature := *request.Temperature
		result.Temperature = &temperature
	}
	for _, message := range request.Messages {
		converted := ModelMessage{
			Role:       ModelRole(message.Role),
			ToolCallID: message.ToolCallID,
			ToolName:   message.ToolName,
		}
		for _, part := range message.Content {
			converted.Content = append(converted.Content, ModelPart{
				Type: PartType(part.Type),
				Text: part.Text,
				URL:  part.URL,
			})
		}
		for _, call := range message.ToolCalls {
			converted.ToolCalls = append(converted.ToolCalls, ToolCall{
				ID:        call.ID,
				Name:      call.Name,
				Arguments: call.Arguments,
			})
		}
		result.Messages = append(result.Messages, converted)
	}
	for _, definition := range request.Tools {
		result.Tools = append(result.Tools, ToolDefinition{
			Name:        definition.Name,
			Description: definition.Description,
			Parameters:  append([]byte(nil), definition.Parameters...),
		})
	}
	return result
}

func modelResponseToInternal(response ModelResponse) internaleino.ModelResponse {
	result := internaleino.ModelResponse{FinishReason: response.FinishReason}
	if response.Usage != nil {
		result.Usage = &internaleino.ModelUsage{
			InputTokens: response.Usage.InputTokens, OutputTokens: response.Usage.OutputTokens,
		}
	}
	for _, part := range response.Content {
		result.Content = append(result.Content, internaleino.Part{
			Type: internaleino.PartType(part.Type),
			Text: part.Text,
			URL:  part.URL,
		})
	}
	for _, call := range response.ToolCalls {
		result.ToolCalls = append(result.ToolCalls, internaleino.ToolCall{
			ID:        call.ID,
			Name:      call.Name,
			Arguments: call.Arguments,
		})
	}
	return result
}
