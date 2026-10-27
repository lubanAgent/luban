package luban

import (
	"fmt"
	"time"

	internalconfig "github.com/lubanAgent/luban/sdk/internal/config"
)

const configCacheTTL = 10 * time.Second

type agentSnapshot struct {
	config AgentConfig
	skills map[string]Skill
}

type configCache struct {
	snapshots *internalconfig.Cache[agentSnapshot]
}

func newConfigCache() *configCache {
	return newConfigCacheWithClock(time.Now)
}

func newConfigCacheWithClock(clock func() time.Time) *configCache {
	return &configCache{
		snapshots: internalconfig.NewCache(configCacheTTL, cloneSnapshot, clock),
	}
}

func (c *configCache) get(key string, load func() (agentSnapshot, error)) (agentSnapshot, error) {
	return c.snapshots.Get(key, load)
}

func (rt *Runtime) loadSnapshot(appKey string) (agentSnapshot, error) {
	if rt == nil {
		return agentSnapshot{}, fmt.Errorf("luban: nil Runtime")
	}
	if !rt.registered.Load() {
		return agentSnapshot{}, fmt.Errorf("luban: Runtime is not registered")
	}
	if appKey == "" {
		return agentSnapshot{}, fmt.Errorf("luban: appKey must not be empty")
	}

	return rt.configCache.get(appKey, func() (agentSnapshot, error) {
		agent, exists := rt.config.Agents[appKey]
		if !exists {
			return agentSnapshot{}, fmt.Errorf("luban: appKey %q was not found", appKey)
		}
		return rt.validateAndSnapshot(appKey, agent)
	})
}

func (rt *Runtime) validateAndSnapshot(appKey string, agent AgentConfig) (agentSnapshot, error) {
	registry := rt.internalConfigRegistry(appKey)
	if rt.hasProjectModelRouting() {
		registry.Models = make(map[string]struct{})
	}
	for _, node := range agent.Flow {
		for _, name := range append([]string{node.ModelName}, childModelNames(node.SubAgents)...) {
			if name == "" {
				continue
			}
			if _, err := rt.modelFor(agent.ProjectKey, name); err != nil {
				continue
			}
			registry.Models[name] = struct{}{}
		}
	}
	validated, err := internalconfig.ValidateAndSnapshot(appKey, toInternalAgentConfig(agent), registry)
	if err != nil {
		return agentSnapshot{}, err
	}
	snapshot := agentSnapshot{
		config: fromInternalAgentConfig(validated.Config),
		skills: make(map[string]Skill, len(validated.Skills)),
	}
	for name, skill := range validated.Skills {
		snapshot.skills[name] = Skill{
			Name:        skill.Name,
			Description: skill.Description,
			Content:     skill.Content,
		}
	}
	return snapshot, nil
}

func childModelNames(children []SubAgentConfig) []string {
	names := make([]string, len(children))
	for i, child := range children {
		names[i] = child.ModelName
	}
	return names
}

func (rt *Runtime) internalConfigRegistry(appKey string) internalconfig.Registry {
	registry := internalconfig.Registry{
		Functions:     make(map[string]struct{}, len(rt.funcs)),
		Models:        make(map[string]struct{}, len(rt.models)),
		Tools:         make(map[string]struct{}, len(rt.tools)),
		Skills:        make(map[string]internalconfig.Skill, len(rt.skills)),
		SkillBackends: make(map[string]struct{}, len(rt.skillBackends)),
	}
	for name := range rt.functionsFor(appKey) {
		registry.Functions[name] = struct{}{}
	}
	for name := range rt.models {
		registry.Models[name] = struct{}{}
	}
	projectKey := rt.config.Agents[appKey].ProjectKey
	if projectKey == "" {
		if projects := rt.projectKeys(); len(projects) == 1 {
			projectKey = projects[0]
		}
	}
	for name := range rt.projectModels[projectKey] {
		registry.Models[name] = struct{}{}
	}
	for name := range rt.toolsFor(appKey) {
		registry.Tools[name] = struct{}{}
	}
	for name, skill := range rt.skillsFor(appKey) {
		registry.Skills[name] = internalconfig.Skill{
			Name:        skill.Name,
			Description: skill.Description,
			Content:     skill.Content,
		}
	}
	for name := range rt.skillBackends {
		registry.SkillBackends[name] = struct{}{}
	}
	return registry
}

