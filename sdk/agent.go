package luban

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"

	"github.com/gin-gonic/gin"
	internaleino "github.com/lubanAgent/luban/sdk/internal/eino"
	internaltrace "github.com/lubanAgent/luban/sdk/internal/trace"
)

const runtimeOutputToolName = internaleino.RuntimeOutputToolName

type publicSkillBackend struct {
	backend SkillBackend
}

func (b publicSkillBackend) List(ctx context.Context) ([]internaleino.SkillInfo, error) {
	ginCtx, err := internaleino.GinContextFrom(ctx)
	if err != nil {
		return nil, err
	}
	skills, err := b.backend.List(ginCtx)
	if err != nil {
		return nil, err
	}
	result := make([]internaleino.SkillInfo, 0, len(skills))
	for _, skill := range skills {
		result = append(result, internaleino.SkillInfo{
			Name:        skill.Name,
			Description: skill.Description,
		})
	}
	return result, nil
}

func (b publicSkillBackend) Get(ctx context.Context, name string) (internaleino.Skill, error) {
	ginCtx, err := internaleino.GinContextFrom(ctx)
	if err != nil {
		return internaleino.Skill{}, err
	}
	skill, err := b.backend.Get(ginCtx, name)
	if err != nil {
		return internaleino.Skill{}, err
	}
	return internaleino.Skill{
		Name:        skill.Name,
		Description: skill.Description,
		Content:     skill.Content,
	}, nil
}

func (rt *Runtime) runAgentNode(
	ctx context.Context,
	ginCtx *gin.Context,
	appKey string,
	flowIndex int,
	node NodeConfig,
	incoming nodeValue,
) (nodeValue, error) {
	if !incoming.HasValue {
		return nodeValue{}, fmt.Errorf("luban: appKey %q flow[%d] Agent has no Request or Batch input", appKey, flowIndex)
	}
	switch input := incoming.Value.(type) {
	case Request:
		return rt.runRequestAgent(ctx, ginCtx, appKey, flowIndex, node, input)
	case Batch:
		return rt.runBatchAgent(ctx, ginCtx, appKey, flowIndex, node, input)
	default:
		return nodeValue{}, fmt.Errorf("luban: appKey %q flow[%d] Agent input has type %T, want luban.Request or luban.Batch", appKey, flowIndex, incoming.Value)
	}
}

