# record 接入 Luban SDK 指南

- 日期：2026-11-02
- 读者：record 业务设计、开发和联调人员。
- 状态：**record 接入方案，尚未修改 record。Luban 的数据库 Trace 已实现并通过本地验证；真实目标库和正式依赖版本仍待验收。**

**record 加载配置，启动时创建并登记 Runtime，业务执行时调用 `rt.Run`。Trace 由 SDK 采集，交给 record 的日志和 trace_store 数据库。**

本文只讲 record 要准备什么、在哪里接入、怎样验收。存储包内部的队列、重试、表结构和开发任务统一维护在 [Luban 开发 Spec](../specs/record-sdk-trace.md)，不在这里重复设计。

## 1. 接入后的整体流程

```text
record 启动
  配置、日志、公司 HTTP、数据库初始化
    → 创建模型调用器和 TraceWriter
    → luban.New → rt.Register → 保存 Runtime

record 原有 HTTP / MQ / cron 入口
  解析本次业务参数
    → rt.Run(ctx, agentKey, input)
        → 准备材料 → 模型 / Tool / Skill → 校验、保存结果
        → 每个完成事件写日志，并交数据库 writer 入队
    → 原入口处理 output / error

record 退出
  停止接收 → 等待在途 Run → 排空 Trace → 关闭数据库 → 关闭日志
```

| record 负责 | Luban 负责 |
| --- | --- |
| 原路由、响应协议、MQ 重试、任务调度 | 执行一份 Agent 配置中的 flow |
| 取数、业务校验、事务、副作用与幂等 | 节点传值、模型循环、Tool/Skill、结构化结果 |
| 模型连接、凭据、公司 HTTP 传输 | OpenAI / Anthropic 协议编码与响应解析 |
| logkit、trace_store 连接、建表和退出挂载 | 生成 Trace；新增可选 Gorm writer 批量写库 |

无需启动 Luban 根 Web 服务，也不用将 record 的业务搬到 Luban 仓库。下文 `recordCase` 只是接入示例，不代表已经确定了业务流程。

## 2. record 的业务设计先补齐这些内容

在本需求详细设计中增加 **“Luban SDK 接入设计”** 一节即可，不必再写一套框架设计。

| 必须说明 | 具体要写什么 |
| --- | --- |
| 一次 Run 做什么 | 哪个入口触发、从哪里开始、在哪里结束；HTTP 同步执行还是现有任务执行 |
| 输入和输出 | 输入 DTO、结果 DTO、成功样例；哪些业务 ID 必须关联 |
| Agent 配置 | Agent Key、projectKey、flow 顺序、模型名、提示词、轮次与温度要求 |
| 业务函数 | 每个函数的签名、前后节点类型、读写哪些数据、何时保存结果 |
| Tool / Skill | 名称、用途、参数和结果；没有就明确不需要，Tool 的副作用与幂等由谁保障 |
| 模型特殊要求 | 图片、视频、fps、流式或其他参数是否实际使用；网关是否支持 Tool Calling |
| 失败和重试 | 业务失败怎么返回、MQ/任务何时重试、重复执行怎样避免重复写入 |
| Trace 与验收 | BusinessID/requestId 来源、预计流量与保留周期、成功及失败 Case |

Trace 的 BusinessID 可以选任务 ID、报告 ID 等实际业务标识，由 record 定义。每次 Run 都有独立 runId；业务重试不复用前一次 runId。

业务方案未定，不影响 Luban 先开发通用存储包；但不能据此宣称具体业务已经完成接入。

## 3. 依赖与文件放在哪里

依赖根 module `github.com/lubanAgent/luban`，代码导入：

```go
import luban "github.com/lubanAgent/luban/sdk"
```

这是同一个 module 中的 SDK 子包。record 保留自己的 Gin replace；Go 声明需从 1.24.0 调整到至少 1.25，Docker builder 已使用 1.25。

本地调试的 replace 指向 **Luban 仓库根目录**。正式接入固定交付提交对应的版本，不提交本机绝对路径，也不直接追随开发分支。当前远端依赖下载尚未通过，开发机和 CI 的代理/checksum 访问需要打通。

以下是拟接入位置；新文件和方法尚未在 record 创建。

