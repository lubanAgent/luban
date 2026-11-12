# Luban SDK 接入与 Trace 持久化开发 Spec

- 日期：2026-11-02
- 分表调整：2026-11-03，Run 表 1 张、Span 按 runId 固定分 20 张，表名采用 `tblLubanTrace...`。
- 开发分支：`codex/record-sdk-trace`
- 代码基线：`632b45e2d5d77504afb25b3cca0cf0c6c3f83b04`
- 状态：**Luban 侧 T1～T4 已实现并完成本地验证；T5 的独立消费者构建已通过，远端版本发布/下载及真实数据库仍待验收。record 未改造。**

实现与测试结果见 [开发验证记录](record-sdk-trace-validation.md)，使用方式见 [tracegorm README](../../sdk/tracegorm/README.md)。本次没有启用根服务的数据库 Trace，也没有访问 trace_store。

**文档分工：** 本文供 Luban 开发使用；record 的业务准备、接入位置、配置和代码示例、数据库启用及交接验收已拆到 [record 接入 Luban SDK 指南](../integration/record-sdk.md)。

## 1. 这次交付什么

**让已有业务服务直接使用 Luban SDK，并通过注入数据库连接保存 Trace。record 是第一个使用方。**

业务仍然调用：

```go
output, err := rt.Run(ctx, agentKey, input)
```

本轮只增加可选的 `sdk/tracegorm` 包、建表 SQL、接入示例和相关测试。Runtime、Tool、Skill、Batch、OutputTool 的业务执行语义保持现状。

| 边界 | 本期处理 |
| --- | --- |
| SDK 执行 | 复用 `New → Register → Run`，不要求业务使用 Luban 根服务 |
| Trace 采集 | 复用现有完成事件和 `TraceWriter`，不增加开始事件 |
| Trace 存储 | 注入 `*gorm.DB`，后台事务写入 1 张 Run 表和 20 张按 runId 路由的 Span 表 |
| 日志 | 宿主继续记录 `luban_trace`，存储错误由回调交给宿主 |
| record 接入 | 本文只定义交接契约，不修改 record 的业务实现 |
| 模型请求 | 复用 SDK 协议和 `HTTPDoer` / `ModelInvoker`；公司适配归宿主 |
| Trace 页面 | 复用离线页面，验证从数据库导出的事件可解析；不开发在线查询页面 |
| 不纳入本期 | 配置中心、配置存库、自动建表、月分表、自动清理、可靠 MQ、磁盘缓冲、断点恢复、全局限流 |

**可靠性定位：诊断数据的尽力持久化。** `Run` 成功不等于 Trace 已写入；正常退出会等待排空。进程被强杀、队列满或数据库持续不可用时可能丢失数据库记录，必须由日志及计数明确体现，不承诺零丢失。

这份 Spec 提供可直接拆任务的默认实现。流量、保留周期和目标库版本属于启用前的环境核验项，不阻塞通用存储包开发。

## 2. 当前代码与复用点

| 已有能力 | 代码位置 | 本轮处理 |
| --- | --- | --- |
| 注入 TraceWriter | `sdk/runtime.go` 的 `Options.TraceWriter` | 不改签名 |
| TraceWriter、RunID | `sdk/trace.go` | 不改签名 |
| 完整事件快照 | `sdk/internal/trace/trace.go` | 保持 version=1 及现有字段 |
| Run 最终事件 | `sdk/run.go` | 仍在 Run 返回前发出，整次 Run 的事件最后到达 |
| 模型、Tool、Skill 事件 | `sdk/internal/eino/` | 保持事件归属与 Token 表达 |
| 模型传输参考 | SDK 的 `HTTPDoer` / `ModelInvoker` 接口 | 由宿主在业务侧实现；根 module 与新存储包不绑定具体传输 |
| 日志 Writer | `server/log.go` | 通用日志包可单独使用，不自动启用数据库 |
| 离线解析器 | `sdk/trace-viewer/trace-data.js` | 原事件 JSON 导出后应继续兼容 |

本期不为 record 改 Go/Gin Context 体系，不改变配置 key，也不要求业务采用 `agents/` 自动发现。SDK 子包仍位于唯一根 Go module 内。

## 3. 使用方式和依赖方向

```text
宿主初始化 Gorm 连接
        ↓
tracegorm.New：校验本地参数和 Gorm 句柄，启动一个写入器
        ↓
注入 Options.TraceWriter
        ↓
Runtime 完成节点 → TraceEvent → 日志 + 入队
                                         ↓
                                  后台批次事务
                                         ↓
                         tblLubanTraceSpan0～19 + tblLubanTraceRun
```

依赖方向：`record → sdk/tracegorm → sdk 的公共事件类型 + Gorm`。SDK 根包不能反向 import `tracegorm`，`tracegorm` 不 import `record`、`adapters`、`server`、公司库或业务 Model。

