package main

import (
	"context"
	"log"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/lubanAgent/luban/adapters"
	"github.com/lubanAgent/luban/agents"
	"github.com/lubanAgent/luban/server"
)

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	// 先识别命令。看帮助或参数写错时，不读连接配置，也不连接数据库。
	command, err := server.ParseCommand(args)
	if err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n" + server.Usage + "\n")
		return 2
	}
	if command.Help {
		_, _ = os.Stdout.WriteString(server.Usage + "\n")
		return 0
	}
	gin.SetMode(gin.ReleaseMode)
	options := server.Options{Assets: os.DirFS(".")}
	// CLI 和 HTTP 的普通日志、Trace 都由公司适配器统一注入。
	// 装配公司 HTTP 和按需配置的 Gorm；HTTP 模式再装配 IPS。
	// Agent 文件和模型连接由 server 统一加载，这里不执行 Agent。
	closeHost, err := adapters.Configure(&options, command)
	if err != nil {
		log.Printf("configure host: %v", err)
		return 1
	}
	defer closeHost()
	app, err := server.New(options)
	if err != nil {
		log.Printf("start Luban: %v", err)
		return 1
	}
	defer app.Close()
	if !command.IsHTTP() {
		return app.RunCommand(command)
	}
	engine := gin.New()
	// 公司实现先装探针、指标和代理 IP 规则，再为后续业务接口加登录校验。
	// 不注入时，server 使用自己的默认 HTTP 基础能力。
	if err := app.SetupHTTP(engine); err != nil {
		log.Printf("setup HTTP: %v", err)
		return 1
	}
	if err := app.RegisterHTTP(engine); err != nil {
		log.Printf("register HTTP: %v", err)
		return 1
	}
	// 不在 main 监听系统信号：由最终选中的 HTTP server 统一处理。
	stopping, cancel := context.WithCancel(context.Background())
	defer cancel()
	// 消息/确认接口只提交任务；后台 Worker 取到任务后执行同一个 Agent。
	if err := agents.Mount(stopping, engine, app); err != nil {
		log.Printf("mount business: %v", err)
		return 1
	}
	if err := app.Serve(engine); err != nil {
		log.Printf("HTTP service: %v", err)
		return 1
	}
	return 0
}
