package eino

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/gin-gonic/gin"
	internaltrace "github.com/lubanAgent/luban/sdk/internal/trace"
)

type einoModelAdapter struct {
	invoker     ModelInvoker
	modelName   string
	temperature *float64
}

var _ model.BaseChatModel = (*einoModelAdapter)(nil)

// NewModelAdapter creates the Eino ChatModel wrapper around a model invoker.
func NewModelAdapter(modelName string, temperature *float64, invoker ModelInvoker) model.BaseChatModel {
	return &einoModelAdapter{
		invoker:     invoker,
		modelName:   modelName,
		temperature: cloneFloat64(temperature),
	}
}

func (m *einoModelAdapter) Generate(ctx context.Context, messages []*schema.Message, opts ...model.Option) (message *schema.Message, resultErr error) {
	if m == nil || isNilInterface(m.invoker) {
		return nil, fmt.Errorf("luban: model invoker is nil")
	}

	ginCtx, err := ginContextFrom(ctx)
	if err != nil {
		return nil, err
	}

	request, err := m.makeRequest(messages, opts...)
	if err != nil {
		return nil, err
	}
	scope, _ := ginCtx.Get(agentScopeGinKey)
	agentName := ""
	if agent, ok := scope.(AgentScope); ok {
		agentName = agent.Name
	}
	_, span := internaltrace.Start(ctx, "model", m.modelName, nil, agentName, request)
	response, err := invokeModel(m.invoker, ginCtx, request)
	defer func() { span.End(response, "", resultErr) }()
	if err != nil {
		return nil, fmt.Errorf("luban: model %q: %w", m.modelName, err)
	}
	message, err = responseToEinoMessage(response)
	if err != nil {
		return nil, fmt.Errorf("luban: model %q response: %w", m.modelName, err)
	}
	return message, nil
}

func (m *einoModelAdapter) Stream(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	message, err := m.Generate(ctx, messages, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

func (m *einoModelAdapter) makeRequest(messages []*schema.Message, opts ...model.Option) (ModelRequest, error) {
	base := &model.Options{}
	if m.temperature != nil {
		temperature := float32(*m.temperature)
		base.Temperature = &temperature
	}
	common := model.GetCommonOptions(base, opts...)

	request := ModelRequest{
		ModelName: m.modelName,
		Messages:  make([]ModelMessage, 0, len(messages)),
	}
	if common.Temperature != nil {
		temperature := float64(*common.Temperature)
		request.Temperature = &temperature
	}
	for i, message := range messages {
		converted, err := messageFromEino(message)
		if err != nil {
			return ModelRequest{}, fmt.Errorf("message[%d]: %w", i, err)
		}
		request.Messages = append(request.Messages, converted)
	}
	for i, info := range common.Tools {
		if info == nil {
			return ModelRequest{}, fmt.Errorf("tool schema[%d] is nil", i)
		}
		definition := ToolDefinition{Name: info.Name, Description: info.Desc}
		if info.ParamsOneOf != nil {
			jsonSchema, err := info.ParamsOneOf.ToJSONSchema()
			if err != nil {
				return ModelRequest{}, fmt.Errorf("tool %q schema: %w", info.Name, err)
			}
			if jsonSchema != nil {
				parameters, err := json.Marshal(jsonSchema)
				if err != nil {
					return ModelRequest{}, fmt.Errorf("tool %q schema: %w", info.Name, err)
				}
				definition.Parameters = parameters
			}
		}
		request.Tools = append(request.Tools, definition)
	}
	return request, nil
}

func invokeModel(invoker ModelInvoker, ctx *gin.Context, request ModelRequest) (response ModelResponse, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			response = ModelResponse{}
			err = fmt.Errorf("ModelInvoker panicked: %v", recovered)
		}
	}()
	return invoker.Invoke(ctx, request)
}

