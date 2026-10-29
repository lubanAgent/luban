# Luban（鲁班）公司接入版

一份 Agent 配置描述执行流程，框架负责模型调用、Tool/Skill、节点传值和 Trace。一个服务、一份 main.go，运行多个 Agent。

当前分支已接入公司 golib；开发需要 Go 1.25+ 和公司私有基础库权限。SDK/server 不导入公司包，也不依赖原 admissions 工程。

## 三步运行第一个 Agent

1. 填模型连接。`conf/luban.yaml` 是地址、Key、协议和项目路由的唯一配置位置：

   ```sh
   export LUBAN_OPENPROXY_BASE_URL="https://your-gateway/openproxy/rp/v1"
   export LUBAN_OPENPROXY_KEY="your-key"
   ```

2. 查看或修改 `agents/summary/agent.json`，将 `modelName` 填为网关真实支持的模型名。
3. 在根目录执行：

   ```sh
   go run . run summary --input '{"userPrompt":"会议结论：先完成迁移，再安排联调。"}'
   ```

CLI 最终结果写 stdout，普通日志和 Trace 统一走公司 zlog，输出位置由 `conf/mount/config.yaml` 的日志配置决定；默认本地写入 `log/luban.log`。CLI 不需要业务数据库。增加纯配置 Agent 只需增加 `agents/<名字>/agent.json`，复用同一项目连接，无需改 main 或登记 Go 包。含 Go 函数/Tool 的 Agent 才需要在 `agents/imports.go` 导入对应包。

HTTP 使用 `go run .` 启动。当前不检查登录会话，直接访问 `POST /luban/ai/agent/chat`。完整文本、图片、视频与多项目例子见[快速接入](docs/快速接入.md)。

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

公司模型 HTTP 通过 `adapters/model_http.go` 使用 golib；消息、媒体、Tool、用量和 OpenAI/Anthropic 协议转换复用 SDK。模型名、地址与 Key 均不写死在适配代码中。通用客户端可通过 `Options.Resources` 注入，详见[公司适配说明](docs/XXSY适配版.md)。

## 志愿填报案例

`agents/admissions/` 保留一个 Agent、四个 Tool、六份 Skill。默认仅加载配置，**不启动 Worker、不要求其数据库、不暴露同步 HTTP**。

需要启用异步业务时，配置 `mysql.admissions`，核验业务表，并在 `conf/luban.yaml` 设置 `backgroundAgents: [admissions]`。这会挂载 `POST /admissions/mis/strategy/conversation/message`：HTTP 写任务并返回 taskId，Worker 后续执行同一个 Agent。数据库本身不会隐式启用路由。Worker 退出后才关闭连接；目标库不自动建表。

## 验证与部署

`make check` 检查格式、依赖、普通/race 测试、vet 和构建。测试包括真实根二进制 CLI、新增纯 JSON Agent、公司 HTTP 传输与项目路由、统一 HTTP 输入、显式业务启用及退出。仅使用 fake 网关、httptest 和临时 SQLite；真实业务数据库、模型效果与容器仍待联调。

Trace 搜索 `luban_trace` / `runId`，收集后导入 [离线查看页](sdk/trace-viewer/index.html)，本期不落库。

```sh
docker build --secret id=netrc,src="$HOME/.netrc" -t luban-xxsy .
```

镜像运行时挂载自身 `conf/mount` 并注入环境变量，无需相邻业务工程。构建需私有基础库凭据。详见[开发进度](docs/开发进度.md)。