模型兼容性不需要扩展 Runtime。record 通过 SDK 的协议调用器加公司 `HTTPDoer` 适配即可；原 `CallLLM` 缺少 Tool 协议，不能直接替代该适配。特殊媒体参数如 `video_url.fps` 等待真实需求确认，不预先加入框架。

## 4. 公共 API

以下 API 已在 `sdk/tracegorm` 实现，仍需宿主显式创建和注入；不会因为导入 SDK 自动启用。

```go
package tracegorm

func New(ctx context.Context, db *gorm.DB, options Options) (*Writer, error)

// 返回 runId 对应的物理 Span 表名，写入与导出共用此规则。
func SpanTable(runID string) string

type Options struct {
    Service         string
    Metadata        func(*gin.Context) Metadata
    OnError         func(WriteError)
    QueueSize       int
    MaxPendingBytes int64
    MaxEventBytes   int64
    BatchSize       int
    BatchBytes      int64
    FlushInterval   time.Duration
    WriteTimeout    time.Duration
}

type Metadata struct {
    BusinessType string
    BusinessID   string
    RequestID    string
}

type WriteError struct {
    Operation  string // metadata / enqueue / persist / worker
    RunID      string // 批次级错误留空
    SpanID     string // 批次级错误留空
    EventCount int
    Err        error
}

type Stats struct {
    Received      uint64
    Accepted      uint64
    Persisted     uint64
    Rejected      uint64
    Failed        uint64
    Pending       uint64
    PendingBytes  int64
    WriteFailures uint64
}

type Writer struct { /* 私有状态 */ }

func (w *Writer) WriteTrace(ctx *gin.Context, event luban.TraceEvent)
func (w *Writer) Stats() Stats
func (w *Writer) Close(ctx context.Context) error
```

实现必须有编译期检查：

```go
var _ luban.TraceWriter = (*Writer)(nil)
```

### 4.1 最少只需注入连接和服务名

```go
writer, err := tracegorm.New(startupCtx, traceDB, tracegorm.Options{
    Service: "record",
})
if err != nil {
    return err
}

rt, err := luban.New(luban.Options{
    Config:      agentConfig,
    Models:      modelInvokers,
    TraceWriter: writer,
})
// 按现有 API 调用 rt.Register(funcs, tools, skills)。
```

示意省略了业务变量定义。模型也可以通过已有 `Projects` 或 `ProjectDefaults` 注入，不新增第三种模型接入接口。

若后续 New Runtime 或 Register 失败，装配方必须 Close 已创建的 writer 后再退出；全部装配成功后才发布 Runtime。退出等待 Context 需重新创建，不能复用已经超时的启动/业务 Context。

默认 `OnError` 使用标准库 `log/slog` 输出错误摘要、事件 ID 和数量，不输出完整 payload。宿主可替换为 logkit；失败信息不依赖 SDK 的 `server.Logger`。

### 4.2 默认值与校验

| 字段 | 默认值 | 规则 |
| --- | --- | --- |
| Service | 必填 | 非空，最多 64 个 Unicode 字符 |
| QueueSize | 1024 | 待处理事件数上限，**包含正在写入/重试的批次** |
| MaxPendingBytes | 32 MiB | 待处理序列化数据预算，含在途批次 |
| MaxEventBytes | 8 MiB | 单条事件及元数据的序列化预算 |
| BatchSize | 50 | 单个事务事件数上限 |
| BatchBytes | 1 MiB | 常规批次序列化预算 |
| FlushInterval | 250 ms | 队列非空时触发批处理的最长定时等待；不包含前一批的数据库等待 |
| WriteTimeout | 2 s | 每次事务尝试的超时 |

数值为 0 采用默认，负数错误；要求 `BatchSize ≤ QueueSize`、`BatchBytes ≤ MaxPendingBytes`、`MaxEventBytes ≤ MaxPendingBytes`。单个事件大于 BatchBytes、但不超过其他上限时，单独写入一个批次。

预算按已序列化的原始事件和元数据字节计算，不把它宣传为进程 RSS 上限。SDK 生成快照、序列化和数据库驱动仍有额外内存开销；宿主应按最大事件体积做容量验证。

`New` 拒绝 nil ctx、nil/无效 Gorm 对象、已带 error 的句柄和不支持的驱动。MySQL 方言为本期生产目标，SQLite 只用于本地自动化；其他方言返回明确错误，不因使用 Gorm 就宣称全部支持。

### 4.3 元数据如何取得

