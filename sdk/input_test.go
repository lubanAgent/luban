package luban

import (
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestDecodeInputPreservesTypedFirstFunctionAndShortMedia(t *testing.T) {
	type input struct {
		ID int64 `json:"id"`
	}
	rt, err := New(Options{Config: Config{Agents: map[string]AgentConfig{
		"typed": {Flow: []NodeConfig{{Func: "prepare"}}},
		"self":  {Flow: []NodeConfig{{Func: "self"}}},
		"model": {Flow: []NodeConfig{{ModelName: "model", SystemPrompt: "test"}}},
	}}, Models: map[string]ModelInvoker{"model": &runtimeTestModel{name: "model"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Register(map[string]any{
		"prepare": func(_ *gin.Context, value input) input { return value },
		"self":    func(*gin.Context) {},
	}, nil, nil); err != nil {
		t.Fatal(err)
	}
	value, err := rt.DecodeInput("typed", []byte(`{"id":7}`))
	if err != nil || value != (input{ID: 7}) {
		t.Fatalf("typed input = %#v, %v", value, err)
	}
	if _, err := rt.DecodeInput("typed", []byte("null")); err == nil {
		t.Fatal("null decoded into value struct")
	}
	if _, err := rt.DecodeInput("self", []byte("arbitrary body")); err != nil {
		t.Fatalf("ctx-only function must own binding: %v", err)
	}
	value, err = rt.DecodeInput("model", []byte(`{"userPrompt":"inspect","images":["https://a.test/image"],"videos":["https://a.test/video"]}`))
	if err != nil {
		t.Fatal(err)
	}
	parts := value.(Request).Messages[0].Content
	if len(parts) != 3 || parts[0].Type != PartText || parts[1].Type != PartImage || parts[2].Type != PartVideo {
		t.Fatalf("parts = %+v", parts)
	}
	for _, raw := range []string{`{}`, `null`, `{"messages":[],"userPrompt":"x"}`, `{"images":[""]}`, `{"messages":[]}`, `{} {}`} {
		if _, err := rt.DecodeInput("model", []byte(raw)); err == nil {
			t.Errorf("accepted invalid input %s", raw)
		}
	}
}

func TestRegistrationOwnerAndNamedFunctionRestrictions(t *testing.T) {
	if _, err := registrationOwner(0, false); err == nil {
		t.Fatal("invalid caller accepted")
	}
	if _, err := registeredFunctionName(func(*gin.Context) {}); err == nil || !strings.Contains(err.Error(), "closure") {
		t.Fatalf("closure error = %v", err)
	}
	if _, err := registeredFunctionName(nil); err == nil {
		t.Fatal("nil function accepted")
	}
}