func messageFromEino(message *schema.Message) (ModelMessage, error) {
	if message == nil {
		return ModelMessage{}, fmt.Errorf("message is nil")
	}
	out := ModelMessage{
		Role:       ModelRole(message.Role),
		ToolCallID: message.ToolCallID,
		ToolName:   message.ToolName,
	}
	switch message.Role {
	case schema.System, schema.User, schema.Assistant, schema.Tool:
	default:
		return ModelMessage{}, fmt.Errorf("unsupported message role %q", message.Role)
	}

	switch message.Role {
	case schema.User:
		switch {
		case len(message.UserInputMultiContent) > 0:
			for i, part := range message.UserInputMultiContent {
				converted, err := modelPartFromEinoInput(part)
				if err != nil {
					return ModelMessage{}, fmt.Errorf("user content[%d]: %w", i, err)
				}
				out.Content = append(out.Content, converted)
			}
		case len(message.MultiContent) > 0:
			for i, part := range message.MultiContent {
				converted, err := modelPartFromEinoLegacy(part)
				if err != nil {
					return ModelMessage{}, fmt.Errorf("user content[%d]: %w", i, err)
				}
				out.Content = append(out.Content, converted)
			}
		default:
			out.Content = append(out.Content, ModelPart{Type: PartText, Text: message.Content})
		}
	case schema.Assistant:
		if len(message.AssistantGenMultiContent) > 0 {
			for i, part := range message.AssistantGenMultiContent {
				converted, err := modelPartFromEinoOutput(part)
				if err != nil {
					return ModelMessage{}, fmt.Errorf("assistant content[%d]: %w", i, err)
				}
				out.Content = append(out.Content, converted)
			}
		} else {
			out.Content = append(out.Content, ModelPart{Type: PartText, Text: message.Content})
		}
	case schema.System, schema.Tool:
		out.Content = append(out.Content, ModelPart{Type: PartText, Text: message.Content})
	}

	for _, call := range message.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, ToolCall{
			ID:        call.ID,
			Name:      call.Function.Name,
			Arguments: call.Function.Arguments,
		})
	}
	return out, nil
}

func modelPartFromEinoInput(part schema.MessageInputPart) (ModelPart, error) {
	switch part.Type {
	case schema.ChatMessagePartTypeText:
		return ModelPart{Type: PartText, Text: part.Text}, nil
	case schema.ChatMessagePartTypeImageURL:
		if part.Image == nil {
			return ModelPart{}, fmt.Errorf("image URL is empty")
		}
		return modelPartFromCommon(PartImage, &part.Image.MessagePartCommon)
	case schema.ChatMessagePartTypeVideoURL:
		if part.Video == nil {
			return ModelPart{}, fmt.Errorf("video URL is empty")
		}
		return modelPartFromCommon(PartVideo, &part.Video.MessagePartCommon)
	case schema.ChatMessagePartTypeAudioURL:
		if part.Audio == nil {
			return ModelPart{}, fmt.Errorf("audio URL is empty")
		}
		return modelPartFromCommon(PartAudio, &part.Audio.MessagePartCommon)
	default:
		return ModelPart{}, fmt.Errorf("unsupported user content type %q", part.Type)
	}
}

func modelPartFromEinoOutput(part schema.MessageOutputPart) (ModelPart, error) {
	switch part.Type {
	case schema.ChatMessagePartTypeText:
		return ModelPart{Type: PartText, Text: part.Text}, nil
	case schema.ChatMessagePartTypeImageURL:
		if part.Image == nil {
			return ModelPart{}, fmt.Errorf("image URL is empty")
		}
		return modelPartFromCommon(PartImage, &part.Image.MessagePartCommon)
	case schema.ChatMessagePartTypeVideoURL:
		if part.Video == nil {
			return ModelPart{}, fmt.Errorf("video URL is empty")
		}
		return modelPartFromCommon(PartVideo, &part.Video.MessagePartCommon)
	case schema.ChatMessagePartTypeAudioURL:
		if part.Audio == nil {
			return ModelPart{}, fmt.Errorf("audio URL is empty")
		}
		return modelPartFromCommon(PartAudio, &part.Audio.MessagePartCommon)
	default:
		return ModelPart{}, fmt.Errorf("unsupported assistant content type %q", part.Type)
	}
}

