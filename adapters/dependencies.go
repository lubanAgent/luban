package adapters

import (
	"git.yukework.com/pkg/golib/v2/base"
	"git.yukework.com/pkg/golib/v2/zlog"
	"github.com/gin-gonic/gin"
	luban "github.com/lubanAgent/luban/sdk"
	"github.com/lubanAgent/luban/server"
	"go.uber.org/zap"
)

// 编译时检查：如果这里漏实现了接口要求的方法，Go 编译器会直接报错。
// 这两行不创建对象，也不会在运行时执行日志操作。
var (
	_ server.Logger     = companyLogger{}
	_ luban.TraceWriter = companyTraceWriter{}
)

// companyLogger 把服务的普通日志转交给公司 zlog。
// 框架只调用 Debugf/Infof/Warnf/Errorf，不需要知道 zlog 的内部实现。
// ctx 原样传入，让 zlog 按自身规则附带当前请求的日志字段。
type companyLogger struct{}

func (companyLogger) Debugf(ctx *gin.Context, format string, args ...any) {
	zlog.Debugf(ctx, format, args...)
}
func (companyLogger) Infof(ctx *gin.Context, format string, args ...any) {
	zlog.Infof(ctx, format, args...)
}
func (companyLogger) Warnf(ctx *gin.Context, format string, args ...any) {
	zlog.Warnf(ctx, format, args...)
}
func (companyLogger) Errorf(ctx *gin.Context, format string, args ...any) {
	zlog.Errorf(ctx, format, args...)
}

// renderCompanyAgentResult 决定 HTTP 返回给调用者的 JSON 格式。
// Agent 只返回业务结果或 error；这里交给公司 base 包生成 errNo/errMsg/data。
// 它用于 HTTP 响应，命令行的最终结果仍由 CLI 输出。
func renderCompanyAgentResult(ctx *gin.Context, output any, err error) {
	if err != nil {
		base.RenderJsonFail(ctx, err)
		return
	}
	base.RenderJsonSucc(ctx, output)
}

// companyTraceWriter 专门接收 SDK 生成的执行记录，与上面的普通日志接口分开。
// SDK 负责记录节点的输入、输出、耗时和状态；这里负责把记录写进公司日志。
type companyTraceWriter struct{}

// WriteTrace 每次写入一条 Trace 事件。
// luban_trace 是搜索关键字，runId 用来归集一次运行，traceEvent 保留完整事件，
// 因此收集这些日志后仍可直接导入 sdk/trace-viewer 的离线页面。
// 实际写到文件还是终端，由 zlog 的配置和运行环境决定。
func (companyTraceWriter) WriteTrace(ctx *gin.Context, event luban.TraceEvent) {
	zlog.InfoLogger(ctx, "luban_trace",
		zap.String("runId", event.RunID),
		zap.Any("traceEvent", event),
	)
}
