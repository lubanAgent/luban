package eino

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	"github.com/bytedance/sonic"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"
	"github.com/gin-gonic/gin"
)

const RuntimeOutputToolName = "__runtime_output"

type Tool struct {
	Name        string
	Description string
	Input       any
	Invoke      func(*gin.Context, any) (any, error)
}

type einoToolAdapter struct {
	info      *schema.ToolInfo
	inputType reflect.Type
	invoke    func(ctx context.Context, ginCtx *gin.Context, input any) (any, error)
}

var _ tool.InvokableTool = (*einoToolAdapter)(nil)

func (t *einoToolAdapter) Info(context.Context) (*schema.ToolInfo, error) {
	if t == nil || t.info == nil {
		return nil, fmt.Errorf("luban: Tool schema is not initialized")
	}
	return t.info, nil
}

func (t *einoToolAdapter) InvokableRun(ctx context.Context, arguments string, _ ...tool.Option) (output string, err error) {
	if t == nil || t.invoke == nil {
		return "", fmt.Errorf("luban: Tool is not initialized")
	}
	toolName := ""
	if t.info != nil {
		toolName = t.info.Name
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			output = ""
			if panicErr, ok := recovered.(error); ok {
				err = fmt.Errorf("luban: Tool %q panicked: %w", toolName, panicErr)
			} else {
				err = fmt.Errorf("luban: Tool %q panicked: %v", toolName, recovered)
			}
		}
	}()

	input, err := decodeToolInput(t.inputType, arguments)
	if err != nil {
		return "", fmt.Errorf("luban: Tool %q arguments: %w", toolName, err)
	}
	ginCtx, err := GinContextFrom(ctx)
	if err != nil {
		return "", err
	}

	result, err := t.invoke(ctx, ginCtx.Copy(), input)
	if err != nil {
		return "", fmt.Errorf("luban: Tool %q: %w", toolName, err)
	}
	return marshalToolOutput(result)
}

type outputKey struct {
	agentName string
	callID    string
}

// OutputCollector isolates structured results by Agent and Eino ToolCallID.
type OutputCollector struct {
	mu     sync.Mutex
	values map[outputKey]any
}

func NewOutputCollector() *OutputCollector {
	return &OutputCollector{values: make(map[outputKey]any)}
}

func (c *OutputCollector) store(agentName, callID string, value any) error {
	if callID == "" {
		return fmt.Errorf("luban: OutputTool call has no ToolCallID")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values[outputKey{agentName: agentName, callID: callID}] = value
	return nil
}

// Load returns a submitted value for the exact Agent event and ToolCallID.
func (c *OutputCollector) Load(agentName, callID string) (any, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	value, exists := c.values[outputKey{agentName: agentName, callID: callID}]
	return value, exists
}

func newBusinessEinoTool(businessTool Tool) (*einoToolAdapter, error) {
	if businessTool.Invoke == nil {
		return nil, fmt.Errorf("Tool %q Invoke is nil", businessTool.Name)
	}
	if businessTool.Name == "" {
		return nil, fmt.Errorf("Tool name is empty")
	}
	if businessTool.Description == "" {
		return nil, fmt.Errorf("Tool %q description is empty", businessTool.Name)
	}
	inputType := reflect.TypeOf(businessTool.Input)
	if inputType == nil {
		return nil, fmt.Errorf("Tool %q Input must return a struct or pointer to struct", businessTool.Name)
	}
	schemaType := inputType
	if schemaType.Kind() == reflect.Pointer {
		schemaType = schemaType.Elem()
	}
	if schemaType.Kind() != reflect.Struct {
		return nil, fmt.Errorf("Tool %q Input type %v must be a struct or pointer to struct", businessTool.Name, inputType)
	}
	info, err := toolInfoFor(businessTool.Name, businessTool.Description, inputType)
	if err != nil {
		return nil, fmt.Errorf("Tool %q schema: %w", businessTool.Name, err)
	}
	return &einoToolAdapter{
		info:      info,
		inputType: inputType,
		invoke: func(_ context.Context, ginCtx *gin.Context, input any) (any, error) {
			return businessTool.Invoke(ginCtx, input)
		},
	}, nil
}

func newOutputEinoTool(agentName string, outputType reflect.Type, collector *OutputCollector) (*einoToolAdapter, error) {
	if outputType == nil {
		return nil, fmt.Errorf("luban: Output type is nil")
	}
	info, err := toolInfoFor(RuntimeOutputToolName, "When you have completed the task and no longer need another tool or skill, submit the final result using this tool.", outputType)
	if err != nil {
		return nil, fmt.Errorf("luban: OutputTool schema: %w", err)
	}
	return &einoToolAdapter{
		info:      info,
		inputType: outputType,
		invoke: func(ctx context.Context, _ *gin.Context, value any) (any, error) {
			callID := compose.GetToolCallID(ctx)
			if err := collector.store(agentName, callID, value); err != nil {
				return nil, err
			}
			return value, nil
		},
	}, nil
}

func toolInfoFor(name, description string, inputType reflect.Type) (info *schema.ToolInfo, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			info = nil
			err = fmt.Errorf("schema generation panicked: %v", recovered)
		}
	}()
	if inputType == nil {
		return nil, fmt.Errorf("input type is nil")
	}
	reflector := &jsonschema.Reflector{
		Anonymous:      true,
		DoNotReference: true,
	}
	jsonSchema := reflector.ReflectFromType(inputType)
	jsonSchema.Version = ""
	return &schema.ToolInfo{
		Name:        name,
		Desc:        description,
		ParamsOneOf: schema.NewParamsOneOfByJSONSchema(jsonSchema),
	}, nil
}

func decodeToolInput(inputType reflect.Type, arguments string) (any, error) {
	if inputType == nil {
		return nil, fmt.Errorf("input type is nil")
	}
	pointerType := inputType.Kind() == reflect.Pointer
	targetType := inputType
	if pointerType {
		targetType = inputType.Elem()
	}
	target := reflect.New(targetType)
	if err := sonic.UnmarshalString(arguments, target.Interface()); err != nil {
		return nil, err
	}
	if pointerType {
		return target.Interface(), nil
	}
	return target.Elem().Interface(), nil
}

func marshalToolOutput(value any) (string, error) {
	if text, ok := value.(string); ok {
		return text, nil
	}
	return sonic.MarshalString(value)
}