- `Service` 启动时固定。
- Agent Key 取最终 `type=run` 事件的 `Name`，不从模型节点名推测。
- `Metadata` 只在最终 Run 事件的同步回调中调用一次，宿主可从 Gin Keys 取得业务标识与 requestId。
- 直接调用 WriteTrace 时允许 nil Gin Context；此时跳过 Metadata，按空元数据处理。
- 元数据必须在 Run 结束前可用；同一业务的重试可复用 BusinessID，但每次 Run 仍有独立 runId。
- 回调不做网络/数据库操作，不保存 Context，也不调用 Writer.Close。
- Metadata panic 或字段超长时，仍保存原始事件和 Run 摘要，元数据置空，通过 `OnError(Operation=metadata)` 报告；不让附加标识错误造成整条 Trace 丢失。
- 不读取数据库密码、模型 Key、请求 Header，也不扫描任意 Gin Keys。除了这三个明确字段，不增加自由扩展 map。

多个 ID 可由业务选一个主要 BusinessID；本期不提前设计业务专用索引。未提供 Metadata 时字段为空，仍可按 runId、Service、Agent Key 和时间查找。

## 5. 初始化、执行和退出契约

### 5.1 初始化

`New` 只做本地装配，不访问数据库验证连接或表结构：

1. 校验参数、Driver 和 Gorm 基础句柄。
2. 使用独立 Session，清除查询条件并覆盖原 Context；不修改宿主句柄。
3. 不 Ping、不读取 schema、不验证表或索引；宿主必须先通过数据库变更流程完成建表。
4. 不 AutoMigrate、不插入探测记录、不建表。
5. 成功后创建独立内部 Context、队列和一个 worker。startupCtx 随后取消不应停止已成功创建的 writer。

`New` 要求基础数据库连接，不能传事务或带业务 Scope 的对象。实现须拒绝可识别的事务（例如 `gorm.TxCommitter`）；不能以 `Session(NewDB: true)` 误认为已经脱离原事务。宿主仍负责正确注入基础句柄。

实现补充：内部 Session 先生成干净 Statement，再用于后续 WithContext，防止宿主原查询条件残留；私有行的表名/列名固定，不受宿主 NamingStrategy 影响。写入 Session 使用静默 SQL logger、跳过 Model Hooks，避免失败 INSERT 将完整 Prompt 打到普通 SQL 日志；宿主另行注册的 Gorm callbacks/plugins 仍需自行保证兼容。缺失表、字段不兼容或索引错误会在实际异步写入时通过 `OnError` 和 `Stats` 体现，不影响业务 Run 结果。

### 5.2 接收事件

`WriteTrace` 不返回 error，保持已有接口。路径固定为：

```text
检查 writer 状态 → 校验事件 → 读取可选元数据 → 完成序列化
    → 原子检查数量/字节预算 → 入队 → 返回
```

回调内不访问数据库、不等待队列腾空，不为每条事件创建 goroutine。

事件校验针对持久化格式：Version=1，RunID/SpanID/Type 非空，字段长度符合表结构，StartedAt 转 UTC 后位于目标 DATETIME 可表示的 1000～9999 年，DurationNs 非负，Status 属于 success/error/stopped/skipped，Input/Output 为有效 JSON（空值统一编码 null）。`FlowIndex` 为 nil 或非负数。skipped 仅用于有 FlowIndex 的外层 Agent 节点，必须带 `skipReason=empty_input`；其他状态不能带跳过原因。未知 Type 只要满足长度和格式即可保留，不固定为当前六种。

Type=run 必须没有 ParentSpanID，Name 作为 Agent Key 校验长度；对于空 Agent Key 导致的 Run 参数错误，仅 error 状态允许 Name 为空以保留错误记录。ID 使用非空白的可打印 ASCII，避免数据库 VARCHAR 尾部空格比较混淆身份；文本和 JSON 校验 UTF-8。其他字段由原始 event_json 完整保存。Name/Description/Output 不截短后再假装保存成功。

序列化完成后不再引用调用方可变数据：复制 FlowIndex 所指的值，Input/Output 编码到独立字节；队列不持有 `*gin.Context`、请求对象或业务返回值。

上限超出、序列化失败、writer 已关闭等情况，Rejected 加一，通过 OnError 明确报告。**8 MiB 不是采集截断规则；超过上限的事件拒绝入库，宿主日志仍保留原记录。** 调整上限时需同步验证数据库 packet、内存与写入耗时。

### 5.3 批次与重试

- 单 worker 按入队顺序获取批次；数量、字节或定时条件任一满足即可写入。
- 每个事务先插入全部 span，再插入该批中的 Run 摘要；统一提交。没有 Run 事件的批次只写 span。
- 每次尝试使用独立 `context.WithTimeout(workerCtx, WriteTimeout)`，不继承业务请求的取消 Context。
- 每批最多尝试 **3 次**，失败后等待 100 ms、200 ms，再重试同一不可变批次；等待可以被内部停止取消。
- 任一次事务失败都回滚，不单独提交 Run 摘要。
- 最终失败标记整批 Failed，报告批次数量并释放预算，继续后面的批次。首期不做按事件拆批重试或持久化补偿。
- 中间失败只累计 WriteFailures；同一批重试耗尽后调用一次 `OnError(Operation=persist)`，错误包装 ErrPersistence 并保留原始原因。进程内只保留失败总数和第一个最终失败原因用于 Close 汇总，不无限积累错误列表或失败事件正文。
- 成功提交后计入 Persisted，释放预算。commit 返回不确定错误时仍按同样批次重试，重复键规则负责幂等。
- 超时只约束遵守 Context 的驱动；宿主仍须合理设置数据库连接/读写超时，不能承诺强行终止任意阻塞驱动。

