package eino

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/cloudwego/eino/adk"
	einoskill "github.com/cloudwego/eino/adk/middlewares/skill"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

const skillToolName = "skill"

type ChatModelAgent = adk.ChatModelAgent
type Agent = adk.Agent

type AgentConfig struct {
	Name         string
	Description  string
	ModelName    string
	SystemPrompt string
	Temperature  *float64
	MaxLoopCount *int
	Tools        []Tool
	Skills       []Skill
	OutputType   reflect.Type
	IsParent     bool
}

// NewAgent creates an Eino ChatModelAgent using only the internal DTOs and
// callbacks supplied by the public Luban runtime.
func NewAgent(ctx context.Context, config AgentConfig, invoker ModelInvoker, collector *OutputCollector) (*adk.ChatModelAgent, error) {
	if invoker == nil {
		return nil, fmt.Errorf("model invoker is nil")
	}
	if err := checkAgentToolNames(config); err != nil {
		return nil, err
	}

	agentTools := make([]tool.BaseTool, 0, len(config.Tools)+1)
	for _, businessTool := range config.Tools {
		adapted, err := newBusinessEinoTool(businessTool)
		if err != nil {
			return nil, fmt.Errorf("adapt Tool %q: %w", businessTool.Name, err)
		}
		agentTools = append(agentTools, adapted)
	}
	returnDirectly := make(map[string]bool)
	if config.OutputType != nil && !config.IsParent {
		outputTool, err := newOutputEinoTool(config.Name, config.OutputType, collector)
		if err != nil {
			return nil, err
		}
		agentTools = append(agentTools, outputTool)
		returnDirectly[RuntimeOutputToolName] = true
	}

	middlewares := make([]adk.ChatModelAgentMiddleware, 0, 1)
	if len(config.Skills) > 0 {
		backend, err := newMemorySkillBackend(config.Skills)
		if err != nil {
			return nil, err
		}
		middleware, err := einoskill.NewMiddleware(ctx, &einoskill.Config{
			Backend: backend,
			CustomSystemPrompt: func(context.Context, string) string {
				return "Use the skill tool to load an allowed Skill by name when its instructions are needed. Skill results contain only in-memory instructions; there is no filesystem or script access."
			},
			CustomToolDescription: func(_ context.Context, skills []einoskill.FrontMatter) string {
				var builder strings.Builder
				builder.WriteString("Load the full instructions of one listed Skill when needed.")
				for _, skill := range skills {
					builder.WriteString("\n- ")
					builder.WriteString(skill.Name)
					if skill.Description != "" {
						builder.WriteString(": ")
						builder.WriteString(skill.Description)
					}
				}
				return builder.String()
			},
			BuildContent: func(_ context.Context, skill einoskill.Skill, _ string) (string, error) {
				return skill.Content, nil
			},
		})
		if err != nil {
			return nil, fmt.Errorf("create Skill middleware: %w", err)
		}
		middlewares = append(middlewares, middleware)
	}

	maxIterations := 5
	if config.MaxLoopCount != nil {
		maxIterations = *config.MaxLoopCount
	}
	adapter := NewModelAdapter(config.ModelName, config.Temperature, invoker)
	return adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:          config.Name,
		Description:   config.Description,
		Instruction:   config.SystemPrompt,
		Model:         adapter,
		MaxIterations: maxIterations,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{Tools: agentTools},
			ReturnDirectly:  returnDirectly,
		},
		Handlers: middlewares,
	})
}

// AttachSubAgents creates a parent Agent with child transfers. Each child is
// wrapped to prevent transfer back to its parent.
func AttachSubAgents(ctx context.Context, parent *adk.ChatModelAgent, children []*adk.ChatModelAgent) (adk.Agent, error) {
	var result adk.Agent = parent
	if len(children) == 0 {
		return result, nil
	}
	adapted := make([]adk.Agent, 0, len(children))
	for _, child := range children {
		adapted = append(adapted, adk.AgentWithOptions(ctx, child, adk.WithDisallowTransferToParent()))
	}
	return adk.SetSubAgents(ctx, result, adapted)
}

type AgentResult struct {
	TransferredTo   string
	AssistantText   map[string]string
	OutputAgentName string
	OutputCallID    string
}

// RunAgent consumes the Eino event stream and records the specific output
// submission selected by Eino.
func RunAgent(
	ctx context.Context,
	agent adk.Agent,
	messages []*schema.Message,
	parentName string,
) (AgentResult, error) {
	result := AgentResult{AssistantText: make(map[string]string)}
	iterator := agent.Run(ctx, &adk.AgentInput{Messages: messages})
	var runErrors []error
	for {
		event, ok := iterator.Next()
		if !ok {
			break
		}
		if event == nil {
			continue
		}
		if event.Err != nil {
			runErrors = append(runErrors, fmt.Errorf("agent %q: %w", event.AgentName, event.Err))
		}
		eventAgentName := event.AgentName
		if eventAgentName == "" {
			eventAgentName = parentName
		}
		if event.Action != nil && event.Action.TransferToAgent != nil {
			result.TransferredTo = event.Action.TransferToAgent.DestAgentName
		}
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}
		message, _, err := adk.GetMessage(event)
		if err != nil {
			runErrors = append(runErrors, fmt.Errorf("agent %q event message: %w", event.AgentName, err))
			continue
		}
		if message == nil {
			continue
		}
		if message.Role == schema.Tool && message.ToolName == RuntimeOutputToolName {
			result.OutputAgentName = eventAgentName
			result.OutputCallID = message.ToolCallID
		}
		if message.Role == schema.Assistant {
			result.AssistantText[eventAgentName] = message.Content
		}
	}
	if len(runErrors) > 0 {
		return AgentResult{}, errors.Join(runErrors...)
	}
	if err := ctx.Err(); err != nil {
		return AgentResult{}, err
	}
	return result, nil
}

