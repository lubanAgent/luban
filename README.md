# Luban（鲁班）

一份 Agent 配置描述执行流程，框架负责模型调用、Tool/Skill、节点传值、HTTP/MQ/Cron 入口和 Trace。一个服务、一份 `main.go`，运行多个 Agent。

主 module `github.com/lubanAgent/luban` 只依赖标准库和公开开源包。

## 三步运行第一个 Agent

1. 配置公开模型网关。`conf/luban.yaml` 只保存环境变量引用，不提交真实 Key：

   ```sh
   export LUBAN_LLM_BASE_URL="https://your-gateway/v1"
   export LUBAN_LLM_API_KEY="your-key"
   ```

2. 查看或修改 `agents/summary/agent.json`，将 `modelName` 填为网关真实支持的模型名。
3. 在仓库根目录执行：

   ```sh
   go run . run summary --input '{"userPrompt":"会议结论：先完成迁移，再安排联调。"}'
   ```

CLI 的机器可读结果写 stdout；普通日志和 Trace 写 stderr。HTTP 使用 `go run .` 启动，默认入口为 `POST /luban/ai/agent/<agentKey>`。

增加纯配置 Agent 只需增加 `agents/<名字>/agent.json`。含 Go 函数或 Tool 的 Agent 才需要登记对应包。

## 框架能力

- `sdk/`：Runtime、Eino 编排、模型协议、Tool/Skill、节点传值、Trace 和 MQ/HTTP/Cron 入口适配。
- `server/`：配置与 Skill 加载、模型连接选择、HTTP/CLI 装配、默认监听和服务生命周期。
- `agents/`：Agent 配置、业务函数、Tool、Skill、领域规则和示例。
- `models/`、`router/`：业务数据库与通用路由；核心 SDK 不依赖这些包。
- `sdk/tracegorm/`：可选 Gorm Trace writer、Run 表与 20 张 Span 分表。

Trace 表采用 `tblLubanTraceRun` 和 `tblLubanTraceSpan0`～`tblLubanTraceSpan19`。Span 按完整 runId 的 CRC32 IEEE 值取模 20，编号从 0 开始且不补零；建表 SQL 见 [sql/trace/001_create_tables.mysql.sql](sql/trace/001_create_tables.mysql.sql)。

空模型输入会跳过当前模型节点并继续后续业务节点；只有 `__runtime_output` 时，SDK 要求模型必须通过 OutputTool 提交结构化结果。详细契约见 [Trace Spec](docs/specs/record-sdk-trace.md)、[空输入验证](docs/specs/empty-input-skip-validation.md) 和 [record SDK 接入指南](docs/integration/record-sdk.md)。

## 目录与边界

```text
main.go                 通用启动入口，仅使用公开依赖
agents/<name>/          Agent 配置、函数、Tool、Skill
sdk/                    Runtime 与 SDK
server/                 通用服务和默认实现
models/                 业务数据库模型
router/                 通用业务路由
conf/luban.yaml         公开服务、模型连接和服务配置
```

根模块启动时不创建数据库、不建表、不 Ping 模型网关。数据库、模型 HTTP、日志、Renderer、TraceWriter 和服务监听都可以由宿主注入。

模型连接和其他密钥必须通过部署环境注入。`conf/luban.yaml` 中只允许 `${ENV_NAME}` 引用，不允许提交字面凭据。

`make check` 会扫描常见密钥字段。若不慎提交凭据，请立即按 [SECURITY.md](SECURITY.md) 轮换并处理历史记录。

## 验证与部署

```sh
make check
docker build -t luban .
```

`make check` 覆盖格式、module tidy、普通测试、race、vet 和 build。

离线 Trace 查看页位于 [sdk/trace-viewer/index.html](sdk/trace-viewer/index.html)，可用 `go run ./sdk/examples/trace-gorm` 生成假数据和临时 SQLite 数据。