错误统计与诊断不得改变 Runtime 的业务结果。Run 的失败、Stop、取消或 panic 已转换成 error 时，Trace 仍按同一通道尝试保存。

### 5.4 关闭

Writer 生命周期为 `open → closing → closed`；致命 worker panic 进入不可继续写入的终态。

| 场景 | 必须行为 |
| --- | --- |
| 第一次 Close | 原子切换 closing，拒绝后续入队，唤醒 worker 排空已有数据 |
| 并发 WriteTrace/Close | 已成功入队的事件必须属于排空集合；不能向已关闭 channel 发送 |
| 并发或重复 Close | 共享同一排空过程；各调用者使用自己的等待 ctx |
| Close 等待 ctx 到期 | 返回 ctx.Err，writer 继续 closing 和后台排空；调用者可重新 Close 等待 |
| 全部排空成功 | 返回 nil；表示已接受的事件均完成写入，不代表所有 Received 都被接受 |
| 有接受的事件最终失败 | 排空结束仍返回包装 `ErrPersistence` 的汇总错误，含失败数量 |
| worker panic | recover，拒绝新事件，未解决的 Accepted 全部计 Failed，报告错误，结束等待，Close 返回 ErrPersistence |
| 关闭以后写入 | 不 panic，Rejected 增加，报告 ErrClosed |

Close 的等待 ctx 到期不关闭宿主 DB，也不丢弃在途队列。调用方若必须按时结束进程，须先记录未完成计数，明确接受该部分可能丢失；随后不得声称已经排空。

宿主退出顺序必须是：

```text
停止新请求/新任务 → 等待在途 Run → Close(writer) → 关闭 DB → 关闭日志
```

建议宿主初始排空等待 10 秒；它是宿主退出预算，不是 SDK 额外强制超时。大量积压可能无法在 10 秒内完成，应在测试环境核对。

`OnError` 在持锁之外调用，允许并发，需快速返回；不保证任意阻塞回调也能及时退出。回调 panic 由 writer recover 后通过标准日志报出，不得杀死 worker；不递归调用出错的回调。

### 5.5 错误与统计

公开 sentinel：`ErrClosed`、`ErrQueueFull`、`ErrEventTooLarge`、`ErrInvalidEvent`、`ErrPersistence`。构造错误直接由 New 返回；异步错误通过 WriteError 上报，支持 errors.Is。

Stats 返回同一锁保护的快照：

| 计数 | 定义 |
| --- | --- |
| Received | WriteTrace 调用数 |
| Accepted / Rejected | 成功入队数 / 未入队数 |
| Persisted | 已被成功事务处理的事件数，包含重复键，无关 RowsAffected |
| Failed | 已接受但重试耗尽或 worker 终止后未确认成功的事件数；commit 结果不确定时，数据库可能已有该行 |
| Pending / PendingBytes | 队列和在途批次的未解决事件及序列化预算 |
| WriteFailures | 失败的事务尝试次数；重试后成功也保留计数 |

静止时满足 `Received = Accepted + Rejected`、`Accepted = Persisted + Failed + Pending`。序列化中的调用尚未归类，运行中的第一条等式可短暂不成立。所有计数仅在本进程内，重启清零，不等于数据库行数。

Close 的终态结果只统计 Accepted 的持久化结果；关闭后的误用不会改变已经返回的终态错误。Rejected 由回调与 Stats 单独体现。

## 6. 表结构与幂等

共 **21 张本期新建目标表**：`tblLubanTraceRun` 不分表，`tblLubanTraceSpan0`～`tblLubanTraceSpan19` 固定 20 张。没有声称 trace_store 中已经存在。不在 SQL 中创建库，也不写 `USE trace_store`。同一存储包可由其他业务注入其他数据库。

分表规则为 **完整 runId 原始字节的 CRC32 IEEE 值 % 20**，含 `run_` 前缀，区分大小写，无随机种子。序号从 0 开始且不补零，因此是 `0`～`19`；`tracegorm.SpanTable(runID)` 同时供写入与查询使用，返回值只包含固定表名前缀和序号，不拼入调用者原始文本。比如 `run_test` 落 `0`，`run_00000000000000000000000000000000` 落 `17`。同一次 Run 的所有事件都落同表；跨机器与重试的路由一致。