func (rt *Runtime) runRequestAgent(
	ctx context.Context,
	ginCtx *gin.Context,
	appKey string,
	flowIndex int,
	node NodeConfig,
	request Request,
) (nodeValue, error) {
	messages := make([]internaleino.Message, 0, len(request.Messages))
	for _, message := range request.Messages {
		converted := internaleino.Message{Role: string(message.Role)}
		for _, part := range message.Content {
			converted.Content = append(converted.Content, internaleino.Part{
				Type: internaleino.PartType(part.Type),
				Text: part.Text,
				URL:  part.URL,
			})
		}
		messages = append(messages, converted)
	}
	einoMessages, err := internaleino.MessagesToEino(messages)
	if err != nil {
		return nodeValue{}, fmt.Errorf("luban: appKey %q flow[%d] Request: %w", appKey, flowIndex, err)
	}

	parentName := fmt.Sprintf("luban_flow_%d", flowIndex)
	collector := internaleino.NewOutputCollector()
	parentConfig, parentInvoker, err := rt.makeEinoAgentConfig(
		parentName, node.Description, node.ModelName, node.SystemPrompt,
		node.Temperature, node.MaxLoopCount, node.Tools, node.Skills,
		request.Output, internaleino.AgentScope{
			AppKey: appKey, FlowIndex: flowIndex, Name: parentName,
			ModelName: node.ModelName, Description: node.Description,
		}, len(node.SubAgents) > 0,
	)
	if err != nil {
		return nodeValue{}, fmt.Errorf("luban: appKey %q flow[%d] Agent setup: %w", appKey, flowIndex, err)
	}
	parentAgent, err := internaleino.NewAgent(ctx, parentConfig, parentInvoker, collector)
	if err != nil {
		return nodeValue{}, fmt.Errorf("luban: appKey %q flow[%d] Agent setup: %w", appKey, flowIndex, err)
	}

	children := make([]*internaleino.ChatModelAgent, 0, len(node.SubAgents))
	childNames := make(map[string]int, len(node.SubAgents))
	for childIndex, child := range node.SubAgents {
		childName := fmt.Sprintf("luban_flow_%d_sub_%d", flowIndex, childIndex)
		childNames[childName] = childIndex
		childConfig, childInvoker, err := rt.makeEinoAgentConfig(
			childName, child.Description, child.ModelName, child.SystemPrompt,
			child.Temperature, child.MaxLoopCount, child.Tools, child.Skills,
			request.Output, internaleino.AgentScope{
				AppKey: appKey, FlowIndex: flowIndex, Name: childName,
				ModelName: child.ModelName, Description: child.Description,
				IsSubAgent: true, SubAgentIndex: childIndex,
			}, false,
		)
		if err != nil {
			return nodeValue{}, fmt.Errorf("luban: appKey %q flow[%d] subAgents[%d] setup: %w", appKey, flowIndex, childIndex, err)
		}
		childAgent, err := internaleino.NewAgent(ctx, childConfig, childInvoker, collector)
		if err != nil {
			return nodeValue{}, fmt.Errorf("luban: appKey %q flow[%d] subAgents[%d] setup: %w", appKey, flowIndex, childIndex, err)
		}
		children = append(children, childAgent)
	}

	var agent internaleino.Agent
	agent, err = internaleino.AttachSubAgents(ctx, parentAgent, children)
	if err != nil {
		return nodeValue{}, fmt.Errorf("luban: appKey %q flow[%d] attach sub-agents: %w", appKey, flowIndex, err)
	}

	runResult, err := internaleino.RunAgent(ctx, agent, einoMessages, parentName)
	if err != nil {
		return nodeValue{}, fmt.Errorf("luban: appKey %q flow[%d] Agent: %w", appKey, flowIndex, err)
	}

	if len(children) > 0 {
		if runResult.TransferredTo == "" {
			// The parent only chooses a child. Its own text is not a business result.
			return nodeValue{Value: nil, HasValue: true}, nil
		}
		selectedIndex, exists := childNames[runResult.TransferredTo]
		if !exists {
			return nodeValue{}, fmt.Errorf("luban: appKey %q flow[%d] transferred to unknown child %q", appKey, flowIndex, runResult.TransferredTo)
		}
		ginCtx.Set(selectedSubAgentGinKey, SubAgentSelection{
			Index: selectedIndex, Name: runResult.TransferredTo,
			Description: node.SubAgents[selectedIndex].Description,
		})
		if request.Output != nil {
			value, exists := collector.Load(runResult.OutputAgentName, runResult.OutputCallID)
			if runResult.OutputAgentName != runResult.TransferredTo || !exists {
				return nodeValue{}, fmt.Errorf("luban: appKey %q flow[%d] child Agent %q did not submit Output", appKey, flowIndex, runResult.TransferredTo)
			}
			if err := outputTypeMatches(value, request.Output.goType); err != nil {
				return nodeValue{}, fmt.Errorf("luban: appKey %q flow[%d] child Agent %q Output: %w", appKey, flowIndex, runResult.TransferredTo, err)
			}
			return nodeValue{Value: value, HasValue: true}, nil
		}
		message, exists := runResult.AssistantText[runResult.TransferredTo]
		if !exists {
			return nodeValue{}, fmt.Errorf("luban: appKey %q flow[%d] child Agent %q returned no message", appKey, flowIndex, runResult.TransferredTo)
		}
		return nodeValue{Value: message, HasValue: true}, nil
	}

	if request.Output != nil {
		value, exists := collector.Load(runResult.OutputAgentName, runResult.OutputCallID)
		if runResult.OutputAgentName != parentName || !exists {
			return nodeValue{}, fmt.Errorf("luban: appKey %q flow[%d] Agent declared Output but did not submit it", appKey, flowIndex)
		}
		if err := outputTypeMatches(value, request.Output.goType); err != nil {
			return nodeValue{}, fmt.Errorf("luban: appKey %q flow[%d] Agent Output: %w", appKey, flowIndex, err)
		}
		return nodeValue{Value: value, HasValue: true}, nil
	}
	message, exists := runResult.AssistantText[parentName]
	if !exists {
		return nodeValue{}, fmt.Errorf("luban: appKey %q flow[%d] Agent returned no final message", appKey, flowIndex)
	}
	return nodeValue{Value: message, HasValue: true}, nil
}