| record 位置 | 要做的事 |
| --- | --- |
| `conf/mount/luban_agents.json`、`conf/config.go` | JSON 平铺在 mount 下，由现有 `env.LoadConf` 读取到 `conf.LubanAgents` |
| `api/llm/luban.go` | 实现公司 HTTPDoer，构造 SDK 协议调用器 |
| `router/airuntime.go` | InitAIRuntime：New、Register、注入模型和 Trace，成功后发布 Runtime |
| `helpers/luban.go` | 保存/获取已初始化的 Runtime；不导入业务 service |
| `service/<本需求>/` | 业务输入输出、准备和保存函数、按需实现 Tool/Skill |
| `helpers/mysql.go`、`conf/mount/resource.yaml` | 按需增加 trace_store 基础连接 |
| 原 HTTP/MQ/cron 入口和退出位置 | 调用 Run，接入停止等待和 Trace 排空 |

依赖方向保持 `router → service → helpers`，避免在 helpers 中导入 service 注册函数形成循环。SDK 显式 Register 不要求 `agents/` 目录或自动扫描。

## 4. 从配置到一次 Run

### 4.1 配置：描述节点顺序

`luban_agents.json` 最外层直接是 Agent Key，**没有 `agents` 包装层**：

```json
{
  "recordCase": {
    "projectKey": "record",
    "flow": [
      {"func": "PrepareRecordCase", "description": "准备模型输入"},
      {
        "modelName": "<本需求使用的真实模型名>",
        "systemPrompt": "根据输入生成摘要，并提交结构化结果。",
        "temperature": 0.2,
        "maxLoopCount": 5
      },
      {"func": "FinishRecordCase", "description": "处理模型结果"}
    ]
  }
}
```

`recordCase` 是 Run 使用的 Key，`record` 是模型连接的项目 Key。模型名只写在配置中；温度可省略，省略时不传。业务需要 Tool/Skill 时，再在模型节点增加 `tools` / `skills` 名单并登记对应实现。

### 4.2 模型：SDK 处理协议，record 提供公司 HTTP

```go
// endpoint、apiKey 从本需求对应的现有 conf.API 配置取得。
// companyHTTP 是 record 新增的 HTTPDoer 实现，不是现有 CallLLM。
model, err := luban.NewProtocolInvoker(luban.ModelConnection{
    BaseURL: endpoint,
    APIKey:  apiKey,
    Protocol: "openai",
}, companyHTTP)
if err != nil {
    return err
}
```

适配的标准只有一个方法：

```go
type HTTPDoer interface {
    Do(*http.Request) (*http.Response, error)
}
```

调用过程：**SDK 生成完整请求 → companyHTTP 经 toolkit 发送 → SDK 解析正文、ToolCall 和用量。**

record 的现有 `CallLLM` 会重组消息、固定温度，且不承接完整 tools/tool_calls，不能直接包装后用于这条 Agent 链路。原方法继续服务未迁移业务。

接入实现注意四件事：

- 沿用对应客户端的 Domain 和 URLPath，保留完整 `/gateway/rp/v1/chat/completions`，不能只传域名。
- 从实际配置映射请求/连接/响应头超时、路径超时、重试、重试状态码、连接池及代理设置；SDK 不额外叠加传输重试。
- toolkit 会重新创建请求 Context，适配需要保留原取消链；响应体读完并关闭后才释放超时 Context。
- 可参考 [公司 HTTP 传输实现](../../adapters/model_http.go)，在 record 本地适配，不能直接导入整个 Luban adapters 包。

同一网关可处理多个模型名。若不同业务使用不同凭据，分别创建调用器，按 projectKey 注入 `ProjectDefaults`，不要在适配代码中逐个硬编码模型名。

### 4.3 函数：输入和返回值按相邻节点传递

以下是演示 DTO 和函数，正式实现替换成本需求类型：

```go
type RecordInput struct {
    Text string `json:"text"`
}

type RecordResult struct {
    Summary string `json:"summary"`
}

func PrepareRecordCase(ctx *gin.Context, input RecordInput) (luban.Request, error) {
    return luban.Request{
        Messages: []luban.Message{{
            Role: luban.RoleUser,
            Content: []luban.Part{{Type: luban.PartText, Text: input.Text}},
        }},
        Output: luban.Output[RecordResult](),
    }, nil
}

func FinishRecordCase(ctx *gin.Context, result RecordResult) (RecordResult, error) {
    // 正式业务在这里做领域校验、按需保存；失败返回 error。
    return result, nil
}
```

类型衔接为 `RecordInput → Request → RecordResult → RecordResult`。Output 明确模型应提交的结构体类型，下游直接收该类型。

每个业务函数必须带 `*gin.Context` 首参，除此之外最多一个业务输入、一个业务输出和末尾 error。中间节点不自动做 JSON 转换或结构体/指针转换。末节点只有 error 时，成功的 Run 返回空 map；需要返回业务数据就让末节点明确返回。