分表数不作为可随意修改的 Options：修改哈希规则或数量会改变已有数据位置，必须另行迁移。已有旧 snake_case 单表的数据不会自动迁移，也不自动双读/双写；本轮只更新新建 DDL，没有访问或修改目标库。字段名保持 snake_case。

数据库目标是 MySQL 兼容模式；OceanBase 必须验证对应模式、驱动、字符集、事务和元数据接口后才能启用，Oracle 模式不在本期范围。

字段长度和公共字段参考 `example_record`：Run、Span 标识采用 `varchar(64)`，状态/类型采用 `varchar(32/64)`，业务关联值保留到 `varchar(255)`。所有表都带公共字段 `id BIGINT UNSIGNED AUTO_INCREMENT`、`create_time BIGINT DEFAULT 0`、`update_time BIGINT DEFAULT 0`、`deleted TINYINT UNSIGNED DEFAULT 0`。每列和每张表都有中文 `COMMENT`；普通字段提供明确 `DEFAULT`。自增 `id` 由数据库生成；`longtext` 字段 `error_msg/event_json` 按数据库规范必须可空，且禁止设置默认值，SDK 每次写入事件时仍显式赋值。`started_at` 的缺省哨兵值为 `1970-01-01 00:00:00.000000`，正常写入始终使用真实事件时间。时间和软删字段采用与 record 一致的语义；SDK 首次插入时令创建、更新时间相同且 `deleted=0`，Trace 事件不更新。字段直接继承表级 `utf8mb4`，不逐列声明字符集和排序规则；SDK 生成的 runId/spanId 固定为小写十六进制或 `s_N`，因此默认排序规则不会产生身份碰撞。

### 6.1 `tblLubanTraceRun`

只在收到最终 Run 事件时写入一行，字段代表业务执行结果，不代表 Trace 完整率。

```sql
CREATE TABLE tblLubanTraceRun (
    id            bigint(20) unsigned NOT NULL AUTO_INCREMENT COMMENT '主键ID',
    run_id        varchar(64) NOT NULL DEFAULT '' COMMENT 'Run唯一标识',
    service       varchar(128) NOT NULL DEFAULT '' COMMENT '服务名称',
    agent_key     varchar(255) NOT NULL DEFAULT '' COMMENT 'Agent标识',
    business_type varchar(64) NOT NULL DEFAULT '' COMMENT '业务类型',
    business_id   varchar(255) NOT NULL DEFAULT '' COMMENT '业务标识',
    request_id    varchar(255) NOT NULL DEFAULT '' COMMENT '请求标识',
    status        varchar(32) NOT NULL DEFAULT '' COMMENT '运行状态',
    started_at    datetime(6) NOT NULL DEFAULT '1970-01-01 00:00:00.000000' COMMENT '开始时间，UTC',
    duration_ns   bigint(20) NOT NULL DEFAULT 0 COMMENT '执行耗时，纳秒',
    error_msg     longtext COMMENT '错误信息',
    create_time   bigint(20) NOT NULL DEFAULT 0 COMMENT '创建时间，Unix秒',
    update_time   bigint(20) NOT NULL DEFAULT 0 COMMENT '更新时间，Unix秒',
    deleted       tinyint(3) unsigned NOT NULL DEFAULT 0 COMMENT '删除标记，0未删除',
    PRIMARY KEY (id),
    UNIQUE KEY uk_trace_run_run_id (run_id),
    KEY idx_trace_run_agent_time (service, agent_key, started_at, run_id),
    KEY idx_trace_run_business (service, business_type, business_id, started_at),
    KEY idx_trace_run_time (started_at, run_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Luban Trace运行摘要';
```

### 6.2 `tblLubanTraceSpan0`～`tblLubanTraceSpan19`

每个完成事件一行，**包含最终 Run 事件本身**。run 表仅是便于列表检索的投影，完整事件以本表 event_json 为准。

下面展示 0 表，1～19 的结构完全相同，表名和 `idx_trace_span_timeNN` 索引名使用各自序号；完整 21 张表见 [建表 SQL](../../sql/trace/001_create_tables.mysql.sql)。

```sql
CREATE TABLE tblLubanTraceSpan0 (
    id             bigint(20) unsigned NOT NULL AUTO_INCREMENT COMMENT '主键ID',
    run_id         varchar(64) NOT NULL DEFAULT '' COMMENT 'Run唯一标识',
    span_id        varchar(64) NOT NULL DEFAULT '' COMMENT 'Span唯一标识',
    parent_span_id varchar(64) NOT NULL DEFAULT '' COMMENT '父Span唯一标识',
    kind           varchar(64) NOT NULL DEFAULT '' COMMENT 'Span类型',
    status         varchar(32) NOT NULL DEFAULT '' COMMENT 'Span状态',
    started_at     datetime(6) NOT NULL DEFAULT '1970-01-01 00:00:00.000000' COMMENT '开始时间，UTC',
    duration_ns    bigint(20) NOT NULL DEFAULT 0 COMMENT '执行耗时，纳秒',
    event_json     longtext COMMENT '完整Trace事件JSON',
    create_time    bigint(20) NOT NULL DEFAULT 0 COMMENT '创建时间，Unix秒',
    update_time    bigint(20) NOT NULL DEFAULT 0 COMMENT '更新时间，Unix秒',
    deleted        tinyint(3) unsigned NOT NULL DEFAULT 0 COMMENT '删除标记，0未删除',
    PRIMARY KEY (id),
    UNIQUE KEY uk_trace_span_identity0 (run_id, span_id),
    KEY idx_trace_span_time0 (started_at, run_id, span_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Luban Trace Span分表0';
```

