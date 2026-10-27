package callsummary

import (
	"fmt"

	"github.com/gin-gonic/gin"
	luban "github.com/lubanAgent/luban/sdk"
)

func init() { luban.RegisterFuncs(PrepareCall, BuildResult) }

// CallInput is automatically decoded by the service's HTTP/CLI entry point.
type CallInput struct {
	CallID int64  `json:"callId"`
	Text   string `json:"text"`
}

// PrepareCall is a normal business function. A production service can fetch
// the transcript through models/ or api/ before returning the same Request.
func PrepareCall(ctx *gin.Context, input CallInput) (luban.Request, error) {
	if input.Text == "" {
		return luban.Request{}, fmt.Errorf("call text must not be empty")
	}
	if err := luban.Set(ctx, "callId", input.CallID); err != nil {
		return luban.Request{}, err
	}
	return luban.Request{Messages: []luban.Message{{Role: luban.RoleUser, Content: []luban.Part{{Type: luban.PartText, Text: input.Text}}}}}, nil
}

// BuildResult receives the preceding Agent's raw string result.
func BuildResult(ctx *gin.Context, summary string) map[string]any {
	id, _ := luban.Get(ctx, "callId")
	return map[string]any{"callId": id, "summary": summary}
}