图片、视频在 Request.Messages.Content 中用 PartImage/PartVideo 和 URL 提供。当前标准 Part 没有视频 fps，真实需求若依赖它，需要双方补充契约，不能当作已经支持。

### 4.4 初始化：一个进程创建一次

下面是 InitAIRuntime 内的核心片段，`model` 来自 4.2，函数引用替换为实际 service 包：

```go
rt, err := luban.New(luban.Options{
    Config: conf.LubanAgents,
    ProjectDefaults: map[string]luban.ModelInvoker{
        "record": model,
    },
    TraceWriter: traceWriter,
})
if err != nil {
    return err
}
if err := rt.Register(map[string]any{
    "PrepareRecordCase": PrepareRecordCase,
    "FinishRecordCase":  FinishRecordCase,
}, nil, nil); err != nil {
    return err
}
// New、Register 全部成功后，由 record 的 helpers 保存 rt。
```

Register 的最后两项分别是 Tool 和 Skill；本示例不需要，传 nil。需要时分别传 `[]luban.Tool` 和 `[]luban.Skill`。Skill 正文可由 record 的代码变量或启动加载结果提供。

Runtime 发布后复用，支持并发 Run；Register 只在启动时调用一次。若已创建数据库 writer，后续初始化失败还必须关闭 writer，不能只 return 而遗留后台任务。

### 4.5 执行：业务入口直接调用

```go
output, err := rt.Run(ctx, "recordCase", RecordInput{Text: text})
runID := luban.RunID(ctx) // Run 返回后仍可取得，方便关联业务日志。
```

| 入口 | record 怎样处理 |
| --- | --- |
| HTTP | 原 controller 解析业务参数后调用 Run，再按现有 RenderJsonSucc/Fail 和业务错误码处理 |
| MQ | 使用原消息/任务参数调用 Run；error 交回原消费框架处理重试 |
| cron | 进程初始化一次 Runtime，每条任务调用 Run；业务控制批次、并发、重试和结果保存 |

直接 Run 接收 Go 值，不会自动把 HTTP Body 解码成任意业务结构体。无 HTTP Request 的任务可以用 `luban.BindContext(ctx, taskCtx)` 绑定取消信号，无需伪造 HTTP 请求；必须处理绑定错误。

开始 Run 前初始化公司日志 ID。SDK 使用 Gin 公开能力，不补齐公司 CustomContext；不要将请求 Context 留给自建后台 goroutine 长期使用。

## 5. Trace 怎样接日志和 trace_store

### 5.1 先接已有的日志能力

record 实现并注入已有 `luban.TraceWriter` 接口。下列写法参考当前公司适配：

```go
type recordTraceWriter struct {
    database luban.TraceWriter // 不启用落库时为 nil；启用后放入 Gorm writer。
}

func (w recordTraceWriter) WriteTrace(ctx *gin.Context, event luban.TraceEvent) {
    logkit.InfoLogger(ctx, "luban_trace",
        zap.String("runId", event.RunID),
        zap.Any("traceEvent", event),
    )
    if w.database != nil {
        w.database.WriteTrace(ctx, event)
    }
}
```

SDK 负责生成节点输入、输出、耗时、状态等事件；record 负责输出到自己的日志。搜索 `luban_trace` 和 runId 即可归集，继续使用 [离线查看页](../../sdk/trace-viewer/index.html)。只 import SDK 不会自动启用根服务的公司日志适配。

### 5.2 接入数据库 writer

以下 API 已在 `codex/record-sdk-trace` 实现，使用方式见 [tracegorm README](../../sdk/tracegorm/README.md)。正式接入前仍需固定可下载版本、核验目标数据库并执行建表：

```go
writer, err := tracegorm.New(startupCtx, trace_storeDB, tracegorm.Options{
    Service: "record",
})
if err != nil {
    return err
}
traceWriter := recordTraceWriter{database: writer}
// 将 traceWriter 交给 4.4 的 luban.Options.TraceWriter。
// record 保存 writer，用于退出时调用 Close。
```

正式接入可通过 Options.Metadata 从 Gin Context 读取 `BusinessType`、`BusinessID`、`RequestID`，通过 Options.OnError 将异步写入错误交给公司日志。Metadata 在最终 Run 事件时同步取一次；不要在回调访问数据库，也不要向后台保留 Gin Context。OnError 必须可并发、快速返回，使用适合后台场景的日志方式。