字段映射：

| 内容 | 来源和规则 |
| --- | --- |
| id | 公共自增主键，由数据库生成 |
| run_id / span_id / parent_span_id | 原事件；ASCII ID，最多 64 字节且不截断 |
| kind / status | 原 Type / Status；kind 最多 64 字节，status 最多 32 字节 |
| started_at | 原 StartedAt 转 UTC，截去微秒以下精度存索引列；原时间完整保留在 event_json |
| duration_ns | 原纳秒耗时，不能由两个 DATETIME 相减重算 |
| event_json | 原事件完整 JSON，包括 Input/Output、Description、FlowIndex、TransferFromSpanID 和真实 Token 用量 |
| create_time / update_time | 首次插入的 Unix 秒时间戳；Trace 行不再更新，因此两者初始相同，重试不改写 |
| deleted | 初始为 0；SDK 不做软删除，供宿主平台查询和清理 |
| service / agent_key | Options.Service / 最终 Run.Name；最多分别 128 / 255 个 Unicode 字符 |
| business_* / request_id | 可选 Metadata 快照；BusinessType 最多 64，其余最多 255 个 Unicode 字符 |

JSON 文本选 LONGTEXT 便于不同 MySQL 兼容数据库接入，写入前验证 JSON。不保存图片/视频二进制，不额外下载媒体；如果输入本身含大数据字符串，按同一大小规则处理。

实现用 UTC 微秒格式字符串绑定 DATETIME，避免宿主 DSN 的 `loc=Local` 将 UTC 索引时间再次转换；event_json 保留原时间与完整精度。

无外键：子事件完成早于父事件，父可能尚未入库或最终丢失。禁止为了满足外键而伪造未完成的父事件。

### 6.3 幂等与完整性

- 公共主键均为 `id`；run_id 是唯一键，span 的 `(run_id, span_id)` 是唯一键。宿主平台可使用公共 `id` 统一分页和关联，SDK 幂等仍按 Run/span 身份判断。
- 第一次成功写入为准，重复事件不改写已有内容；上游必须保持同一身份的完成事件不变。
- 使用 Gorm `OnConflict{DoNothing: true}` 或同等精确重复键语义。当前 MySQL 驱动生成 `ON DUPLICATE KEY UPDATE run_id=run_id`；不得使用会掩盖其他约束/数据错误的宽泛 `INSERT IGNORE`，也不使用替换删除语义。
- 一批事件按分表归组，按 0～19 顺序写入非空分组，再写 Run 摘要；全部在同一个事务内。某个分表或摘要写入失败，整批回滚并沿用原重试策略；摘要不能独立于其原事件提交。
- 其他 span 可能跨批次写入。**run.status=success 仅说明业务成功，不能据此认定所有 span 已保存。**
- 没有最终 Run 行但有 span 的运行，可以按 runId 查原始事件；不能补一个 success 摘要。
- 本期不做 Token 汇总冗余列、expected span 数或实时 running 状态；离线页面按实际事件还原。

## 7. 日志保留与读取

record 推荐同时保留日志和数据库：

```go
// 这是宿主的接入示意，不放入 SDK 公司依赖。
func (w businessTraceWriter) WriteTrace(ctx *gin.Context, event luban.TraceEvent) {
    // 先按原方式输出 runId + traceEvent，再交给数据库 writer。
    w.writeLog(ctx, event)
    w.database.WriteTrace(ctx, event)
}
```

数据库 writer 自身**不自动输出全部正常 Trace**，也不自动注册自己；没有数据库需求的使用方继续只注入日志 writer。若宿主不保留全量日志，队列拒绝或最终失败的数据就没有完整正文兜底；错误回调只有摘要，不能冒充完整备份。

本期提供 README 中的参数化查询示例。已知 runId，只查它对应的一张表；表名使用 SDK 的固定路由，查询值仍绑定参数：

```go
var rows []struct{ EventJSON string }
err := db.Table(tracegorm.SpanTable(runID)).
    Select("event_json").Where("run_id = ?", runID).
    Order("started_at, span_id").Scan(&rows).Error
```

