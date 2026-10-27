package config

import (
	"fmt"
	"math"
)

type Config struct {
	Agents map[string]AgentConfig
}

type AgentConfig struct {
	Description string
	ProjectKey  string
	Flow        []NodeConfig
}

type NodeConfig struct {
	Description  string
	Func         string
	ModelName    string
	SystemPrompt string
	Temperature  *float64
	Tools        []string
	Skills       []string
	SubAgents    []SubAgentConfig
	MaxLoopCount *int
}

type SubAgentConfig struct {
	Description  string
	ModelName    string
	SystemPrompt string
	Temperature  *float64
	Tools        []string
	Skills       []string
	MaxLoopCount *int
	Unsupported  []string
}

type Skill struct {
	Name        string
	Description string
	Content     string
}

type Registry struct {
	Functions     map[string]struct{}
	Models        map[string]struct{}
	Tools         map[string]struct{}
	Skills        map[string]Skill
	SkillBackends map[string]struct{}
}

type Snapshot struct {
	Config AgentConfig
	Skills map[string]Skill
}

// ValidateAndSnapshot checks a single Agent configuration against the
// registered business capabilities, then returns an isolated snapshot.
func ValidateAndSnapshot(appKey string, agent AgentConfig, registry Registry) (Snapshot, error) {
	if len(agent.Flow) == 0 {
		return Snapshot{}, fmt.Errorf("luban: appKey %q has an empty flow", appKey)
	}
	snapshot := Snapshot{
		Config: cloneAgent(agent),
		Skills: make(map[string]Skill),
	}
	for index, node := range agent.Flow {
		location := fmt.Sprintf("flow[%d]", index)
		if node.Func != "" && node.ModelName != "" {
			return Snapshot{}, fmt.Errorf("luban: appKey %q %s mixes func and modelName", appKey, location)
		}
		if node.Func != "" {
			if node.SystemPrompt != "" || node.Temperature != nil || len(node.Tools) > 0 ||
				len(node.Skills) > 0 || len(node.SubAgents) > 0 || node.MaxLoopCount != nil {
				return Snapshot{}, fmt.Errorf("luban: appKey %q %s business node has Agent-only fields", appKey, location)
			}
			if _, exists := registry.Functions[node.Func]; !exists {
				return Snapshot{}, fmt.Errorf("luban: appKey %q %s function %q was not registered", appKey, location, node.Func)
			}
			continue
		}
		if node.ModelName == "" {
			return Snapshot{}, fmt.Errorf("luban: appKey %q %s must set func or modelName", appKey, location)
		}
		if err := validateAgentFields(appKey, location, node.ModelName, node.SystemPrompt, node.Temperature, node.MaxLoopCount, node.Tools, registry); err != nil {
			return Snapshot{}, err
		}
		if err := collectSkills(appKey, location, node.Skills, registry, snapshot.Skills); err != nil {
			return Snapshot{}, err
		}
		for childIndex, child := range node.SubAgents {
			childLocation := fmt.Sprintf("%s.subAgents[%d]", location, childIndex)
			if len(child.Unsupported) > 0 {
				return Snapshot{}, fmt.Errorf("luban: appKey %q %s has unsupported fields %v", appKey, childLocation, child.Unsupported)
			}
			if child.ModelName == "" || child.SystemPrompt == "" {
				return Snapshot{}, fmt.Errorf("luban: appKey %q %s requires modelName and systemPrompt", appKey, childLocation)
			}
			if err := validateAgentFields(appKey, childLocation, child.ModelName, child.SystemPrompt, child.Temperature, child.MaxLoopCount, child.Tools, registry); err != nil {
				return Snapshot{}, err
			}
			if err := collectSkills(appKey, childLocation, child.Skills, registry, snapshot.Skills); err != nil {
				return Snapshot{}, err
			}
		}
	}
	return snapshot, nil
}

func validateAgentFields(
	appKey, location, modelName, systemPrompt string,
	temperature *float64,
	maxLoopCount *int,
	toolNames []string,
	registry Registry,
) error {
	if modelName == "" || systemPrompt == "" {
		return fmt.Errorf("luban: appKey %q %s requires modelName and systemPrompt", appKey, location)
	}
	if _, exists := registry.Models[modelName]; !exists {
		return fmt.Errorf("luban: appKey %q %s model %q was not registered", appKey, location, modelName)
	}
	if temperature != nil && (math.IsNaN(*temperature) || math.IsInf(*temperature, 0) || *temperature < 0) {
		return fmt.Errorf("luban: appKey %q %s temperature must be finite and nonnegative", appKey, location)
	}
	if maxLoopCount != nil && *maxLoopCount <= 0 {
		return fmt.Errorf("luban: appKey %q %s maxLoopCount must be positive when specified", appKey, location)
	}
	for _, name := range toolNames {
		if _, exists := registry.Tools[name]; !exists {
			return fmt.Errorf("luban: appKey %q %s Tool %q was not registered", appKey, location, name)
		}
	}
	return nil
}

func collectSkills(
	appKey, location string,
	names []string,
	registry Registry,
	dest map[string]Skill,
) error {
	for _, name := range names {
		if skill, exists := registry.Skills[name]; exists {
			dest[name] = skill
			continue
		}
		if _, exists := registry.SkillBackends[name]; exists {
			continue
		}
		return fmt.Errorf("luban: appKey %q %s Skill or SkillBackend %q was not registered", appKey, location, name)
	}
	return nil
}

func cloneAgent(src AgentConfig) AgentConfig {
	dst := AgentConfig{Description: src.Description, ProjectKey: src.ProjectKey, Flow: make([]NodeConfig, len(src.Flow))}
	for i, node := range src.Flow {
		dst.Flow[i] = cloneNode(node)
	}
	return dst
}

func cloneNode(src NodeConfig) NodeConfig {
	dst := src
	dst.Temperature = cloneFloat64(src.Temperature)
	dst.MaxLoopCount = cloneInt(src.MaxLoopCount)
	dst.Tools = append([]string(nil), src.Tools...)
	dst.Skills = append([]string(nil), src.Skills...)
	dst.SubAgents = make([]SubAgentConfig, len(src.SubAgents))
	for i, child := range src.SubAgents {
		dst.SubAgents[i] = cloneSubAgent(child)
	}
	return dst
}

func cloneSubAgent(src SubAgentConfig) SubAgentConfig {
	dst := src
	dst.Temperature = cloneFloat64(src.Temperature)
	dst.MaxLoopCount = cloneInt(src.MaxLoopCount)
	dst.Tools = append([]string(nil), src.Tools...)
	dst.Skills = append([]string(nil), src.Skills...)
	dst.Unsupported = append([]string(nil), src.Unsupported...)
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
