package eino

import (
	"fmt"
	"sync/atomic"

	"github.com/gin-gonic/gin"
)

const toolCallGuardGinKey = "__luban_tool_call_guard_v1"

type toolCallGuard struct {
	err      error
	active   atomic.Bool
	rejected atomic.Bool
}

func newToolCallGuard(ctx *gin.Context, name string) *toolCallGuard {
	guard := &toolCallGuard{err: fmt.Errorf("luban: Tool %q must not call Stop; only flow business functions may stop", name)}
	guard.active.Store(true)
	ctx.Set(toolCallGuardGinKey, guard)
	return guard
}

// RejectToolStop records misuse in this specific Tool call. The caller panics
// to abort the callback; the invocation boundary also checks rejected in case
// business code catches that panic itself. No Run-global state is changed.
func RejectToolStop(ctx *gin.Context) error {
	value, _ := ctx.Get(toolCallGuardGinKey)
	guard, _ := value.(*toolCallGuard)
	if guard == nil || !guard.active.Load() {
		return nil
	}
	guard.rejected.Store(true)
	return guard.err
}
