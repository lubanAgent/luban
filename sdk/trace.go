package luban

import (
	"github.com/gin-gonic/gin"
	internaltrace "github.com/lubanAgent/luban/sdk/internal/trace"
)

// TraceEvent is a complete snapshot of one completed framework invocation.
type TraceEvent = internaltrace.Event

// TraceWriter receives framework events; implementations must allow concurrent
// calls from independent Runs and Tool callbacks.
type TraceWriter interface {
	WriteTrace(*gin.Context, TraceEvent)
}

const runIDGinKey = "__luban_run_id_v1"

// RunID returns the ID of this Run, including after Run returns so MQ cleanup
// can use the same identifier. Empty means no Run was started on this Context.
func RunID(ctx *gin.Context) string {
	if ctx == nil {
		return ""
	}
	return ctx.GetString(runIDGinKey)
}
