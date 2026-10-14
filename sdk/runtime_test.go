package luban

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v2"
)

type runtimeTestModel struct {
	name string
}

func (m *runtimeTestModel) Name() string { return m.name }
func (m *runtimeTestModel) Invoke(*gin.Context, ModelRequest) (ModelResponse, error) {
	return ModelResponse{}, nil
}

type runtimeTestTool struct {
	name        string
	description string
	panicName   bool
}

func (tool runtimeTestTool) Name() string {
	if tool.panicName {
		panic("name failure")
	}
	return tool.name
}
func (tool runtimeTestTool) Description() string { return tool.description }
func (runtimeTestTool) Input() any               { return struct{}{} }
func (runtimeTestTool) Invoke(*gin.Context, any) (any, error) {
	return nil, nil
}

type runtimeTestSkill struct {
	Name        string
	Description string
	Content     string
}

type runtimeConcreteError struct{}

func (runtimeConcreteError) Error() string { return "concrete error" }

func TestNewCopiesConfigurationAndModelsMap(t *testing.T) {
	loopCount := 3
	temperature := 0.4
	model := &runtimeTestModel{name: "model"}
	models := map[string]ModelInvoker{"model": model}
	config := Config{Agents: map[string]AgentConfig{
		"review": {Flow: []NodeConfig{{
			ModelName:    "model",
			SystemPrompt: "review",
			Temperature:  &temperature,
			MaxLoopCount: &loopCount,
			Tools:        []string{"lookup"},
			Skills:       []string{"guide"},
		}}},
	}}

	rt, err := New(Options{Config: config, Models: models})
	if err != nil {
		t.Fatal(err)
	}

	delete(models, "model")
	temperature = 0.9
	loopCount = 8
	config.Agents["review"].Flow[0].ModelName = "changed"
	config.Agents["review"].Flow[0].Tools[0] = "changed"
	config.Agents["review"] = AgentConfig{}

	node := rt.config.Agents["review"].Flow[0]
	if node.ModelName != "model" || node.Tools[0] != "lookup" {
		t.Fatalf("Runtime config changed with caller data: %+v", node)
	}
	if node.Temperature == nil || *node.Temperature != 0.4 || node.MaxLoopCount == nil || *node.MaxLoopCount != 3 {
		t.Fatalf("Runtime retained caller pointers: temperature=%v maxLoopCount=%v", node.Temperature, node.MaxLoopCount)
	}
	if rt.models["model"] != model {
		t.Fatal("Runtime did not retain the copied Models map entry")
	}
}

func TestNewRejectsInvalidModelEntries(t *testing.T) {
	var typedNil *runtimeTestModel
	for _, test := range []struct {
		name   string
		models map[string]ModelInvoker
	}{
		{name: "empty key", models: map[string]ModelInvoker{"": &runtimeTestModel{name: "model"}}},
		{name: "nil interface", models: map[string]ModelInvoker{"model": nil}},
		{name: "typed nil", models: map[string]ModelInvoker{"model": typedNil}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := New(Options{Models: test.models}); err == nil {
				t.Fatal("expected invalid model entry to fail")
			}
		})
	}
}

