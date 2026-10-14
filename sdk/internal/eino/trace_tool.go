package eino

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	internaltrace "github.com/lubanAgent/luban/sdk/internal/trace"
)

// traceToolMiddleware observes the actual Eino tool execution, including
// native Skill loading and OutputTool. It never executes a tool a second time.
type traceToolMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
	agentName string
}

func (m *traceToolMiddleware) WrapInvokableToolCall(_ context.Context, endpoint adk.InvokableToolCallEndpoint, tc *adk.ToolContext) (adk.InvokableToolCallEndpoint, error) {
	if tc == nil {
		return endpoint, nil
	}
	return func(ctx context.Context, arguments string, options ...tool.Option) (out string, err error) {
		kind := "tool"
		if tc.Name == skillToolName {
			kind = "skill"
		}
		ctx, span := internaltrace.Start(ctx, kind, tc.Name, nil, m.agentName,
			map[string]any{"callId": tc.CallID, "arguments": arguments})
		defer func() {
			if recovered := recover(); recovered != nil {
				span.End(nil, "", fmt.Errorf("tool panicked: %v", recovered))
				panic(recovered)
			}
			span.End(out, "", err)
		}()
		return endpoint(ctx, arguments, options...)
	}, nil
}
