package luban

import (
	"encoding/json"
	"reflect"
	"testing"

	"gopkg.in/yaml.v2"
)

const sampleConfigJSON = `{
  "lessonReview": {
    "flow": [{
      "modelName": "review-model",
      "systemPrompt": "Review the lesson.",
      "maxLoopCount": 3
    }]
  }
}`

func TestConfigJSONAndProjectYAMLDecodeCamelCaseKeys(t *testing.T) {
	for _, decode := range []struct {
		name string
		fn   func([]byte, any) error
	}{
		{name: "json", fn: json.Unmarshal},
		{name: "project-yaml-loader", fn: yaml.Unmarshal},
	} {
		t.Run(decode.name, func(t *testing.T) {
			var config Config
			if err := decode.fn([]byte(sampleConfigJSON), &config); err != nil {
				t.Fatal(err)
			}
			node := config.Agents["lessonReview"].Flow[0]
			if node.ModelName != "review-model" || node.SystemPrompt != "Review the lesson." {
				t.Fatalf("camelCase fields were not decoded: %+v", node)
			}
			if node.MaxLoopCount == nil || *node.MaxLoopCount != 3 {
				t.Fatalf("maxLoopCount was not decoded: %+v", node.MaxLoopCount)
			}
		})
	}
}

func TestConfigRootKeysRoundTrip(t *testing.T) {
	for _, decode := range []struct {
		name string
		fn   func([]byte, any) error
	}{
		{name: "json", fn: json.Unmarshal},
		{name: "project-yaml-loader", fn: yaml.Unmarshal},
	} {
		t.Run(decode.name, func(t *testing.T) {
			var config Config
			if err := decode.fn([]byte(sampleConfigJSON), &config); err != nil {
				t.Fatal(err)
			}
			if len(config.Agents) != 1 || len(config.Agents["lessonReview"].Flow) != 1 {
				t.Fatalf("root appKey was not decoded: %+v", config.Agents)
			}
			raw, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			var root map[string]json.RawMessage
			if err := json.Unmarshal(raw, &root); err != nil {
				t.Fatal(err)
			}
			if len(root) != 1 || len(root["lessonReview"]) == 0 {
				t.Fatalf("root config was wrapped unexpectedly: %s", raw)
			}
		})
	}
}

func TestMaxLoopCountPresenceAndDefaultSemantics(t *testing.T) {
	for _, decode := range []struct {
		name string
		fn   func([]byte, any) error
	}{
		{name: "json", fn: json.Unmarshal},
		{name: "project-yaml-loader", fn: yaml.Unmarshal},
	} {
		t.Run(decode.name, func(t *testing.T) {
			var missing, explicitNull, explicitZero, positive NodeConfig
			inputs := []struct {
				raw string
				dst *NodeConfig
			}{
				{`{}`, &missing},
				{`{"maxLoopCount":null}`, &explicitNull},
				{`{"maxLoopCount":0}`, &explicitZero},
				{`{"maxLoopCount":4}`, &positive},
			}
			for _, input := range inputs {
				if err := decode.fn([]byte(input.raw), input.dst); err != nil {
					t.Fatal(err)
				}
			}
			if missing.MaxLoopCount != nil || explicitNull.MaxLoopCount != nil {
				t.Fatalf("missing and null should use the default: missing=%v null=%v", missing.MaxLoopCount, explicitNull.MaxLoopCount)
			}
			if explicitZero.MaxLoopCount == nil || *explicitZero.MaxLoopCount != 0 {
				t.Fatalf("explicit zero lost its presence: %v", explicitZero.MaxLoopCount)
			}
			if positive.MaxLoopCount == nil || *positive.MaxLoopCount != 4 {
				t.Fatalf("positive value was not preserved: %v", positive.MaxLoopCount)
			}
		})
	}
}

func TestSubAgentRetainsKnownForbiddenShapesAcrossDecoders(t *testing.T) {
	const raw = `{"modelName":"child","systemPrompt":"prompt","func":"business","flow":[{"func":"nested"}],"subAgents":[{"modelName":"grandchild","systemPrompt":"prompt"}]}`
	for _, decode := range []struct {
		name string
		fn   func([]byte, any) error
	}{
		{name: "json", fn: json.Unmarshal},
		{name: "project-yaml-loader", fn: yaml.Unmarshal},
	} {
		t.Run(decode.name, func(t *testing.T) {
			var child SubAgentConfig
			if err := decode.fn([]byte(raw), &child); err != nil {
				t.Fatal(err)
			}
			if got, want := child.unsupportedConfigFields(), []string{"func", "flow", "subAgents"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("unsupported fields = %v, want %v", got, want)
			}
		})
	}
}

func TestSubAgentAllowsNullAndEmptyNestedList(t *testing.T) {
	for _, raw := range []string{
		`{"modelName":"child","systemPrompt":"prompt","flow":null,"subAgents":null}`,
		`{"modelName":"child","systemPrompt":"prompt","flow":[],"subAgents":[]}`,
	} {
		var child SubAgentConfig
		if err := json.Unmarshal([]byte(raw), &child); err != nil {
			t.Fatal(err)
		}
		if got := child.unsupportedConfigFields(); len(got) != 0 {
			t.Fatalf("empty optional child fields were marked unsupported: %v", got)
		}
	}
}

func TestSubAgentRejectsMalformedNestedFieldType(t *testing.T) {
	var child SubAgentConfig
	if err := json.Unmarshal([]byte(`{"modelName":"child","systemPrompt":"prompt","subAgents":"bad"}`), &child); err == nil {
		t.Fatal("expected invalid subAgents type to be rejected by the decoder")
	}
}

func TestSubAgentNullDecodeClearsPreviousForbiddenMarkers(t *testing.T) {
	var child SubAgentConfig
	if err := json.Unmarshal([]byte(`{"modelName":"child","systemPrompt":"prompt","func":"forbidden"}`), &child); err != nil {
		t.Fatal(err)
	}
	if len(child.unsupportedConfigFields()) != 1 {
		t.Fatalf("expected a forbidden marker, got %v", child.unsupportedConfigFields())
	}
	if err := json.Unmarshal([]byte(`null`), &child); err != nil {
		t.Fatal(err)
	}
	if got := child.unsupportedConfigFields(); len(got) != 0 {
		t.Fatalf("null decode retained stale markers: %v", got)
	}
}
