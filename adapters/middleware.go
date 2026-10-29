package adapters

import (
	"runtime/debug"

	"git.yukework.com/pkg/golib/v2/base"
	"git.yukework.com/pkg/golib/v2/zlog"
	"github.com/gin-gonic/gin"
)

// recoverRequest 是 HTTP 层的 panic 兜底。
// 记录堆栈，并按公司的错误协议返回；Agent 内部的错误处理仍由 SDK 负责。
func recoverRequest(ctx *gin.Context) {
	defer func() {
		if recover() != nil {
			zlog.Errorf(ctx, "HTTP handler panicked; stack: %s", debug.Stack())
			base.RenderJsonAbort(ctx, base.NewError(5000, "system internal error", ""))
		}
	}()
	ctx.Next()
}
