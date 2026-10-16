package eino

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	internaltrace "github.com/lubanAgent/luban/sdk/internal/trace"
)

// traceToolMiddleware observes actual Eino tool execution and enforces its
// callback boundary, including native Skill loading and OutputTool.
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
				out = ""
				if panicErr, ok := recovered.(error); ok {
					err = fmt.Errorf("luban: Tool %q panicked: %w", tc.Name, panicErr)
				} else {
					err = fmt.Errorf("luban: Tool %q panicked: %v", tc.Name, recovered)
				}
			}
			span.End(out, "", err)
		}()
		ginCtx, err := GinContextFrom(ctx)
		if err != nil {
			return "", err
		}
		toolGinCtx := ginCtx.Copy()
		guard := newToolCallGuard(toolGinCtx, tc.Name)
		defer guard.active.Store(false)
		ctx, err = WithGinContext(ctx, toolGinCtx)
		if err != nil {
			return "", err
		}
		out, err = endpoint(ctx, arguments, options...)
		if guard.rejected.Load() {
			out = ""
			if !errors.Is(err, guard.err) {
				err = errors.Join(err, guard.err)
			}
		}
		return out, err
	}, nil
}
