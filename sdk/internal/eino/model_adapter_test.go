package eino

import (
	"context"
	"errors"
	"math"
	"net/http"
	"reflect"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/gin-gonic/gin"
)

type recordingModelInvoker struct {
	request  ModelRequest
	response ModelResponse
	err      error
	panicVal any
	calls    int
}

func (m *recordingModelInvoker) Name() string { return "gateway" }

func (m *recordingModelInvoker) Invoke(_ *gin.Context, request ModelRequest) (ModelResponse, error) {
	m.calls++
	m.request = request
	if m.panicVal != nil {
		panic(m.panicVal)
	}
	return m.response, m.err
}

func ginStdContext(t *testing.T) (context.Context, *gin.Context) {
	t.Helper()
	request, _ := http.NewRequest(http.MethodPost, "/", nil)
	ginCtx := &gin.Context{Request: request}
	ctx, err := WithGinContext(request.Context(), ginCtx)
	if err != nil {
		t.Fatal(err)
	}
	return ctx, ginCtx
}

func TestEinoModelAdapterMapsMessagesToolsAndTemperature(t *testing.T) {
	invoker := &recordingModelInvoker{response: ModelResponse{
		Content: []ModelPart{{Type: PartText, Text: "answer"}},
		ToolCalls: []ToolCall{{
			ID: "call-1", Name: "lookup", Arguments: `{"id":7}`,
		}},
		FinishReason: "tool_calls",
	}}
	temperature := 0.0
	adapter := &einoModelAdapter{invoker: invoker, modelName: "review-model", temperature: &temperature}
	ctx, _ := ginStdContext(t)
	input := []*schema.Message{
		schema.SystemMessage("system prompt"),
		{
			Role: schema.User,
			UserInputMultiContent: []schema.MessageInputPart{
				{Type: schema.ChatMessagePartTypeText, Text: "review"},
				{Type: schema.ChatMessagePartTypeImageURL, Image: &schema.MessageInputImage{
					MessagePartCommon: schema.MessagePartCommon{URL: stringPointer("https://example.test/image.png")},
				}},
				{Type: schema.ChatMessagePartTypeVideoURL, Video: &schema.MessageInputVideo{
					MessagePartCommon: schema.MessagePartCommon{URL: stringPointer("https://example.test/lesson.mp4")},
				}},
				{Type: schema.ChatMessagePartTypeAudioURL, Audio: &schema.MessageInputAudio{
					MessagePartCommon: schema.MessagePartCommon{URL: stringPointer("https://example.test/voice.wav")},
				}},
			},
		},
		schema.AssistantMessage("", []schema.ToolCall{{
			ID: "previous-call", Type: "function",
			Function: schema.FunctionCall{Name: "lookup", Arguments: `{"id":6}`},
		}}),
		schema.ToolMessage("previous result", "previous-call", schema.WithToolName("lookup")),
	}
	toolInfo := &schema.ToolInfo{
		Name: "lookup", Desc: "Look up a record.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"id": {Type: schema.Integer, Required: true},
		}),
	}

	message, err := adapter.Generate(ctx, input,
		model.WithTemperature(0.7),
		model.WithTools([]*schema.ToolInfo{toolInfo}),
	)
	if err != nil {
		t.Fatal(err)
	}

	request := invoker.request
	if request.ModelName != "review-model" {
		t.Fatalf("model name = %q", request.ModelName)
	}
	if request.Temperature == nil || math.Abs(*request.Temperature-0.7) > 0.000001 {
		t.Fatalf("temperature = %v, want Eino option to override configured zero", request.Temperature)
	}
	if len(request.Messages) != 4 {
		t.Fatalf("message count = %d, want 4", len(request.Messages))
	}
	if request.Messages[0].Role != ModelRoleSystem || request.Messages[0].Content[0].Text != "system prompt" {
		t.Fatalf("system message was not preserved: %+v", request.Messages[0])
	}
	wantParts := []ModelPart{
		{Type: PartText, Text: "review"},
		{Type: PartImage, URL: "https://example.test/image.png"},
		{Type: PartVideo, URL: "https://example.test/lesson.mp4"},
		{Type: PartAudio, URL: "https://example.test/voice.wav"},
	}
	if !reflect.DeepEqual(request.Messages[1].Content, wantParts) {
		t.Fatalf("multimodal content = %+v, want %+v", request.Messages[1].Content, wantParts)
	}
	if len(request.Messages[2].ToolCalls) != 1 ||
		request.Messages[2].ToolCalls[0] != (ToolCall{ID: "previous-call", Name: "lookup", Arguments: `{"id":6}`}) {
		t.Fatalf("assistant tool call was not preserved: %+v", request.Messages[2].ToolCalls)
	}
	if request.Messages[3].ToolCallID != "previous-call" ||
		request.Messages[3].ToolName != "lookup" ||
		request.Messages[3].Content[0].Text != "previous result" {
		t.Fatalf("tool result relationship was not preserved: %+v", request.Messages[3])
	}
	if len(request.Tools) != 1 || request.Tools[0].Name != "lookup" ||
		len(request.Tools[0].Parameters) == 0 {
		t.Fatalf("tool schema was not adapted: %+v", request.Tools)
	}
	if message.Role != schema.Assistant || message.Content != "answer" ||
		message.ResponseMeta == nil || message.ResponseMeta.FinishReason != "tool_calls" ||
		len(message.ToolCalls) != 1 || message.ToolCalls[0].ID != "call-1" {
		t.Fatalf("model response was not mapped back to Eino: %+v", message)
	}
}

