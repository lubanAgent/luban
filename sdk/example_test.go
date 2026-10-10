package luban_test

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	luban "github.com/lubanAgent/luban/sdk"
)

type offlineModel struct{}

func (*offlineModel) Name() string { return "offline" }

func (*offlineModel) Invoke(_ *gin.Context, _ luban.ModelRequest) (luban.ModelResponse, error) {
	return luban.ModelResponse{
		Content: []luban.ModelPart{{Type: luban.PartText, Text: "The lesson is clear."}},
	}, nil
}

func ExampleRuntime_Run() {
	rt, err := luban.New(luban.Options{
		Config: luban.Config{Agents: map[string]luban.AgentConfig{
			"lessonReview": {Flow: []luban.NodeConfig{
				{Func: "PrepareReview"},
				{ModelName: "offline", SystemPrompt: "Review the lesson briefly."},
				{Func: "SaveReview"},
			}},
		}},
		Models: map[string]luban.ModelInvoker{"offline": &offlineModel{}},
	})
	if err != nil {
		panic(err)
	}
	if err := rt.Register(map[string]any{
		"PrepareReview": func(_ *gin.Context, lessonID int64) luban.Request {
			return luban.Request{Messages: []luban.Message{{
				Role: luban.RoleUser,
				Content: []luban.Part{{
					Type: luban.PartText,
					Text: fmt.Sprintf("Review lesson %d.", lessonID),
				}},
			}}}
		},
		"SaveReview": func(_ *gin.Context, summary string) string {
			return "saved: " + summary
		},
	}, nil, nil); err != nil {
		panic(err)
	}

	ctx := &gin.Context{Request: &http.Request{}}
	result, err := rt.Run(ctx, "lessonReview", int64(42))
	if err != nil {
		panic(err)
	}
	fmt.Println(result)
	// Output: saved: The lesson is clear.
}