按行原样导出 event_json 为 NDJSON，可交给现有页面。这个排序只用于展示稳定性，不把 `s_10` 与 `s_2` 的字符串顺序当作实际执行先后；父子关系仍依据 ID 和时间。

不在 SDK 中新增 HTTP 路由、鉴权、通用查询 Service 或页面网络访问。业务将来需要查询 API 时另行设计。

## 8. 文件与任务拆分

以下路径均相对于 Luban 仓库根目录。文件允许在不改变职责的前提下小幅合并。

| 文件/目录 | 变化 | 职责 |
| --- | --- | --- |
| `sdk/tracegorm/writer.go` | 新增 | API、队列、并发状态、Stats、Close |
| `sdk/tracegorm/options.go` | 新增 | 默认值、错误类型、元数据契约 |
| `sdk/tracegorm/store.go`、`event.go` | 新增 | 私有行/快照、事务与幂等；启动不检查 schema，不导出业务 Model |
| `sdk/tracegorm/sharding.go` | 新增 | 固定 20 分表路由，导出 SpanTable 供查询复用 |
| `sdk/tracegorm/*_test.go` | 新增 | 并发、故障、SQL、实际 Runtime 闭环 |
| `sdk/tracegorm/README.md` | 新增 | 最小接入、双写日志、退出、故障、导出说明 |
| `sql/trace/001_create_tables.mysql.sql` | 新增 | 本文 DDL；不自动执行 |
| `sdk/examples/trace-gorm/` | 新增 | 本地 fake model + 临时 SQLite 完整示例 |
| `sdk/trace-viewer/trace-data.test.js` | 按需修改 | 使用数据库导出格式的回归 fixture |
| `README.md` | 修改 | 说明可选 Trace 持久化与 SDK 入口 |
| `docs/specs/record-sdk-trace.md` | 本文 | 开发契约与状态 |

| 阶段 | 开发内容 | 完成条件 |
| --- | --- | --- |
| T1 | API、Options、事件编码、DDL、基础 Store | 参数/字段校验和 MySQL SQL 测试通过 |
| T2 | 队列预算、批次、事务和重试 | 正常写入、去重、回滚和失败继续通过 |
| T3 | Stats、错误回调、Close、panic 收敛 | race、排空与超时重入通过，无 worker 泄漏 |
| T4 | Runtime/Tool/Skill/Batch 接入、导出与示例 | 实际 SDK 输出可落 SQLite 并被页面解析 |
| T5 | 宿主消费构建、文档与版本交付 | 独立消费者构建、无根服务导入；远端可拉取固定版本 |

T1～T4 不依赖 record 业务方案。T5 的正式接入、目标库和容器验收需要 record/环境交接，但不因此提前改写 record 的业务流程。

## 9. 验收矩阵

实现测试必须验证可观察行为；并发/取消用屏障或可控 store，不用长时间 sleep 猜时序。内部 store 可为测试保留私有接缝，不为测试新增公共框架接口。

| 编号 | 场景 | 通过标准 |
| --- | --- | --- |
| C01 | 旧用户未注入数据库 writer | 原 Run、日志 writer、页面测试全部通过 |
| C02 | New 参数/句柄错误 | 立即返回明确 error；空库或缺表可正常启动装配，首次写入失败由 OnError/Stats 暴露 |
| C03 | 正常函数→模型→Tool/Skill→OutputTool | 业务结果不变；每个完成事件落一行，可解析完整输入输出和真实用量 |
| C04 | 多 Run、并发 Tool、Batch | runId 和 span 关系不串；所有已接受事件在 Close 后可查；race 通过 |
| C05 | 重复投递、commit 结果不确定后重试 | 数据库无重复行、不覆盖首次内容；无其他错误被重复键逻辑吞掉 |
| C06 | 插入 span 后 Run 摘要失败 | 本批事务回滚，无单独提交的摘要；重复尝试结果一致 |
| C07 | 临时故障后恢复 | 同一批最多三次尝试，成功后继续；WriteFailures 保留，Failed 不增加 |
| C08 | 最终写入失败 | Run 仍按业务结果返回；Failed/回调可见；后续批次继续；Close 返回 ErrPersistence |
| C09 | 队列满、字节满、单事件超限 | 拒绝且无数据库等待；Rejected/错误明确；无静默截断；预算含在途批次 |
| C10 | 并发 Close/WriteTrace、重复 Close | 无 panic/race，接受边界确定；关闭后事件被拒绝 |
| C11 | Close 等待超时后再次等待 | 首次返回 ctx.Err；排空继续；再次 Close 得到最终结果；DB 未被 SDK 关闭 |
| C12 | 业务请求取消 | 业务返回原取消 error；数据库使用独立 Context，取消事件仍可保存 |
| C13 | 业务 error、Stop、panic | 对应终态和完整已产生事件可落库；不伪造剩余节点 |
| C14 | Metadata 与 OnError panic | worker 不被杀死，业务结果不变；元数据异常不丢原事件，诊断不递归 |
| C15 | worker 内部 panic | 未解决 Accepted 计 Failed，Close 不挂死，新事件拒绝 |
| C16 | 入队后修改原始 RawMessage/FlowIndex/Context | 数据库内容保持入队快照；后台不访问已回收 Gin Context |
| C17 | 父未选择子 Agent、父子移交 | nil/移交语义保持；ParentSpanID/TransferFromSpanID 与现有采集一致 |
| C18 | 没有最终 Run 事件 | 保留已有 span，不生成 success 摘要，不声称已完整完成 |
| C19 | UTF-8、大 Prompt/Skill、边界长度 | 精确保存预算内数据；超长索引字段明确拒绝或按元数据规则处理，不依赖 DB 截断 |
| C20 | 数据库导出 | NDJSON 可由 `parseLogs` 读取，与保存前事件语义一致 |
| C21 | 只导入 sdk 和 tracegorm 的宿主 | 使用 record 的 Gin/Gorm/Eino 组合编译；包依赖图不包含根 server、agents 或公司 integration module |
| C22 | 表结构 | MySQL DDL 与私有行类型字段、长度、索引由离线测试保持一致；New 不检查数据库 schema |
| C23 | 默认值、Metadata、统计 | 默认值生效；零/负数/关系非法分别按约定处理；静止时统计等式成立，重复键不以 RowsAffected 计算 Persisted |