trace_store 连接需要实际增加初始化：当前 record 的 `helpers.InitMysql()` 只识别 demo、record、queryengine，**仅添加配置不会自动得到 trace_store 连接**。新增对应资源配置与基础 Gorm 句柄后，注入 writer；不要传业务事务句柄。原业务库仍可由业务沿现有 helpers 使用，不要求为了 Trace 重写业务数据库访问。

| 拟建表 | record 如何使用 |
| --- | --- |
| `tblLubanTraceRun` | 按业务标识、服务、Agent、时间找到一次完成运行 |
| `tblLubanTraceSpan00`～`tblLubanTraceSpan19` | 按 runId 固定分 20 张，同次 Run 的事件同表；用 `tracegorm.SpanTable(runID)` 定位，包含最终 Run 事件 |

DDL 以 [Spec 第 6 节](../specs/record-sdk-trace.md#6-表结构与幂等) 为唯一来源，由数据库变更流程执行。SDK 不自动建库、建表，也不关闭宿主连接。

运行时写库失败不改变业务结果。必须保留全量 Trace 日志及错误计数：数据库写入是尽力而为，队列满、持续故障或进程强杀可能丢失入库记录。只看到 Run 成功或 run 摘要行，不等于全部 Trace 已存储。

未启用落库可以只注入日志 writer；明确启用后，如果建表/连接检查失败，初始化报错，不静默假装启用成功。

## 6. 退出和回退怎么处理

HTTP/MQ 进程以及 AI cron 都要接入下面的顺序，不能只改 HTTP：

1. 停止接收新请求/任务，等待已开始的 Run 结束。
2. 创建独立退出 Context，调用 `writer.Close(shutdownCtx)`。
3. 排空完成后关闭数据库和日志。

建议初始排空预算 10 秒，按实测调整。Close 超时只表示本次等待结束，后台还会继续排空；可再次 Close 等待。若宿主必须退出，应记录 Pending/Failed 等计数并明确接受可能丢失，不能宣称已排空。

record 当前 `helpers.Release()` 为空，cron 也缺少 Trace 排空步骤，需找到实际退出挂载点补齐；main 有 defer 不等于容器退出一定足够。

回退时先停止、等待并排空，再去掉数据库 writer 注入，恢复日志 writer。保留已写入数据和表，不执行删表回退。

## 7. 接入顺序和验收

| 顺序 | record 的工作 | 通过标准 |
| --- | --- | --- |
| R1 | 补齐第 2 节的业务设计，调整 Go/依赖 | Run 范围和 DTO 明确，开发机及 CI 可拉取固定版本 |
| R2 | 加载 JSON，适配公司 HTTP，New/Register | fake 模型下可执行完整 flow，不启动 Luban 根服务 |
| R3 | 在真实业务入口调用 Run，接 logkit | 结果类型、错误出口、requestId/runId 和业务幂等正确 |
| R4 | Luban writer 交付后，接 trace_store 和退出排空 | 正常/失败/取消记录可查，重复事件不重复写入 |
| R5 | 测试环境验证真实 Case 和故障 | 业务结果、完整 Trace、导出查看、真实退出均通过 |

启用数据库 Trace 前，还需要核验：

| 条件 | 要求 |
| --- | --- |
| 数据库 | 确认 trace_store 的类型、版本、权限；OceanBase 需 MySQL 兼容模式，验证两表索引、事务、去重及元数据检查 |
| 大事件 | 默认事件上限 8 MiB；max_allowed_packet 至少 64 MiB，并实测含转义字符、错误摘要的边界数据往返；不满足则显式调低上限或调整数据库后重测 |
| 容量与保留 | 给出每秒 Run 数、每 Run 事件数/字节、保留天数和清理责任方；SDK 本期不创建自动清理任务 |
| 故障可见性 | DB 不可用、队列满、退出超时都有日志/计数，业务结果保持原语义 |
| 可读性 | 按 runId 导出 span 表 event_json 为逐行 JSON，离线页面可还原 Tool/Skill 和错误 |

本轮已有兼容性证据：record 与 SDK 在 Go 1.25.0 下使用临时 modfile、本地 replace 构建通过；公司 HTTP/Gin、Tool/Skill/OutputTool、取消和日志标识的隔离 race 探针通过。

**仍待验证：固定版本远端下载、实际业务流程、真实模型能力、trace_store、容量和容器退出。** writer 的离线实现验证见 [验证记录](../specs/record-sdk-trace-validation.md)。上述 record 接入代码仍为设计示例，没有修改 record 或连接真实数据库；本地测试仅向临时 SQLite 执行测试 SQL。
