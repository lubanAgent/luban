// Package adapters 负责把公司的基础能力接到鲁班上。
//
// 阅读入口是 Configure：main 调用它，把日志、HTTP 客户端、数据库等填进
// server.Options；随后 server.New 使用这些实现，执行 Agent 时就会走公司的能力。
// 本包负责基础设施接入，Agent 的业务函数、Tool 和 Skill 放在 agents 目录。
package adapters

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"git.yukework.com/pkg/golib/v2/base"
	"git.yukework.com/pkg/golib/v2/env"
	serverhttp "git.yukework.com/pkg/golib/v2/server/http"
	golibsignal "git.yukework.com/pkg/golib/v2/server/signal"
	"git.yukework.com/pkg/golib/v2/zlog"
	"github.com/lubanAgent/luban/conf"
	"github.com/lubanAgent/luban/server"
	"gorm.io/gorm"
)

// Configure 在服务启动时执行一次，为整个服务装配公司基础设施。
//
// options 可以理解为“框架需要使用哪些实现”的清单。给其中的字段赋值，
// 就是把公司的实现交给框架，后续各 Agent 共用这些资源。
// 调用方已经填好的字段会被保留，没有填写的才由这里补上公司实现。
//
// 返回的 closeHost 由 main 在退出时调用，负责关闭这里创建的连接并刷新日志。
// 此处不执行 Agent，也不启动业务后台任务；这些工作由后续入口负责。
func Configure(options *server.Options, command server.Command) (closeHost func(), err error) {
	if options == nil {
		return nil, fmt.Errorf("company adapter: server options must not be nil")
	}
	// 1. 读取本工程 conf/mount 下的公司配置。
	configuration, err := conf.LoadCompany(".")
	if err != nil {
		return nil, err
	}
	root, err := filepath.Abs(".")
	if err != nil {
		return nil, err
	}
	// 告诉 golib 当前服务的根目录和模块名，供日志路径、module 字段等使用。
	env.SetRootPath(root)
	env.SetAppName("luban")
	// 2. 记录“本次装配创建的资源”，退出时只关闭这些资源。
	// 调用方传入的数据库或 HTTP 客户端仍由调用方管理。
	owned := make(map[string]*gorm.DB)
	var ownedHTTP *modelHTTP
	logging := false
	var once sync.Once
	cleanup := func() {
		// 初始化失败和正常退出都可能触发清理，保证实际只清理一次。
		once.Do(func() {
			if ownedHTTP != nil {
				if transport, ok := ownedHTTP.transport.(interface{ CloseIdleConnections() }); ok {
					transport.CloseIdleConnections()
				}
			}
			for _, db := range owned {
				if sqlDB, dbErr := db.DB(); dbErr == nil {
					_ = sqlDB.Close()
				}
			}
			if logging {
				// 日志最后关闭，确保前面的资源收尾日志也能写完。
				zlog.CloseLogger()
			}
		})
	}
	// 公司基础库初始化时如果 panic，转成启动错误；之前已创建的资源也要释放。
	defer func() {
		if recover() != nil {
			err = fmt.Errorf("initialize company resources failed; check this service's configuration")
			closeHost = nil
		}
		if err != nil {
			cleanup()
		}
	}()
	// 3. 初始化公司日志。下面注入的普通日志和 Trace 都使用这套 zlog 配置。
	logging = true
	zlog.InitLog(configuration.Basic.Log)
	if options.Assets == nil {
		options.Assets = os.DirFS(".")
	}
	if options.Logger == nil {
		options.Logger = companyLogger{}
	}
	// 4. 模型请求由 SDK 组装，这里替换实际发送 HTTP 的实现。
	// 模型名、地址、Key 仍来自 Agent 配置和 conf/luban.yaml，不写死在这里。
	if options.HTTPDoer == nil {
		ownedHTTP = newModelHTTP(configuration.API.ModelHTTP)
		options.HTTPDoer = ownedHTTP
	}
	// 5. 按名字整理数据库连接，例如 admissions、school。
	// 先复制调用方已有的连接，避免改动原 map；配置中缺少的连接才在这里创建。
	databases := make(map[string]*gorm.DB, len(options.Databases))
	for name, db := range options.Databases {
		databases[name] = db
	}
	for name, config := range configuration.Resource.Mysql {
		if _, injected := databases[name]; injected {
			// 同名连接优先使用调用方注入的版本，不再重复连接数据库。
			continue
		}
		db, dbErr := base.InitMysqlClient(config)
		if dbErr != nil {
			return nil, fmt.Errorf("initialize database %q failed; check conf/mount/resource.yaml", name)
		}
		owned[name], databases[name] = db, db
	}
	options.Databases = databases
	// 6. HTTP 执行完 Agent 后，使用公司的成功/失败响应格式。
	if options.Renderer == nil {
		options.Renderer = renderCompanyAgentResult
	}
	// 普通日志和 Trace 是两个独立接口，都要注入。
	// CLI 与 HTTP 共用这个 TraceWriter，不在入口单独覆盖它。
	if options.TraceWriter == nil {
		options.TraceWriter = companyTraceWriter{}
	}
	// 7. 只有启动 Web 服务才装配 HTTP 基础能力、监听和退出回调。
	// 命令行执行一次 Agent 后直接返回，不监听端口。
	if command.IsHTTP() {
		if options.SetupHTTP == nil {
			options.SetupHTTP = companyHTTPSetup(configuration)
		}
		if options.ServeHTTP == nil {
			options.ServeHTTP = companyHTTPServer(configuration.Basic.Server, serverhttp.Start)
		}
		if options.RegisterShutdown == nil {
			// 把框架登记的停止函数接到公司的退出流程。
			// 比如先停止业务 Worker，等任务收尾后再由 main 关闭数据库。
			options.RegisterShutdown = func(name string, stop func()) {
				golibsignal.RegisterShutdown(name, func(context.Context) error { stop(); return nil })
			}
		}
	}
	return cleanup, nil
}