func checkAgentToolNames(config AgentConfig) error {
	names := make(map[string]string)
	add := func(name, kind string) error {
		if prior, exists := names[name]; exists {
			return fmt.Errorf("Agent %q has duplicate Eino tool name %q from %s and %s", config.Name, name, prior, kind)
		}
		names[name] = kind
		return nil
	}
	for _, businessTool := range config.Tools {
		if err := add(businessTool.Name, "business Tool"); err != nil {
			return err
		}
	}
	if len(config.Skills) > 0 {
		if err := add(skillToolName, "Skill loader"); err != nil {
			return err
		}
	}
	if config.OutputType != nil && !config.IsParent {
		if err := add(RuntimeOutputToolName, "OutputTool"); err != nil {
			return err
		}
	}
	if config.IsParent {
		if err := add(adk.TransferToAgentToolName, "sub-agent transfer"); err != nil {
			return err
		}
	}
	return nil
}

// MessagesToEino converts public request messages, first adapted to this
// package's private protocol, to Eino messages.
func MessagesToEino(messages []Message) ([]*schema.Message, error) {
	result := make([]*schema.Message, 0, len(messages))
	for index, message := range messages {
		converted, err := messageToEino(message)
		if err != nil {
			return nil, fmt.Errorf("message[%d]: %w", index, err)
		}
		result = append(result, converted)
	}
	return result, nil
}

func messageToEino(message Message) (*schema.Message, error) {
	switch message.Role {
	case "user":
		converted := &schema.Message{Role: schema.User}
		for index, part := range message.Content {
			input, err := inputPartToEino(part)
			if err != nil {
				return nil, fmt.Errorf("content[%d]: %w", index, err)
			}
			converted.UserInputMultiContent = append(converted.UserInputMultiContent, input)
		}
		return converted, nil
	case "assistant":
		converted := &schema.Message{Role: schema.Assistant}
		allText := true
		for _, part := range message.Content {
			if part.Type != PartText {
				allText = false
				break
			}
		}
		if allText {
			var builder strings.Builder
			for _, part := range message.Content {
				builder.WriteString(part.Text)
			}
			converted.Content = builder.String()
			return converted, nil
		}
		for index, part := range message.Content {
			output, err := outputPartToEino(part)
			if err != nil {
				return nil, fmt.Errorf("content[%d]: %w", index, err)
			}
			converted.AssistantGenMultiContent = append(converted.AssistantGenMultiContent, output)
		}
		return converted, nil
	default:
		return nil, fmt.Errorf("unsupported Request role %q", message.Role)
	}
}

func inputPartToEino(part Part) (schema.MessageInputPart, error) {
	switch part.Type {
	case PartText:
		return schema.MessageInputPart{Type: schema.ChatMessagePartTypeText, Text: part.Text}, nil
	case PartImage:
		if part.URL == "" {
			return schema.MessageInputPart{}, fmt.Errorf("image URL is empty")
		}
		return schema.MessageInputPart{Type: schema.ChatMessagePartTypeImageURL, Image: &schema.MessageInputImage{
			MessagePartCommon: schema.MessagePartCommon{URL: stringPointer(part.URL)},
		}}, nil
	case PartVideo:
		if part.URL == "" {
			return schema.MessageInputPart{}, fmt.Errorf("video URL is empty")
		}
		return schema.MessageInputPart{Type: schema.ChatMessagePartTypeVideoURL, Video: &schema.MessageInputVideo{
			MessagePartCommon: schema.MessagePartCommon{URL: stringPointer(part.URL)},
		}}, nil
	case PartAudio:
		if part.URL == "" {
			return schema.MessageInputPart{}, fmt.Errorf("audio URL is empty")
		}
		return schema.MessageInputPart{Type: schema.ChatMessagePartTypeAudioURL, Audio: &schema.MessageInputAudio{
			MessagePartCommon: schema.MessagePartCommon{URL: stringPointer(part.URL)},
		}}, nil
	default:
		return schema.MessageInputPart{}, fmt.Errorf("unsupported part type %q", part.Type)
	}
}

func outputPartToEino(part Part) (schema.MessageOutputPart, error) {
	switch part.Type {
	case PartText:
		return schema.MessageOutputPart{Type: schema.ChatMessagePartTypeText, Text: part.Text}, nil
	case PartImage:
		if part.URL == "" {
			return schema.MessageOutputPart{}, fmt.Errorf("image URL is empty")
		}
		return schema.MessageOutputPart{Type: schema.ChatMessagePartTypeImageURL, Image: &schema.MessageOutputImage{
			MessagePartCommon: schema.MessagePartCommon{URL: stringPointer(part.URL)},
		}}, nil
	case PartVideo:
		if part.URL == "" {
			return schema.MessageOutputPart{}, fmt.Errorf("video URL is empty")
		}
		return schema.MessageOutputPart{Type: schema.ChatMessagePartTypeVideoURL, Video: &schema.MessageOutputVideo{
			MessagePartCommon: schema.MessagePartCommon{URL: stringPointer(part.URL)},
		}}, nil
	case PartAudio:
		if part.URL == "" {
			return schema.MessageOutputPart{}, fmt.Errorf("audio URL is empty")
		}
		return schema.MessageOutputPart{Type: schema.ChatMessagePartTypeAudioURL, Audio: &schema.MessageOutputAudio{
			MessagePartCommon: schema.MessagePartCommon{URL: stringPointer(part.URL)},
		}}, nil
	default:
		return schema.MessageOutputPart{}, fmt.Errorf("unsupported part type %q", part.Type)
	}
}
