package luban

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Config contains the named Agent configurations available to a Runtime.
type Config struct {
	Agents map[string]AgentConfig `json:"agents" yaml:"agents"`
}

// AgentConfig describes an Agent and its ordered flow.
type AgentConfig struct {
	Description string       `json:"description,omitempty" yaml:"description,omitempty"`
	Flow        []NodeConfig `json:"flow" yaml:"flow"`
}

// NodeConfig describes one business-function or Agent node.
//
// Exactly one of Func or ModelName is used by a valid node. MaxLoopCount is
// nil when omitted or set to JSON/YAML null; the Runtime applies the default.
type NodeConfig struct {
	Description  string           `json:"description,omitempty" yaml:"description,omitempty"`
	Func         string           `json:"func,omitempty" yaml:"func,omitempty"`
	ModelName    string           `json:"modelName,omitempty" yaml:"modelName,omitempty"`
	SystemPrompt string           `json:"systemPrompt,omitempty" yaml:"systemPrompt,omitempty"`
	Temperature  *float64         `json:"temperature,omitempty" yaml:"temperature,omitempty"`
	Tools        []string         `json:"tools,omitempty" yaml:"tools,omitempty"`
	Skills       []string         `json:"skills,omitempty" yaml:"skills,omitempty"`
	SubAgents    []SubAgentConfig `json:"subAgents,omitempty" yaml:"subAgents,omitempty"`
	MaxLoopCount *int             `json:"maxLoopCount,omitempty" yaml:"maxLoopCount,omitempty"`
}

// SubAgentConfig describes a child Agent. Child Agents are limited to one
// level and use their own model and prompt settings.
//
// Unsupported non-empty func, flow, and nested subAgents keys are retained
// during JSON/YAML decoding so the Runtime can report the invalid
// configuration instead of silently ignoring it.
type SubAgentConfig struct {
	Description  string   `json:"description,omitempty" yaml:"description,omitempty"`
	ModelName    string   `json:"modelName" yaml:"modelName"`
	SystemPrompt string   `json:"systemPrompt" yaml:"systemPrompt"`
	Temperature  *float64 `json:"temperature,omitempty" yaml:"temperature,omitempty"`
	Tools        []string `json:"tools,omitempty" yaml:"tools,omitempty"`
	Skills       []string `json:"skills,omitempty" yaml:"skills,omitempty"`
	MaxLoopCount *int     `json:"maxLoopCount,omitempty" yaml:"maxLoopCount,omitempty"`

	unsupportedFields []string `json:"-" yaml:"-"`
}

// UnmarshalJSON retains the known child-only forbidden keys while continuing
// to ignore unrelated unknown fields.
func (c *SubAgentConfig) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		*c = SubAgentConfig{}
		return nil
	}

	type plain SubAgentConfig
	var wire struct {
		*plain
		Func      string          `json:"func"`
		Flow      json.RawMessage `json:"flow"`
		SubAgents json.RawMessage `json:"subAgents"`
	}
	wire.plain = (*plain)(c)
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}

	fields, err := unsupportedChildFields(wire.Func, wire.Flow, wire.SubAgents)
	if err != nil {
		return err
	}
	c.unsupportedFields = fields
	return nil
}

// UnmarshalYAML supports the loader used by record-style env.LoadConf without
// importing that loader or a YAML package into the SDK implementation.
func (c *SubAgentConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain SubAgentConfig
	var wire struct {
		Plain     plain         `yaml:",inline"`
		Func      string        `yaml:"func"`
		Flow      []interface{} `yaml:"flow"`
		SubAgents []interface{} `yaml:"subAgents"`
	}
	if err := unmarshal(&wire); err != nil {
		return err
	}

	*c = SubAgentConfig(wire.Plain)
	fields := make([]string, 0, 2)
	if wire.Func != "" {
		fields = append(fields, "func")
	}
	if len(wire.Flow) > 0 {
		fields = append(fields, "flow")
	}
	if len(wire.SubAgents) > 0 {
		fields = append(fields, "subAgents")
	}
	c.unsupportedFields = fields
	return nil
}

func unsupportedChildFields(function string, flow, nested json.RawMessage) ([]string, error) {
	fields := make([]string, 0, 3)
	if function != "" {
		fields = append(fields, "func")
	}
	for _, forbidden := range []struct {
		name string
		raw  json.RawMessage
	}{
		{name: "flow", raw: flow},
		{name: "subAgents", raw: nested},
	} {
		if len(forbidden.raw) == 0 || bytes.Equal(bytes.TrimSpace(forbidden.raw), []byte("null")) {
			continue
		}
		var entries []json.RawMessage
		if err := json.Unmarshal(forbidden.raw, &entries); err != nil {
			return nil, fmt.Errorf("%s must be an array or null: %w", forbidden.name, err)
		}
		if len(entries) > 0 {
			fields = append(fields, forbidden.name)
		}
	}
	return fields, nil
}

func (c SubAgentConfig) unsupportedConfigFields() []string {
	return append([]string(nil), c.unsupportedFields...)
}