func modelPartFromEinoLegacy(part schema.ChatMessagePart) (ModelPart, error) {
	switch part.Type {
	case schema.ChatMessagePartTypeText:
		return ModelPart{Type: PartText, Text: part.Text}, nil
	case schema.ChatMessagePartTypeImageURL:
		if part.ImageURL == nil {
			return ModelPart{}, fmt.Errorf("image URL is empty")
		}
		url := firstNonEmpty(part.ImageURL.URL, part.ImageURL.URI)
		if url == "" {
			return ModelPart{}, fmt.Errorf("image URL is empty")
		}
		return ModelPart{Type: PartImage, URL: url}, nil
	case schema.ChatMessagePartTypeVideoURL:
		if part.VideoURL == nil {
			return ModelPart{}, fmt.Errorf("video URL is empty")
		}
		url := firstNonEmpty(part.VideoURL.URL, part.VideoURL.URI)
		if url == "" {
			return ModelPart{}, fmt.Errorf("video URL is empty")
		}
		return ModelPart{Type: PartVideo, URL: url}, nil
	case schema.ChatMessagePartTypeAudioURL:
		if part.AudioURL == nil {
			return ModelPart{}, fmt.Errorf("audio URL is empty")
		}
		url := firstNonEmpty(part.AudioURL.URL, part.AudioURL.URI)
		if url == "" {
			return ModelPart{}, fmt.Errorf("audio URL is empty")
		}
		return ModelPart{Type: PartAudio, URL: url}, nil
	default:
		return ModelPart{}, fmt.Errorf("unsupported legacy content type %q", part.Type)
	}
}

func modelPartFromCommon(partType PartType, common *schema.MessagePartCommon) (ModelPart, error) {
	if common == nil {
		return ModelPart{}, fmt.Errorf("%s URL is empty", partType)
	}
	if common.Base64Data != nil {
		return ModelPart{}, fmt.Errorf("%s base64 content is not supported by ModelPart", partType)
	}
	if common.URL == nil || *common.URL == "" {
		return ModelPart{}, fmt.Errorf("%s URL is empty", partType)
	}
	return ModelPart{Type: partType, URL: *common.URL}, nil
}

func responseToEinoMessage(response ModelResponse) (*schema.Message, error) {
	message := &schema.Message{Role: schema.Assistant}
	allText := true
	var text strings.Builder
	for _, part := range response.Content {
		switch part.Type {
		case PartText:
			text.WriteString(part.Text)
		case PartImage, PartVideo, PartAudio:
			allText = false
			if part.URL == "" {
				return nil, fmt.Errorf("%s response URL is empty", part.Type)
			}
		default:
			return nil, fmt.Errorf("unsupported response content type %q", part.Type)
		}
	}
	if allText {
		message.Content = text.String()
	} else {
		// Rebuild mixed content in one pass so text/media ordering is preserved.
		message.AssistantGenMultiContent = make([]schema.MessageOutputPart, 0, len(response.Content))
		for _, part := range response.Content {
			switch part.Type {
			case PartText:
				message.AssistantGenMultiContent = append(message.AssistantGenMultiContent, schema.MessageOutputPart{
					Type: schema.ChatMessagePartTypeText,
					Text: part.Text,
				})
			case PartImage:
				url := part.URL
				message.AssistantGenMultiContent = append(message.AssistantGenMultiContent, schema.MessageOutputPart{
					Type: schema.ChatMessagePartTypeImageURL,
					Image: &schema.MessageOutputImage{
						MessagePartCommon: schema.MessagePartCommon{URL: &url},
					},
				})
			case PartVideo:
				url := part.URL
				message.AssistantGenMultiContent = append(message.AssistantGenMultiContent, schema.MessageOutputPart{
					Type: schema.ChatMessagePartTypeVideoURL,
					Video: &schema.MessageOutputVideo{
						MessagePartCommon: schema.MessagePartCommon{URL: &url},
					},
				})
			case PartAudio:
				url := part.URL
				message.AssistantGenMultiContent = append(message.AssistantGenMultiContent, schema.MessageOutputPart{
					Type: schema.ChatMessagePartTypeAudioURL,
					Audio: &schema.MessageOutputAudio{
						MessagePartCommon: schema.MessagePartCommon{URL: &url},
					},
				})
			}
		}
	}
	for _, call := range response.ToolCalls {
		message.ToolCalls = append(message.ToolCalls, schema.ToolCall{
			ID:   call.ID,
			Type: "function",
			Function: schema.FunctionCall{
				Name:      call.Name,
				Arguments: call.Arguments,
			},
		})
	}
	if response.FinishReason != "" || response.Usage != nil {
		message.ResponseMeta = &schema.ResponseMeta{FinishReason: response.FinishReason}
		if response.Usage != nil {
			message.ResponseMeta.Usage = &schema.TokenUsage{
				PromptTokens: response.Usage.InputTokens, CompletionTokens: response.Usage.OutputTokens,
				TotalTokens: response.Usage.InputTokens + response.Usage.OutputTokens,
			}
		}
	}
	return message, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func stringPointer(value string) *string {
	return &value
}
