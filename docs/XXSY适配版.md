# XXSY 适配版

Luban 是独立公司工程，根目录一份 `go.mod` / `main.go`，直接依赖 golib、IPS 等基础库。原 admissions 工程仅作参考，不参与编译和启动。

```text
main 识别命令
  → adapters.Configure：公司日志、模型 HTTP、按需 Gorm、HTTP 模式的 IPS
  → server.New：加载全部 Agent/Skill，按项目与 modelList 选择模型连接
  → CLI 执行；或 HTTP 基础设施 → Agent 路由 → agents.Mount → 公司监听
  → App.Close 停 Worker → 关框架资源 → adapters cleanup 关公司资源
```

## 配置放哪里

| 文件 | 内容 |
| --- | --- |
| `conf/luban.yaml` | 模型项目、baseUrl/apiKey/protocol/modelList、HTTP 路由、后台 Agent 启用 |
| `conf/mount/api.yaml` | `modelHTTP` 的 timeout/connectTimeout/retry，不再重复填写地址或 Key |
| `conf/mount/resource.yaml` | 按名称配置业务 MySQL；默认空，文件也可省略 |
| `conf/mount/custom.yaml` | IPS 登录配置；CLI 不加载 |
| `conf/mount/config.yaml` | golib 日志、AccessLog、server 超时/别名/地址、pprof、GC |

模型连接只有一处配置：

```yaml
llm:
  admissions:                       # 项目 Key，可供多个 Agent 共用
    - baseUrl: ${LUBAN_OPENPROXY_BASE_URL}
      apiKey: ${LUBAN_OPENPROXY_KEY}
    - modelList: [your-claude-model]
      protocol: anthropic
      baseUrl: ${CLAUDE_BASE_URL}
      apiKey: ${CLAUDE_KEY}
```

`baseUrl` 是完整协议前缀，例如 `https://gateway.example.com/openproxy/rp/v1`，也支持完整接口地址。SDK 补 OpenAI 的 `/chat/completions` 或 Anthropic 的 `/messages` 路径。先精确匹配 modelList，再走项目默认连接；单项目 Agent 可省 projectKey，多项目必须显式填写。各项目可使用相同模型名、不同 Key。

公司适配实现 `HTTPDoer`，经 golib HttpStream 发送 SDK 已编码的请求。协议、模型名、Tool、媒体和用量解析由 SDK 处理；共享连接池，每次请求独立认证，保留 Run 取消和总超时，响应由 SDK 限量读取并关闭。高级接入仍可用 `ProjectModels` 覆盖单个模型，或用 `ProjectDefaults` 覆盖项目默认连接；配置中的专用连接优先于注入的项目默认，单模型注入优先级最高。

## 默认不开业务数据库

纯配置 Agent 和通话摘要不需要业务库。需要 admissions 异步任务时，才补：

```yaml
# conf/mount/resource.yaml
mysql:
  admissions:
    service: luban-admissions
    addr: ${LUBAN_DB_ADDR}
    database: ${LUBAN_DB_NAME}
    user: ${LUBAN_DB_USER}
    password: ${LUBAN_DB_PASSWORD}
    charset: utf8mb4
    maxidleconns: 5
    maxopenconns: 20
    readTimeOut: 10s
    writeTimeOut: 10s
```

```yaml
# conf/luban.yaml，保留原有 llm 等配置
http:
  excludeAgents: [admissions]
backgroundAgents: [admissions]
```

`agents/mount.go` 是特殊业务装配入口，调用 `agents/admissions/worker.Mount` 创建仓库、核验 schema、登记消息/确认接口并启动任务。普通 Agent 无需写挂载代码。只注入 DB 不会公开业务接口；缺 DB 或 schema 时，显式启用的 Worker 报启动错误。不自动建表。

启用后接口为 `POST /admissions/mis/strategy/conversation/message`。HTTP 提交持久任务，受信任 Worker 领取后执行完整 Agent。admissions 不接受公开同步 HTTP，也不能通过 CLI 伪造任务。结果查询/发布不在当前范围。

## 公司启动与退出

HTTP 仍须配置 `LUBAN_IPS_DOMAIN`、`LUBAN_IPS_APP_ID`、`LUBAN_IPS_APP_SECRET` 并通过登录；不静默关闭 IPS。`SetupHTTP` 复用 golib.Bootstraps，先登记 `/ready`、`/runtime-metrics`，再安装业务登录校验。代理 IP、Recovery、GC 和性能分析沿公司规则。

`ServeHTTP` 复用 serverhttp.Start，沿用 TCP/Unix Socket、超时、路由别名和信号处理；公司配置 server.address 优先。`RegisterShutdown` 把 `App.OnShutdown` 的业务停止回调加入同一退出机制，App.Close 也调用同一回调，确保 Worker 停止后才关数据库。main 不重复监听信号。

成功/失败响应使用 RenderJsonSucc/Fail；日志包含 Debugf/Infof/Warnf/Errorf。CLI 和 HTTP 的 Trace 均写 zlog 的 `luban_trace`，包含 runId、traceEvent，不建表。输出位置沿用 `conf/mount/config.yaml` 的日志配置，默认本地写入 `log/luban.log`；CLI 最终结果仍写 stdout。

## 注入其他客户端

在 adapters 初始化 Redis/COS 等客户端，通过 `options.Resources` 注册：

```go
options.Resources = map[string]any{"redis": redisClient, "cos": cosClient}
```

节点/Tool 使用 `luban.Resource[*redis.Client](ctx, "redis")` 取原类型客户端。注册表启动时复制，服务内复用；调用者保证并发安全并在 adapter cleanup 中关闭。Gorm 继续通过 `Options.Databases` 注入，SDK 不关闭宿主连接。普通业务外部 API 放自己的 Agent 目录，公司适配不包含业务规则。

验证使用 fake 网关/IPS、httptest、真实根二进制和临时 SQLite。真实 IPS、目标业务库、完整模型效果与容器部署尚待联调。