func TestRegisterCommitsAllRegistriesTogether(t *testing.T) {
	rt, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	err = rt.Register(
		map[string]any{"Prepare": func(*gin.Context) {}},
		[]Tool{runtimeTestTool{name: "lookup", description: "Lookup data."}},
		[]Skill{{Name: "guide", Description: "Guidance.", Content: "Use the lookup Tool."}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !rt.registered.Load() || len(rt.funcs) != 1 || len(rt.tools) != 1 || len(rt.skills) != 1 {
		t.Fatalf("registries were not committed: registered=%t funcs=%d tools=%d skills=%d",
			rt.registered.Load(), len(rt.funcs), len(rt.tools), len(rt.skills))
	}
}

func TestRegisterFailureDoesNotPublishPartialRegistries(t *testing.T) {
	rt, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	err = rt.Register(
		map[string]any{"Prepare": func(*gin.Context) {}},
		[]Tool{runtimeTestTool{name: "lookup", description: ""}},
		[]Skill{{Name: "guide", Description: "Guidance.", Content: "Body."}},
	)
	if err == nil {
		t.Fatal("expected invalid Tool metadata to fail")
	}
	if rt.registered.Load() || len(rt.funcs) != 0 || len(rt.tools) != 0 || len(rt.skills) != 0 {
		t.Fatal("failed registration published a partial registry")
	}
	if err := rt.Register(nil, nil, nil); err == nil {
		t.Fatal("Register retry should be rejected after the first attempt")
	}
}

func TestRegisterRejectsDuplicateOrInvalidToolAndSkillMetadata(t *testing.T) {
	tests := []struct {
		name   string
		tools  []Tool
		skills []Skill
	}{
		{name: "duplicate Tool", tools: []Tool{
			runtimeTestTool{name: "lookup", description: "First."},
			runtimeTestTool{name: "lookup", description: "Second."},
		}},
		{name: "empty Tool name", tools: []Tool{runtimeTestTool{description: "Lookup."}}},
		{name: "panicking Tool metadata", tools: []Tool{runtimeTestTool{name: "lookup", description: "Lookup.", panicName: true}}},
		{name: "duplicate Skill", skills: []Skill{
			{Name: "guide", Description: "First.", Content: "A"},
			{Name: "guide", Description: "Second.", Content: "B"},
		}},
		{name: "empty Skill content", skills: []Skill{{Name: "guide", Description: "Guidance."}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rt, err := New(Options{})
			if err != nil {
				t.Fatal(err)
			}
			if err := rt.Register(nil, test.tools, test.skills); err == nil {
				t.Fatal("expected invalid registration to fail")
			}
			if rt.registered.Load() {
				t.Fatal("invalid registrations must not be published")
			}
		})
	}
}

func TestLoadSnapshotValidatesConfigReferencesAndCachesSkillSnapshot(t *testing.T) {
	rt, err := New(Options{
		Config: Config{Agents: map[string]AgentConfig{
			"review": {Flow: []NodeConfig{{
				ModelName:    "model",
				SystemPrompt: "review",
				Tools:        []string{"lookup"},
				Skills:       []string{"guide"},
			}}},
		}},
		Models: map[string]ModelInvoker{"model": &runtimeTestModel{name: "model"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Register(nil,
		[]Tool{runtimeTestTool{name: "lookup", description: "Lookup."}},
		[]Skill{{Name: "guide", Description: "Guidance.", Content: "Original."}},
	); err != nil {
		t.Fatal(err)
	}

	first, err := rt.loadSnapshot("review")
	if err != nil {
		t.Fatal(err)
	}
	first.skills["guide"] = Skill{Content: "caller mutation"}
	second, err := rt.loadSnapshot("review")
	if err != nil {
		t.Fatal(err)
	}
	if second.skills["guide"].Content != "Original." {
		t.Fatalf("caller mutated cached Skill: %+v", second.skills["guide"])
	}

	if _, err := rt.loadSnapshot("missing"); err == nil {
		t.Fatal("expected unknown appKey error")
	}
	if _, err := rt.loadSnapshot(""); err == nil {
		t.Fatal("expected empty appKey error")
	}
}

func TestLoadSnapshotRejectsInvalidAgentShapes(t *testing.T) {
	tests := []struct {
		name string
		flow []NodeConfig
	}{
		{name: "empty flow"},
		{name: "no node kind", flow: []NodeConfig{{SystemPrompt: "prompt"}}},
		{name: "business function has Agent fields", flow: []NodeConfig{{Func: "Prepare", SystemPrompt: "prompt"}}},
		{name: "missing model", flow: []NodeConfig{{ModelName: "missing", SystemPrompt: "prompt"}}},
		{name: "empty system prompt", flow: []NodeConfig{{ModelName: "model"}}},
		{name: "zero max loop", flow: []NodeConfig{{ModelName: "model", SystemPrompt: "prompt", MaxLoopCount: intPointer(0)}}},
		{name: "negative max loop", flow: []NodeConfig{{ModelName: "model", SystemPrompt: "prompt", MaxLoopCount: intPointer(-1)}}},
		{name: "unknown Tool", flow: []NodeConfig{{ModelName: "model", SystemPrompt: "prompt", Tools: []string{"missing"}}}},
		{name: "unknown Skill", flow: []NodeConfig{{ModelName: "model", SystemPrompt: "prompt", Skills: []string{"missing"}}}},
		{name: "missing function", flow: []NodeConfig{{Func: "missing"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rt, err := New(Options{
				Config: Config{Agents: map[string]AgentConfig{"review": {Flow: test.flow}}},
				Models: map[string]ModelInvoker{"model": &runtimeTestModel{name: "model"}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := rt.Register(map[string]any{"Prepare": func(*gin.Context) {}},
				[]Tool{runtimeTestTool{name: "lookup", description: "Lookup."}},
				[]Skill{{Name: "guide", Description: "Guidance.", Content: "Body."}},
			); err != nil {
				t.Fatal(err)
			}
			if _, err := rt.loadSnapshot("review"); err == nil {
				t.Fatal("expected invalid config to fail")
			}
		})
	}
}

func TestLoadSnapshotRejectsChildFieldsAndInvalidChildReferences(t *testing.T) {
	tests := []struct {
		name  string
		child SubAgentConfig
	}{
		{name: "forbidden nested subAgent", child: SubAgentConfig{
			ModelName: "model", SystemPrompt: "child", unsupportedFields: []string{"subAgents"},
		}},
		{name: "forbidden child flow", child: SubAgentConfig{
			ModelName: "model", SystemPrompt: "child", unsupportedFields: []string{"flow"},
		}},
		{name: "forbidden child function", child: SubAgentConfig{
			ModelName: "model", SystemPrompt: "child", unsupportedFields: []string{"func"},
		}},
		{name: "missing model", child: SubAgentConfig{SystemPrompt: "child"}},
		{name: "unknown Tool", child: SubAgentConfig{ModelName: "model", SystemPrompt: "child", Tools: []string{"missing"}}},
		{name: "zero max loop", child: SubAgentConfig{ModelName: "model", SystemPrompt: "child", MaxLoopCount: intPointer(0)}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rt, err := New(Options{
				Config: Config{Agents: map[string]AgentConfig{
					"review": {Flow: []NodeConfig{{ModelName: "model", SystemPrompt: "parent", SubAgents: []SubAgentConfig{test.child}}}},
				}},
				Models: map[string]ModelInvoker{"model": &runtimeTestModel{name: "model"}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := rt.Register(nil, []Tool{runtimeTestTool{name: "lookup", description: "Lookup."}}, nil); err != nil {
				t.Fatal(err)
			}
			if _, err := rt.loadSnapshot("review"); err == nil {
				t.Fatal("expected invalid child config to fail")
			}
		})
	}
}

func TestLoadSnapshotRejectsForbiddenChildFieldsAfterJSONAndYAMLDecode(t *testing.T) {
	fields := []struct {
		name string
		raw  string
	}{
		{name: "func", raw: `"func":"nested"`},
		{name: "flow", raw: `"flow":[{"func":"nested"}]`},
		{name: "subAgents", raw: `"subAgents":[{"modelName":"grandchild","systemPrompt":"nested"}]`},
	}
	decoders := []struct {
		name string
		fn   func([]byte, any) error
	}{
		{name: "json", fn: json.Unmarshal},
		{name: "project-yaml-loader", fn: yaml.Unmarshal},
	}
	for _, decoder := range decoders {
		for _, field := range fields {
			t.Run(decoder.name+"/"+field.name, func(t *testing.T) {
				raw := []byte(`{"review":{"flow":[{"modelName":"model","systemPrompt":"parent","subAgents":[{"modelName":"child","systemPrompt":"child",` + field.raw + `}]}]}}`)
				var config Config
				if err := decoder.fn(raw, &config); err != nil {
					t.Fatal(err)
				}
				rt, err := New(Options{
					Config: config,
					Models: map[string]ModelInvoker{"model": &runtimeTestModel{name: "model"}},
				})
				if err != nil {
					t.Fatal(err)
				}
				if err := rt.Register(nil, nil, nil); err != nil {
					t.Fatal(err)
				}
				if _, err := rt.loadSnapshot("review"); err == nil || !strings.Contains(err.Error(), field.name) {
					t.Fatalf("decoded forbidden child field was not rejected: %v", err)
				}
			})
		}
	}
}

func intPointer(value int) *int { return &value }

func TestRegisterRejectsConcreteErrorReturnSignature(t *testing.T) {
	rt, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	err = rt.Register(map[string]any{"Bad": func(*gin.Context) runtimeConcreteError { return runtimeConcreteError{} }}, nil, nil)
	if err == nil {
		t.Fatal("expected a concrete error type return to be rejected")
	}
}
