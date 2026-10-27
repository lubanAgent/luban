package luban

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sort"
)

// Validate checks all configured Agents without executing business or models.
// Service hosts call it after Register and before accepting requests.
func (rt *Runtime) Validate() error {
	if rt == nil {
		return fmt.Errorf("luban: nil Runtime")
	}
	for agent := range rt.agents {
		if _, exists := rt.config.Agents[agent]; !exists {
			return fmt.Errorf("luban: registered Agent %q has no configuration", agent)
		}
	}
	for _, key := range rt.AgentKeys() {
		agent := rt.config.Agents[key]
		if rt.hasProjectModelRouting() {
			for _, node := range agent.Flow {
				for _, model := range append([]string{node.ModelName}, childModelNames(node.SubAgents)...) {
					if model != "" {
						if _, err := rt.modelFor(agent.ProjectKey, model); err != nil {
							return fmt.Errorf("luban: Agent %q: %w", key, err)
						}
					}
				}
			}
		}
		snapshot, err := rt.loadSnapshot(key)
		if err != nil {
			return err
		}
		if err := rt.validateAgentFlowSignatures(key, snapshot.config.Flow); err != nil {
			return fmt.Errorf("luban: Agent %q: %w", key, err)
		}
	}
	return nil
}

// AgentKeys returns configured names in stable order.
func (rt *Runtime) AgentKeys() []string {
	if rt == nil {
		return nil
	}
	keys := make([]string, 0, len(rt.config.Agents))
	for key := range rt.config.Agents {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// DecodeInput decodes an HTTP/CLI JSON input according to the first node.
// A function with only a Gin argument owns body parsing and receives nil.
func (rt *Runtime) DecodeInput(agentKey string, raw []byte) (any, error) {
	snapshot, err := rt.loadSnapshot(agentKey)
	if err != nil {
		return nil, err
	}
	first := snapshot.config.Flow[0]
	if first.Func != "" {
		fn := rt.functionsFor(agentKey)[first.Func]
		if !fn.HasInput {
			return nil, nil
		}
		value := reflect.New(fn.InputType)
		if err := decodeSingleJSON(raw, value.Interface()); err != nil {
			return nil, fmt.Errorf("luban: Agent %q input for %s: %w", agentKey, first.Func, err)
		}
		if value.Elem().IsZero() && bytes.Equal(bytes.TrimSpace(raw), []byte("null")) && !isNilable(fn.InputType) {
			return nil, fmt.Errorf("luban: Agent %q null is not assignable to %v", agentKey, fn.InputType)
		}
		return value.Elem().Interface(), nil
	}
	var wire struct {
		UserPrompt *string    `json:"userPrompt"`
		Images     *[]string  `json:"images"`
		Videos     *[]string  `json:"videos"`
		Messages   *[]Message `json:"messages"`
	}
	if err := decodeSingleJSON(raw, &wire); err != nil {
		return nil, fmt.Errorf("luban: Agent %q input: %w", agentKey, err)
	}
	short := wire.UserPrompt != nil || wire.Images != nil || wire.Videos != nil
	if wire.Messages != nil {
		if short {
			return nil, fmt.Errorf("luban: messages and userPrompt/images/videos are mutually exclusive")
		}
		if len(*wire.Messages) == 0 {
			return nil, fmt.Errorf("luban: messages must not be empty")
		}
		return Request{Messages: *wire.Messages}, nil
	}
	var parts []Part
	if wire.UserPrompt != nil && *wire.UserPrompt != "" {
		parts = append(parts, Part{Type: PartText, Text: *wire.UserPrompt})
	}
	for _, media := range []struct {
		urls *[]string
		kind PartType
	}{{wire.Images, PartImage}, {wire.Videos, PartVideo}} {
		if media.urls != nil {
			for _, url := range *media.urls {
				if url == "" {
					return nil, fmt.Errorf("luban: %s URL must not be empty", media.kind)
				}
				parts = append(parts, Part{Type: media.kind, URL: url})
			}
		}
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("luban: provide messages or userPrompt/images/videos")
	}
	return Request{Messages: []Message{{Role: RoleUser, Content: parts}}}, nil
}

func decodeSingleJSON(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid JSON input")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("input must contain one JSON value")
	}
	return nil
}
