package adapters

import (
	"fmt"

	"git.yukework.com/pkg/golib/v2"
	"git.yukework.com/pkg/golib/v2/base"
	serverhttp "git.yukework.com/pkg/golib/v2/server/http"
	"git.yukework.com/pkg/golib/v2/zlog"
	"github.com/gin-gonic/gin"
	"github.com/lubanAgent/luban/conf"
)

// companyHTTPSetup 准备公司的 HTTP 初始化函数，交给 server 在装配路由时调用。
// 这里配置的是 main 创建的同一个 Gin engine，不会再启动一套 HTTP 服务。
//
// 当前本地 HTTP 入口不检查登录会话；代理 IP、指标和公司监听规则仍沿用 golib。
func companyHTTPSetup(config *conf.Company) func(*gin.Engine) error {
	return func(engine *gin.Engine) error {
		if engine == nil {
			return fmt.Errorf("company adapter: Gin engine is required")
		}
		// 平台探活只需要一个轻量响应，不执行 Agent，也不打印每次探活日志。
		base.RegReadyProbe(func(ctx *gin.Context) {
			zlog.SetNoLogFlag(ctx)
			ctx.String(200, "success")
		})
		// 复用公司启动流程，统一处理访问日志、代理 IP、指标、性能分析和 GC 配置。
		golib.Bootstraps(engine, golib.BootstrapConf{
			AccessLog: config.Basic.AccessLog,
			Pprof:     config.Basic.Pprof, GCPercent: config.Basic.GCPercent,
			HandleRecovery: func(ctx *gin.Context, _ interface{}) {
				base.RenderJsonAbort(ctx, base.NewError(5000, "system internal error", ""))
			},
		})
		// 业务路由仍使用公司格式捕获 panic；当前不做 Session 校验。
		engine.Use(recoverRequest)
		return nil
	}
}

// companyHTTPServer 准备公司的服务监听函数，交给 server 在启动 HTTP 时调用。
// TCP/Unix Socket、超时、路由别名和退出流程由 golib 的 Start 处理。
// 鲁班沿用这一套监听和信号处理，main 无需再单独启动监听器。
func companyHTTPServer(config serverhttp.ServerConfig, start func(*gin.Engine, serverhttp.ServerConfig) error) func(string, *gin.Engine) error {
	return func(address string, engine *gin.Engine) error {
		current := config
		if current.Address == "" {
			// 优先使用 conf/mount/config.yaml 的 server.address。
			// 公司配置没填地址时，才采用框架传入的地址。
			current.Address = address
		}
		// golib 退出时会往这里发一次通知；当前没有单独的接收方，
		// 留一个缓冲位置，避免通知发送卡住整个退出流程。
		current.CloseChan = make(chan struct{}, 1)
		return start(engine, current)
	}
}