func TestEinoModelAdapterPreservesTemperatureNilAndZero(t *testing.T) {
	for _, test := range []struct {
		name        string
		temperature *float64
		wantNil     bool
		want        float64
	}{
		{name: "omitted", wantNil: true},
		{name: "explicit zero", temperature: floatPointer(0), want: 0},
		{name: "configured value", temperature: floatPointer(0.35), want: 0.35},
	} {
		t.Run(test.name, func(t *testing.T) {
			adapter := &einoModelAdapter{
				invoker:   &recordingModelInvoker{response: ModelResponse{Content: []ModelPart{{Type: PartText, Text: "ok"}}}},
				modelName: "model", temperature: test.temperature,
			}
			request, err := adapter.makeRequest(nil)
			if err != nil {
				t.Fatal(err)
			}
			if test.wantNil {
				if request.Temperature != nil {
					t.Fatalf("temperature = %v, want nil", *request.Temperature)
				}
				return
			}
			if request.Temperature == nil || math.Abs(*request.Temperature-test.want) > 0.000001 {
				t.Fatalf("temperature = %v, want %v", request.Temperature, test.want)
			}
		})
	}
}

func TestResponseToEinoMessagePreservesOptionalUsage(t *testing.T) {
	withUsage, err := responseToEinoMessage(ModelResponse{
		Content: []Part{{Type: PartText, Text: "hello"}},
		Usage:   &ModelUsage{InputTokens: 21, OutputTokens: 9},
	})
	if err != nil {
		t.Fatal(err)
	}
	if withUsage.ResponseMeta == nil || withUsage.ResponseMeta.Usage == nil ||
		withUsage.ResponseMeta.Usage.PromptTokens != 21 ||
		withUsage.ResponseMeta.Usage.CompletionTokens != 9 {
		t.Fatalf("usage lost in Eino response: %+v", withUsage.ResponseMeta)
	}
	withoutUsage, err := responseToEinoMessage(ModelResponse{
		Content: []Part{{Type: PartText, Text: "hello"}},
	})
	if err != nil || withoutUsage.ResponseMeta != nil {
		t.Fatalf("missing usage must remain absent: %+v, %v", withoutUsage, err)
	}
}

func TestEinoModelAdapterRejectsUnsupportedMediaAndPreservesInvokerErrors(t *testing.T) {
	ctx, _ := ginStdContext(t)
	adapter := &einoModelAdapter{
		invoker:   &recordingModelInvoker{},
		modelName: "model",
	}
	_, err := adapter.Generate(ctx, []*schema.Message{{
		Role: schema.User,
		UserInputMultiContent: []schema.MessageInputPart{{
			Type: schema.ChatMessagePartTypeImageURL,
			Image: &schema.MessageInputImage{MessagePartCommon: schema.MessagePartCommon{
				Base64Data: stringPointer("encoded"),
			}},
		}},
	}})
	if err == nil {
		t.Fatal("expected unsupported base64 payload to fail")
	}

	rootErr := errors.New("gateway failed")
	adapter.invoker = &recordingModelInvoker{err: rootErr}
	_, err = adapter.Generate(ctx, []*schema.Message{schema.UserMessage("hello")})
	if !errors.Is(err, rootErr) {
		t.Fatalf("invoker error chain = %v, want %v", err, rootErr)
	}

	adapter.invoker = &recordingModelInvoker{panicVal: "gateway panic"}
	_, err = adapter.Generate(ctx, []*schema.Message{schema.UserMessage("hello")})
	if err == nil {
		t.Fatal("expected ModelInvoker panic to become an error")
	}
}

func TestModelResponseMixedContentOrderIsPreserved(t *testing.T) {
	message, err := responseToEinoMessage(ModelResponse{Content: []ModelPart{
		{Type: PartText, Text: "before"},
		{Type: PartImage, URL: "https://example.test/image.png"},
		{Type: PartText, Text: "after"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(message.AssistantGenMultiContent) != 3 {
		t.Fatalf("content parts = %d, want 3", len(message.AssistantGenMultiContent))
	}
	if message.AssistantGenMultiContent[0].Text != "before" ||
		message.AssistantGenMultiContent[1].Image.URL == nil ||
		*message.AssistantGenMultiContent[1].Image.URL != "https://example.test/image.png" ||
		message.AssistantGenMultiContent[2].Text != "after" {
		t.Fatalf("mixed content order was not preserved: %+v", message.AssistantGenMultiContent)
	}
}

func floatPointer(value float64) *float64 { return &value }
