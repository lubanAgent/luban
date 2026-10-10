package luban_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/lubanAgent/luban/sdk"
)

type lessonReview struct {
	Score int `json:"score"`
}

type sampleTool struct{}

func (sampleTool) Name() string        { return "sample" }
func (sampleTool) Description() string { return "A public API test tool." }
func (sampleTool) Input() any          { return struct{}{} }
func (sampleTool) Invoke(*gin.Context, any) (any, error) {
	return map[string]bool{"ok": true}, nil
}

type sampleModel struct{}

func (sampleModel) Name() string { return "sample-model" }
func (sampleModel) Invoke(*gin.Context, luban.ModelRequest) (luban.ModelResponse, error) {
	return luban.ModelResponse{}, nil
}

func TestPublicTypesAreUsableFromExternalPackage(t *testing.T) {
	maxLoops := 4
	config := luban.Config{
		Agents: map[string]luban.AgentConfig{
			"lessonReview": {
				Flow: []luban.NodeConfig{{
					ModelName:    "sample-model",
					SystemPrompt: "Review the lesson.",
					MaxLoopCount: &maxLoops,
				}},
			},
		},
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) == 0 {
		t.Fatal("public Config encoded to empty JSON")
	}

	var _ luban.Tool = sampleTool{}
	var _ luban.ModelInvoker = sampleModel{}
	_ = luban.Skill{Name: "lesson-review", Description: "Review guidance", Content: "Use evidence."}
	_ = luban.Request{
		Messages: []luban.Message{{
			Role:    luban.RoleUser,
			Content: []luban.Part{{Type: luban.PartText, Text: "Review this lesson."}},
		}},
		Output: luban.Output[lessonReview](),
	}
	_ = luban.Batch{
		Concurrency: 2,
		Output:      luban.Output[lessonReview](),
		Items:       []luban.BatchItem{{Messages: []luban.Message{{Role: luban.RoleUser}}}},
	}
}

func TestExternalBusinessCodeCanRegisterFunctionAndRunAgent(t *testing.T) {
	rt, err := luban.New(luban.Options{Config: luban.Config{Agents: map[string]luban.AgentConfig{
		"summary": {Flow: []luban.NodeConfig{{Func: "BuildSummary"}}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Register(map[string]any{
		"BuildSummary": func(ctx *gin.Context) string {
			ctx.Set("framework", "luban")
			return "summary ready"
		},
	}, nil, nil); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, "/", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := &gin.Context{Request: request}
	output, err := rt.Run(ctx, "summary", nil)
	if err != nil {
		t.Fatal(err)
	}
	if output != "summary ready" {
		t.Fatalf("output = %#v, want business function output", output)
	}
	if value, exists := ctx.Get("framework"); !exists || value != "luban" {
		t.Fatalf("Gin Context did not remain available to the function: %v", value)
	}
}
