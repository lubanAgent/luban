package adapters

import (
	"runtime/debug"

	"git.yukework.com/pkg/golib/v2/base"
	"git.yukework.com/pkg/golib/v2/zlog"
	ips "git.yukework.com/saas/ips-client/v2"
	"github.com/gin-gonic/gin"
)

// sessionReader 只约定“如何获取当前登录会话”。
// 正式运行传 IPS 客户端，测试时可以传假的实现，不必访问真实登录服务。
type sessionReader interface {
	GetSession(*gin.Context) (ips.IpsSession, error)
}

// checkLogin 在业务 HTTP 请求执行前检查 IPS 登录态。
// 只有校验通过的会话才能设置 operatorId，业务不能通过请求参数冒充操作人。
func checkLogin(client sessionReader) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		session, err := client.GetSession(ctx)
		if err != nil || session.Uid <= 0 {
			// 返回公司统一的登录错误，并终止后续处理，避免继续执行 Agent。
			base.RenderJsonFail(ctx, base.NewError(5002, "登录已过期，请重新登录", ""))
			ctx.Abort()
			return
		}
		// 保留公司常用的 Context 字段；Agent 的业务函数可读取 operatorId。
		ctx.Set("CtxIpsUname", session.Uname)
		ctx.Set("CtxIpsUId", int(session.Uid))
		ctx.Set("operatorId", session.Uid)
		// 把操作人加入本次请求的日志，后续排查可以关联到具体用户。
		zlog.AddField(ctx, zlog.String("ipsUname", session.Uname))
		ctx.Next()
	}
}

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