func (rt *Runtime) makeEinoAgentConfig(
	name, description, modelName, systemPrompt string,
	temperature *float64,
	maxLoopCount *int,
	toolNames, skillNames []string,
	output *OutputSpec,
	scope internaleino.AgentScope,
	isParent bool,
) (internaleino.AgentConfig, internaleino.ModelInvoker, error) {
	invoker, exists := rt.models[modelName]
	if !exists || isNilInterface(invoker) {
		return internaleino.AgentConfig{}, nil, fmt.Errorf("model %q was not registered", modelName)
	}

	agentTools := make([]internaleino.Tool, 0, len(toolNames))
	for _, toolName := range toolNames {
		registeredTool, exists := rt.tools[toolName]
		if !exists {
			return internaleino.AgentConfig{}, nil, fmt.Errorf("Tool %q was not registered", toolName)
		}
		input, err := readToolInput(registeredTool.value)
		if err != nil {
			return internaleino.AgentConfig{}, nil, fmt.Errorf("Tool %q Input: %w", toolName, err)
		}
		businessTool := registeredTool.value
		agentTools = append(agentTools, internaleino.Tool{
			Name:        registeredTool.name,
			Description: registeredTool.description,
			Input:       input,
			Invoke: func(ginCtx *gin.Context, input any) (any, error) {
				return businessTool.Invoke(ginCtx, input)
			},
		})
	}

	agentSkills := make([]internaleino.Skill, 0, len(skillNames))
	agentSkillBackends := make([]internaleino.SkillBackendSource, 0, len(skillNames))
	for _, skillName := range skillNames {
		if skill, exists := rt.skills[skillName]; exists {
			agentSkills = append(agentSkills, internaleino.Skill{
				Name:        skill.Name,
				Description: skill.Description,
				Content:     skill.Content,
			})
			continue
		}
		if backend, exists := rt.skillBackends[skillName]; exists {
			agentSkillBackends = append(agentSkillBackends, internaleino.SkillBackendSource{
				Name:    skillName,
				Backend: publicSkillBackend{backend: backend},
			})
			continue
		}
		return internaleino.AgentConfig{}, nil, fmt.Errorf("Skill or SkillBackend %q was not registered", skillName)
	}

	var outputType reflect.Type
	if output != nil {
		if !output.valid() {
			return internaleino.AgentConfig{}, nil, fmt.Errorf("OutputSpec is invalid; use Output[T]()")
		}
		outputType = output.goType
	}

	config := internaleino.AgentConfig{
		Name:          name,
		Description:   description,
		ModelName:     modelName,
		SystemPrompt:  systemPrompt,
		Temperature:   cloneFloat64(temperature),
		MaxLoopCount:  cloneInt(maxLoopCount),
		Tools:         agentTools,
		Skills:        agentSkills,
		SkillBackends: agentSkillBackends,
		Scope:         scope,
		OutputType:    outputType,
		IsParent:      isParent,
	}
	return config, publicModelInvoker{invoker: invoker}, nil
}

