package eino

import (
	"context"
	"strings"

	"github.com/cloudwego/eino/adk"
	internaltrace "github.com/lubanAgent/luban/sdk/internal/trace"
)

const agentScopeGinKey = "__luban_agent_scope_v1"

// AgentScope identifies the Agent configured for the current Eino callback.
type AgentScope struct {
	AppKey        string
	FlowIndex     int
	Name          string
	ModelName     string
	Description   string
	IsSubAgent    bool
	SubAgentIndex int
}

type agentScopeMiddleware struct {
	adk.BaseChatModelAgentMiddleware
	scope AgentScope
}

func (m agentScopeMiddleware) BeforeAgent(ctx context.Context, runCtx *adk.ChatModelAgentContext) (context.Context, *adk.ChatModelAgentContext, error) {
	ginCtx, err := GinContextFrom(ctx)
	if err != nil {
		return nil, nil, err
	}
	scopedGinCtx := ginCtx.Copy()
	scopedGinCtx.Set(agentScopeGinKey, m.scope)
	scopedCtx, err := WithGinContext(ctx, scopedGinCtx)
	if err != nil {
		return nil, nil, err
	}
	parentName := ""
	if m.scope.IsSubAgent {
		if pos := strings.LastIndex(m.scope.Name, "_sub_"); pos >= 0 {
			parentName = m.scope.Name[:pos]
		}
	}
	scopedCtx = internaltrace.StartAgent(scopedCtx, m.scope.Name, parentName, map[string]any{
		"instruction": runCtx.Instruction,
		"modelName":   m.scope.ModelName,
	}, m.scope.Description)
	return scopedCtx, runCtx, nil
}