至少一次完整 SDK→Writer→SQLite→导出→页面解析链路必须使用真实实现，不能全用 mock 代替。SQLite 只证明逻辑，不代替目标 MySQL/OceanBase 验收。

开发检查：

```bash
go test ./sdk/tracegorm/... ./sdk/...
go test -race ./sdk/tracegorm/... ./sdk/...
go vet ./sdk/tracegorm/... ./sdk/...
node --test sdk/trace-viewer/trace-data.test.js
make check
```

复用现有 `make check`，不新增重复检查脚本。测试只使用 fake 模型、httptest、临时 SQLite 或明确提供的专用测试 MySQL；不读取真实 conf 中的凭据。

## 10. 宿主交接边界

record 的具体交接内容已移至 [接入指南](../integration/record-sdk.md)：

- 第 2 节：业务设计需给出的 Run 范围、DTO、配置、Tool/Skill、失败处理与业务标识。
- 第 3～5 节：依赖和启动装配、公司模型 HTTP、日志及 trace_store 接入。
- 第 6～7 节：退出、回退、容量/保留、真实数据库与远端版本验收。

Luban 交付本 Spec 的公共 API、DDL、示例及 C01～C23 测试；record 交付业务流程和宿主适配。T1～T4 可独立开发，正式启用持久化仍需通过接入指南中的环境与业务验收，不能用本地 SQLite 或本地 replace 替代。

清理由数据库/宿主运维侧负责，本期 SDK 不创建 cron；按时间清理时需覆盖全部 20 张 Span 表，并保持 Run 摘要与 span 的保留规则一致。Trace 表不承担业务任务状态或恢复职责。

## 11. 评审与验证状态

| 项目 | 当前状态 |
| --- | --- |
| 新分支和依赖兼容性评估 | 已完成；record 临时 modfile + 本地 replace 构建通过 |
| 公司 Gin、HTTP、Tool/Skill/OutputTool、Trace 关联 | 隔离 race 探针通过；不代表真实业务效果 |
| Go 1.25.0 构建 | record 和 SDK 组合已通过 |
| MySQL 重复键 SQL | 当前驱动 DryRun 已确认；未连接数据库 |
| 本 Spec 的结构/API 一致性自查 | 已执行；检查记录在本文末尾 |
| tracegorm、DDL 文件、入库示例与验收测试 | 已实现；专项测试与 make check（普通/race/vet/build）通过 |
| 完整 SDK→SQLite→原始 JSON→查看页 | 已通过，覆盖 Tool/Skill/OutputTool、Batch、父子与错误 |
| 独立消费者 | Go 1.25.0、本地 replace 构建通过，无公司 integration、server 或 agents 包导入 |
| trace_store 与远端版本安装 | 待环境验证；已知代理/checksum 访问阻塞仍需解决 |

本轮自查重点：

1. 不在异步队列保存 Gin Context，不依赖原请求存活。
2. 队列字节预算包含正在写入及重试的批次。
3. 唯一键只处理重复事件，不吞掉其他 SQL 错误。
4. Close 超时与后台实际关闭分别定义，避免误关宿主 DB。
5. Run 成功、Trace 入队、Trace 入库、Trace 完整是不同事实。
6. 根包不反向依赖可选 writer，公司适配不进入存储包。
7. 初始兼容性证据、本地实现测试与真实库验收明确区分。