func (rt *Runtime) runBatchAgent(
	ctx context.Context,
	ginCtx *gin.Context,
	appKey string,
	flowIndex int,
	node NodeConfig,
	batch Batch,
) (nodeValue, error) {
	if len(batch.Items) == 0 {
		return nodeValue{}, ErrEmptyBatch
	}
	if batch.Output != nil && !batch.Output.valid() {
		return nodeValue{}, fmt.Errorf("luban: appKey %q flow[%d] Batch OutputSpec is invalid; use Output[T]()", appKey, flowIndex)
	}
	concurrency := batch.Concurrency
	if concurrency < 0 {
		return nodeValue{}, fmt.Errorf("luban: appKey %q flow[%d] Batch concurrency must not be negative", appKey, flowIndex)
	}
	if concurrency == 0 {
		concurrency = 10
	}
	if concurrency > len(batch.Items) {
		concurrency = len(batch.Items)
	}

	results := make([]batchItemValue, len(batch.Items))
	baseGinCtx := ginCtx.Copy()
	batchData := dataFromGin(ginCtx).readonlySnapshot()
	baseGinCtx.Set(sharedDataGinKey, batchData)
	defer batchData.close()
	jobs := make(chan int)
	var workers sync.WaitGroup
	workers.Add(concurrency)
	for worker := 0; worker < concurrency; worker++ {
		go func() {
			defer workers.Done()
			for itemIndex := range jobs {
				results[itemIndex] = rt.runBatchItem(ctx, baseGinCtx, appKey, flowIndex, node, batch, itemIndex)
			}
		}()
	}
	for itemIndex := range batch.Items {
		jobs <- itemIndex
	}
	close(jobs)
	workers.Wait()

	collector := batch.Output
	if collector == nil {
		collector = Output[string]()
	}
	result, err := collector.makeBatchResult(results)
	if err != nil {
		return nodeValue{}, fmt.Errorf("luban: appKey %q flow[%d] Batch result: %w", appKey, flowIndex, err)
	}
	return nodeValue{Value: result, HasValue: true}, nil
}

func (rt *Runtime) runBatchItem(
	parentCtx context.Context,
	baseGinCtx *gin.Context,
	appKey string,
	flowIndex int,
	node NodeConfig,
	batch Batch,
	itemIndex int,
) (result batchItemValue) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = batchItemValue{err: fmt.Errorf("luban: appKey %q flow[%d] Batch item[%d] panicked: %v", appKey, flowIndex, itemIndex, recovered)}
		}
	}()
	if err := parentCtx.Err(); err != nil {
		return batchItemValue{err: err}
	}
	itemGinCtx := baseGinCtx.Copy()
	// Keep this item's isolated Gin context while retaining the parent Run's
	// trace recorder and span, including concurrent sibling Batch items.
	itemCtx, err := internaleino.WithGinContext(parentCtx, itemGinCtx)
	if err != nil {
		return batchItemValue{err: err}
	}
	itemCtx, itemSpan := internaltrace.Start(itemCtx, "batchItem", fmt.Sprintf("item[%d]", itemIndex), &flowIndex, "", batch.Items[itemIndex])
	defer func() {
		if recovered := recover(); recovered != nil {
			itemSpan.End(nil, "", fmt.Errorf("Batch item panicked: %v", recovered))
			panic(recovered)
		}
		if result.err != nil {
			itemSpan.End(nil, "", result.err)
		} else {
			itemSpan.End(result.value, "", nil)
		}
	}()
	itemRequest := Request{Messages: batch.Items[itemIndex].Messages, Output: batch.Output}
	itemOutput, err := rt.runRequestAgent(
		itemCtx,
		itemGinCtx,
		appKey,
		flowIndex,
		node,
		itemRequest,
	)
	if cancelErr := itemCtx.Err(); cancelErr != nil {
		if err != nil {
			err = errors.Join(err, cancelErr)
		} else {
			err = cancelErr
		}
	}
	if err != nil {
		return batchItemValue{err: err}
	}
	if !itemOutput.HasValue {
		return batchItemValue{err: fmt.Errorf("luban: appKey %q flow[%d] Batch item[%d] returned no value", appKey, flowIndex, itemIndex)}
	}
	return batchItemValue{value: itemOutput.Value}
}

func outputTypeMatches(value any, expected reflect.Type) error {
	if expected == nil {
		return fmt.Errorf("declared output type is nil")
	}
	if value == nil {
		if isNilable(expected) {
			return nil
		}
		return fmt.Errorf("Output is nil, want %v", expected)
	}
	actual := reflect.TypeOf(value)
	if !actual.AssignableTo(expected) {
		return fmt.Errorf("Output type %v is not assignable to %v", actual, expected)
	}
	return nil
}
