package luban

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
)

type anthropicMessage struct {
	Role    string           `json:"role"`
	Content []map[string]any `json:"content"`
}

func (m *protocolModel) invokeAnthropic(ctx *gin.Context, request ModelRequest) (ModelResponse, error) {
	if request.Temperature != nil && (*request.Temperature < 0 || *request.Temperature > 1) {
		return ModelResponse{}, fmt.Errorf("anthropic temperature must be between 0 and 1")
	}
	var system []map[string]any
	messages := make([]anthropicMessage, 0, len(request.Messages))
	for _, message := range request.Messages {
		parts, err := anthropicContent(message.Content)
		if err != nil {
			return ModelResponse{}, err
		}
		if message.Role == ModelRoleSystem {
			system = append(system, parts...)
			continue
		}
		role := string(message.Role)
		if message.Role == ModelRoleTool {
			role = "user"
			parts = []map[string]any{{"type": "tool_result", "tool_use_id": message.ToolCallID, "content": parts}}
		}
		for _, call := range message.ToolCalls {
			var input map[string]any
			if json.Unmarshal([]byte(call.Arguments), &input) != nil || input == nil {
				return ModelResponse{}, fmt.Errorf("anthropic tool %q arguments must be a JSON object", call.Name)
			}
			parts = append(parts, map[string]any{"type": "tool_use", "id": call.ID, "name": call.Name, "input": input})
		}
		// Parallel tool results belong to one user turn in the Messages API.
		if len(messages) > 0 && messages[len(messages)-1].Role == role {
			messages[len(messages)-1].Content = append(messages[len(messages)-1].Content, parts...)
		} else {
			messages = append(messages, anthropicMessage{Role: role, Content: parts})
		}
	}
	payload := map[string]any{"model": request.ModelName, "max_tokens": 4096, "messages": messages}
	if len(system) > 0 {
		payload["system"] = system
	}
	if request.Temperature != nil {
		payload["temperature"] = *request.Temperature
	}
	if len(request.Tools) > 0 {
		tools := make([]any, 0, len(request.Tools))
		for _, definition := range request.Tools {
			schema := definition.Parameters
			if len(schema) == 0 {
				schema = json.RawMessage(`{"type":"object","properties":{}}`)
			}
			tools = append(tools, map[string]any{"name": definition.Name, "description": definition.Description, "input_schema": schema})
		}
		payload["tools"] = tools
	}
	var response struct {
		Type    string `json:"type"`
		Content []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		Usage      *struct {
			InputTokens         int `json:"input_tokens"`
			OutputTokens        int `json:"output_tokens"`
			CacheReadTokens     int `json:"cache_read_input_tokens"`
			CacheCreationTokens int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	}
	if err := m.post(ctx, modelEndpoint(m.connection.BaseURL, "anthropic"), payload, &response); err != nil {
		return ModelResponse{}, err
	}
	if response.Type != "message" || len(response.Content) == 0 {
		return ModelResponse{}, fmt.Errorf("anthropic response has no message content")
	}
	result := ModelResponse{FinishReason: response.StopReason}
	for _, part := range response.Content {
		switch part.Type {
		case "text":
			result.Content = append(result.Content, ModelPart{Type: PartText, Text: part.Text})
		case "tool_use":
			var input map[string]any
			if part.ID == "" || part.Name == "" || json.Unmarshal(part.Input, &input) != nil || input == nil {
				return ModelResponse{}, fmt.Errorf("anthropic response contains an invalid tool call")
			}
			result.ToolCalls = append(result.ToolCalls, ToolCall{ID: part.ID, Name: part.Name, Arguments: string(part.Input)})
		default:
			return ModelResponse{}, fmt.Errorf("unsupported anthropic response content type %q", part.Type)
		}
	}
	if response.Usage != nil {
		usage := response.Usage
		result.Usage = &ModelUsage{InputTokens: usage.InputTokens + usage.CacheReadTokens + usage.CacheCreationTokens, OutputTokens: usage.OutputTokens}
	}
	return result, nil
}

func anthropicContent(parts []ModelPart) ([]map[string]any, error) {
	content := make([]map[string]any, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case PartText:
			if part.Text != "" {
				content = append(content, map[string]any{"type": "text", "text": part.Text})
			}
		case PartImage:
			if part.URL == "" {
				return nil, fmt.Errorf("image URL is empty")
			}
			source := map[string]any{"type": "url", "url": part.URL}
			if strings.HasPrefix(part.URL, "data:") {
				header, data, ok := strings.Cut(strings.TrimPrefix(part.URL, "data:"), ",")
				mediaType, encoded := strings.CutSuffix(header, ";base64")
				if !ok || !encoded || (mediaType != "image/jpeg" && mediaType != "image/png" && mediaType != "image/gif" && mediaType != "image/webp") {
					return nil, fmt.Errorf("unsupported anthropic image data URL")
				}
				if _, err := base64.StdEncoding.DecodeString(data); err != nil {
					return nil, fmt.Errorf("invalid anthropic image base64 data")
				}
				source = map[string]any{"type": "base64", "media_type": mediaType, "data": data}
			}
			content = append(content, map[string]any{"type": "image", "source": source})
		default:
			return nil, fmt.Errorf("unsupported anthropic media type %q", part.Type)
		}
	}
	return content, nil
}
