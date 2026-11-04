# Luban（鲁班）公司接入版

一份 Agent 配置描述执行流程，框架负责模型调用、Tool/Skill、节点传值和 Trace。一个服务、一份 main.go，运行多个 Agent。

当前分支已接入公司 toolkit；开发需要 Go 1.25+ 和公司私有基础库权限。SDK/server 不导入公司包，也不依赖原 admissions 工程。

当前开发分支 `codex/record-sdk-trace` 已实现可选 [Trace Gorm writer](sdk/tracegorm/README.md)：宿主注入数据库连接，后台批量保存完整事件，支持失败统计和退出排空。设计见 [开发 Spec](docs/specs/record-sdk-trace.md)，本地测试见 [验证记录](docs/specs/record-sdk-trace-validation.md)。record 尚未接入，真实 MySQL/OceanBase 与正式版本下载仍待验收。

Trace 表采用 `tblLubanTraceRun` 和 `tblLubanTraceSpan00`～`tblLubanTraceSpan19`；Span 按完整 runId 的 CRC32 IEEE 值取模 20，写入与查询共用 `tracegorm.SpanTable(runID)`。同次 Run 的事件落在同一张表，全部建表 SQL 仍需宿主显式执行。

**空模型输入跳过：** 顺序 flow 中，`Request{Output: luban.Output[T]()}` 的 `Messages` 为空时，模型节点不请求模型，交给下一函数类型为 T 的零值；`Batch{Output: luban.Output[T]()}` 的 `Items` 为空时交付 `BatchResult[T]` 的空 Items。两者均继续后续节点，在 Trace 中标记 `skipped` / `empty_input`。不声明有效 Output 的空输入报错；单个 Batch 项消息为空不会跳过整批。详见[验证记录](docs/specs/empty-input-skip-validation.md)。

**结构化输出的工具选择：** 模型调用只提供 `__runtime_output` 时，SDK 在 `ModelRequest.ToolChoice` 中要求调用工具；同时存在业务 Tool 或 Skill 时仍由模型自行选择。OpenAI-compatible 请求分别发送 `required` / `auto`，Anthropic 的必选模式发送 `{"type":"any"}`。模型若仍只返回正文而未调用 OutputTool，SDK 会保留原有的未提交 Output 错误。

record 研发的配置、初始化、模型适配、Run 和 Trace 接入步骤，单独见 [record 接入 Luban SDK 指南](docs/integration/record-sdk.md)。框架开发读 Spec，业务接入读指南。

## 三步运行第一个 Agent

1. 填模型连接。`conf/luban.yaml` 是地址、Key、协议和项目路由的唯一配置位置：

   ```sh
   export LUBAN_gateway_BASE_URL="https://your-gateway/gateway/rp/v1"
   export LUBAN_gateway_KEY="your-key"
   ```

2. 查看或修改 `agents/summary/agent.json`，将 `modelName` 填为网关真实支持的模型名。
3. 在根目录执行：

   ```sh
   go run . run summary --input '{"userPrompt":"会议结论：先完成迁移，再安排联调。"}'
   ```

CLI 最终结果写 stdout，普通日志和 Trace 统一走公司 logkit，输出位置由 `conf/mount/config.yaml` 的日志配置决定；默认本地写入 `log/luban.log`。CLI 不需要业务数据库。增加纯配置 Agent 只需增加 `agents/<名字>/agent.json`，复用同一项目连接，无需改 main 或登记 Go 包。含 Go 函数/Tool 的 Agent 才需要在 `agents/imports.go` 导入对应包。

HTTP 使用 `go run .` 启动。当前不检查登录会话，直接访问 `POST /luban/ai/agent/chat`。

## 启动与边界

```text
识别命令 → adapters 装配公司资源 → server 加载所有 Agent / Skill / 模型连接
  ├─ CLI：解码输入 → Runtime.Run → 结果 / Trace
  └─ HTTP：公司探针与认证 → 自动 Agent 路由 → 按需启动业务 Worker → 监听
```

| 目录 | 内容 |
| --- | --- |
| `agents/` | 每个 Agent 的配置、函数、Tool、Skill、领域规则和专属 API；`imports.go` 聚合 Go 包 |
| `adapters/` | 公司日志、模型 HTTP 传输、Gorm 初始化、Renderer 和监听；不包含 Agent 业务 |
| `conf/` | `luban.yaml` 管模型连接/服务入口；`mount/` 管公司资源 |
| `models/` | 数据库连接、表结构与业务读写 |
| `router/` | 通用 HTTP 路由；与 SDK 使用同一套输入解析 |
| `server/` | 资源装配、配置/Skill 加载、自动 HTTP/CLI 与服务生命周期 |
| `sdk/` | Runtime、Eino 编排、Tool/Skill、节点传值、Trace 与入口适配器 |

公司模型 HTTP 通过 `adapters/model_http.go` 使用 toolkit；消息、媒体、Tool、用量和 OpenAI/Anthropic 协议转换复用 SDK。模型名、地址与 Key 均不写死在适配代码中。通用客户端可通过 `Options.Resources` 注入。

## 志愿填报案例

`agents/admissions/` 保留一个 Agent、四个 Tool、六份 Skill。默认仅加载配置，**不启动 Worker、不要求其数据库、不暴露同步 HTTP**。

需要启用异步业务时，配置 `mysql.admissions`，核验业务表，并在 `conf/luban.yaml` 设置 `backgroundAgents: [admissions]`。这会挂载 `POST /admissions/mis/strategy/conversation/message`：HTTP 写任务并返回 taskId，Worker 后续执行同一个 Agent。数据库本身不会隐式启用路由。Worker 退出后才关闭连接；目标库不自动建表。

## 验证与部署

`make check` 检查格式、依赖、普通/race 测试、vet 和构建。测试包括真实根二进制 CLI、新增纯 JSON Agent、公司 HTTP 传输与项目路由、统一 HTTP 输入、显式业务启用及退出。仅使用 fake 网关、httptest 和临时 SQLite；真实业务数据库、模型效果与容器仍待联调。

Trace 搜索 `luban_trace` / `runId`，收集后导入 [离线查看页](sdk/trace-viewer/index.html)。根服务仍默认只写日志；SDK 使用方可显式注入 `tracegorm.Writer`，数据库导出的原始事件也能由该页面解析。离线体验：`go run ./sdk/examples/trace-gorm`，使用假模型与临时 SQLite。

```sh
docker build --secret id=netrc,src="$HOME/.netrc" -t luban-example .
```

镜像运行时挂载自身 `conf/mount` 并注入环境变量，无需相邻业务工程。构建需私有基础库凭据。