func toInternalAgentConfig(agent AgentConfig) internalconfig.AgentConfig {
	result := internalconfig.AgentConfig{Description: agent.Description, ProjectKey: agent.ProjectKey, Flow: make([]internalconfig.NodeConfig, len(agent.Flow))}
	for i, node := range agent.Flow {
		converted := internalconfig.NodeConfig{
			Description:  node.Description,
			Func:         node.Func,
			ModelName:    node.ModelName,
			SystemPrompt: node.SystemPrompt,
			Temperature:  cloneFloat64(node.Temperature),
			Tools:        append([]string(nil), node.Tools...),
			Skills:       append([]string(nil), node.Skills...),
			MaxLoopCount: cloneInt(node.MaxLoopCount),
			SubAgents:    make([]internalconfig.SubAgentConfig, len(node.SubAgents)),
		}
		for j, child := range node.SubAgents {
			converted.SubAgents[j] = internalconfig.SubAgentConfig{
				Description:  child.Description,
				ModelName:    child.ModelName,
				SystemPrompt: child.SystemPrompt,
				Temperature:  cloneFloat64(child.Temperature),
				Tools:        append([]string(nil), child.Tools...),
				Skills:       append([]string(nil), child.Skills...),
				MaxLoopCount: cloneInt(child.MaxLoopCount),
				Unsupported:  child.unsupportedConfigFields(),
			}
		}
		result.Flow[i] = converted
	}
	return result
}

func fromInternalAgentConfig(agent internalconfig.AgentConfig) AgentConfig {
	result := AgentConfig{Description: agent.Description, ProjectKey: agent.ProjectKey, Flow: make([]NodeConfig, len(agent.Flow))}
	for i, node := range agent.Flow {
		converted := NodeConfig{
			Description:  node.Description,
			Func:         node.Func,
			ModelName:    node.ModelName,
			SystemPrompt: node.SystemPrompt,
			Temperature:  cloneFloat64(node.Temperature),
			Tools:        append([]string(nil), node.Tools...),
			Skills:       append([]string(nil), node.Skills...),
			MaxLoopCount: cloneInt(node.MaxLoopCount),
			SubAgents:    make([]SubAgentConfig, len(node.SubAgents)),
		}
		for j, child := range node.SubAgents {
			converted.SubAgents[j] = SubAgentConfig{
				Description:       child.Description,
				ModelName:         child.ModelName,
				SystemPrompt:      child.SystemPrompt,
				Temperature:       cloneFloat64(child.Temperature),
				Tools:             append([]string(nil), child.Tools...),
				Skills:            append([]string(nil), child.Skills...),
				MaxLoopCount:      cloneInt(child.MaxLoopCount),
				unsupportedFields: append([]string(nil), child.Unsupported...),
			}
		}
		result.Flow[i] = converted
	}
	return result
}

func cloneConfig(src Config) Config {
	dst := Config{Agents: make(map[string]AgentConfig, len(src.Agents))}
	for key, agent := range src.Agents {
		dst.Agents[key] = cloneAgentConfig(agent)
	}
	return dst
}

func cloneAgentConfig(src AgentConfig) AgentConfig {
	dst := AgentConfig{Description: src.Description, ProjectKey: src.ProjectKey, Flow: make([]NodeConfig, len(src.Flow))}
	for i, node := range src.Flow {
		dst.Flow[i] = cloneNodeConfig(node)
	}
	return dst
}

func cloneNodeConfig(src NodeConfig) NodeConfig {
	dst := src
	dst.Temperature = cloneFloat64(src.Temperature)
	dst.MaxLoopCount = cloneInt(src.MaxLoopCount)
	dst.Tools = append([]string(nil), src.Tools...)
	dst.Skills = append([]string(nil), src.Skills...)
	dst.SubAgents = make([]SubAgentConfig, len(src.SubAgents))
	for i, child := range src.SubAgents {
		dst.SubAgents[i] = cloneSubAgentConfig(child)
	}
	return dst
}

func cloneSubAgentConfig(src SubAgentConfig) SubAgentConfig {
	dst := src
	dst.Temperature = cloneFloat64(src.Temperature)
	dst.MaxLoopCount = cloneInt(src.MaxLoopCount)
	dst.Tools = append([]string(nil), src.Tools...)
	dst.Skills = append([]string(nil), src.Skills...)
	dst.unsupportedFields = append([]string(nil), src.unsupportedFields...)
	return dst
}

func cloneFloat64(src *float64) *float64 {
	if src == nil {
		return nil
	}
	value := *src
	return &value
}

func cloneInt(src *int) *int {
	if src == nil {
		return nil
	}
	value := *src
	return &value
}

func cloneSnapshot(src agentSnapshot) agentSnapshot {
	dst := agentSnapshot{
		config: cloneAgentConfig(src.config),
		skills: make(map[string]Skill, len(src.skills)),
	}
	for name, skill := range src.skills {
		dst.skills[name] = skill
	}
	return dst
}
