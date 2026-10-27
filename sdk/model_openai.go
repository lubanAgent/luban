package luban

import (
	"encoding/json"
	"fmt"

	"github.com/gin-gonic/gin"
)

type openAIMessage struct {
	Role       ModelRole        `json:"role"`
	Content    any              `json:"content"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

func (m *protocolModel) invokeOpenAI(ctx *gin.Context, request ModelRequest) (ModelResponse, error) {
	if request.Temperature != nil && (*request.Temperature < 0 || *request.Temperature > 2) {
		return ModelResponse{}, fmt.Errorf("openai temperature must be between 0 and 2")
	}
	messages := make([]openAIMessage, 0, len(request.Messages))
	for _, message := range request.Messages {
		content, err := openAIContent(message.Content)
		if err != nil {
			return ModelResponse{}, err
		}
		converted := openAIMessage{Role: message.Role, Content: content, ToolCallID: message.ToolCallID}
		for _, call := range message.ToolCalls {
			wire := openAIToolCall{ID: call.ID, Type: "function"}
			wire.Function.Name, wire.Function.Arguments = call.Name, call.Arguments
			converted.ToolCalls = append(converted.ToolCalls, wire)
		}
		messages = append(messages, converted)
	}
	payload := map[string]any{"model": request.ModelName, "messages": messages}
	if request.Temperature != nil {
		payload["temperature"] = *request.Temperature
	}
	if len(request.Tools) > 0 {
		tools := make([]any, 0, len(request.Tools))
		for _, definition := range request.Tools {
			parameters := definition.Parameters
			if len(parameters) == 0 {
				parameters = json.RawMessage(`{"type":"object","properties":{}}`)
			}
			tools = append(tools, map[string]any{"type": "function", "function": map[string]any{
				"name": definition.Name, "description": definition.Description, "parameters": parameters,
			}})
		}
		payload["tools"], payload["tool_choice"] = tools, "auto"
	}
	var response struct {
		Choices []struct {
			Message struct {
				Content   json.RawMessage  `json:"content"`
				ToolCalls []openAIToolCall `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := m.post(ctx, modelEndpoint(m.connection.BaseURL, "openai"), payload, &response); err != nil {
		return ModelResponse{}, err
	}
	if len(response.Choices) == 0 {
		return ModelResponse{}, fmt.Errorf("openai response has no choices")
	}
	choice := response.Choices[0]
	result := ModelResponse{FinishReason: choice.FinishReason}
	if len(choice.Message.Content) > 0 && string(choice.Message.Content) != "null" {
		var text string
		if err := json.Unmarshal(choice.Message.Content, &text); err == nil {
			result.Content = []ModelPart{{Type: PartText, Text: text}}
		} else {
			var parts []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if json.Unmarshal(choice.Message.Content, &parts) != nil {
				return ModelResponse{}, fmt.Errorf("openai response content is not text")
			}
			for _, part := range parts {
				if part.Type != "text" {
					return ModelResponse{}, fmt.Errorf("unsupported openai response content type %q", part.Type)
				}
				result.Content = append(result.Content, ModelPart{Type: PartText, Text: part.Text})
			}
		}
	}
	for _, call := range choice.Message.ToolCalls {
		if call.Type != "function" || call.ID == "" || call.Function.Name == "" {
			return ModelResponse{}, fmt.Errorf("openai response contains an invalid tool call")
		}
		result.ToolCalls = append(result.ToolCalls, ToolCall{ID: call.ID, Name: call.Function.Name, Arguments: call.Function.Arguments})
	}
	if response.Usage != nil {
		result.Usage = &ModelUsage{InputTokens: response.Usage.PromptTokens, OutputTokens: response.Usage.CompletionTokens}
	}
	return result, nil
}

func openAIContent(parts []ModelPart) (any, error) {
	allText, text := true, ""
	content := make([]any, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case PartText:
			text += part.Text
			content = append(content, map[string]any{"type": "text", "text": part.Text})
		case PartImage, PartVideo:
			if part.URL == "" {
				return nil, fmt.Errorf("%s URL is empty", part.Type)
			}
			allText = false
			key := string(part.Type) + "_url"
			content = append(content, map[string]any{"type": key, key: map[string]any{"url": part.URL}})
		default:
			return nil, fmt.Errorf("unsupported openai media type %q", part.Type)
		}
	}
	if allText {
		return text, nil
	}
	return content, nil
}
