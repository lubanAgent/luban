package luban

import (
	"fmt"
	"github.com/gin-gonic/gin"
	internaleino "github.com/lubanAgent/luban/sdk/internal/eino"
)

// LookupSkill reads a static Skill from this Run's startup snapshot. A business
// preparation node may include required knowledge in its Request, using the
// same source as the model's on-demand Skill tool. Dynamic backends are not run.
func LookupSkill(ctx *gin.Context, name string) (Skill, error) {
	data := dataFromGin(ctx)
	if data == nil {
		return Skill{}, fmt.Errorf("luban: LookupSkill requires an active Run")
	}
	data.mu.RLock()
	defer data.mu.RUnlock()
	if data.closed {
		return Skill{}, fmt.Errorf("luban: LookupSkill called after Run completed")
	}
	skill, exists := data.skills[name]
	if !exists {
		return Skill{}, fmt.Errorf("luban: static Skill %q was not loaded for this Agent", name)
	}
	return skill, nil
}

// Skill is a reusable instruction document available to an Agent.
type Skill struct {
	Name        string
	Description string
	Content     string
}

// SkillInfo describes a Skill that an Agent may load.
type SkillInfo struct {
	Name        string
	Description string
}

// SkillBackend provides Skills dynamically for one Run.
//
// List is called when Eino builds the Skill tool description. Get is called
// only when the model chooses to load a listed Skill. Implementations should
// use the supplied Gin context for request-scoped data and be safe for
// concurrent calls across Runtime.Run invocations.
type SkillBackend interface {
	List(ctx *gin.Context) ([]SkillInfo, error)
	Get(ctx *gin.Context, name string) (Skill, error)
}

// AgentScope identifies the Agent currently loading a Skill or invoking a
// Tool. SubAgentIndex is the zero-based position in the configured subAgents
// array and is meaningful only when IsSubAgent is true.
type AgentScope struct {
	AppKey        string
	FlowIndex     int
	Name          string
	Description   string
	IsSubAgent    bool
	SubAgentIndex int
}

const agentScopeGinKey = "__luban_agent_scope_v1"

// CurrentAgent returns the Agent scope for the current Eino Agent callback.
func CurrentAgent(ctx *gin.Context) (AgentScope, bool) {
	if ctx == nil {
		return AgentScope{}, false
	}
	value, exists := ctx.Get(agentScopeGinKey)
	if !exists {
		return AgentScope{}, false
	}
	scope, ok := value.(internaleino.AgentScope)
	if !ok {
		return AgentScope{}, false
	}
	return AgentScope{
		AppKey: scope.AppKey, FlowIndex: scope.FlowIndex, Name: scope.Name,
		Description: scope.Description, IsSubAgent: scope.IsSubAgent,
		SubAgentIndex: scope.SubAgentIndex,
	}, true
}

// SelectedSubAgent returns the subAgent selected by the parent Agent during
// the current Run. The index matches the configured subAgents array.
func SelectedSubAgent(ctx *gin.Context) (SubAgentSelection, bool) {
	if ctx == nil {
		return SubAgentSelection{}, false
	}
	value, exists := ctx.Get(selectedSubAgentGinKey)
	if !exists {
		return SubAgentSelection{}, false
	}
	selection, ok := value.(SubAgentSelection)
	return selection, ok
}

// SubAgentSelection identifies the configured child selected during a Run.
type SubAgentSelection struct {
	Index       int
	Name        string
	Description string
}

const (
	selectedSubAgentGinKey = "__luban_selected_sub_agent_v1"
)
