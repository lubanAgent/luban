# Trace 数据库写入

`tracegorm` 是 SDK 的可选存储包：业务传入一个基础 `*gorm.DB`，SDK 完成节点后生成的 Trace 会排队、批量入库。未注入时，原有运行和日志行为保持不变。

**不需要启动 Luban Web 服务，不自动建表，不关闭业务数据库。** 当前提供 MySQL 兼容存储和 SQLite 离线测试；OceanBase 必须在实际 MySQL 兼容环境验证后使用。

## 接入

1. 通过数据库变更流程执行 [MySQL 建表 SQL](../../sql/trace/001_create_tables.mysql.sql)，创建 `tblLubanTraceRun` 和 `tblLubanTraceSpan0`～`tblLubanTraceSpan19`，共 21 张表。
2. 启动时创建 writer，并注入 Runtime：

```go
import (
    luban "github.com/lubanAgent/luban/sdk"
    "github.com/lubanAgent/luban/sdk/tracegorm"
)

writer, err := tracegorm.New(startupCtx, traceDB, tracegorm.Options{
    Service: "your-service",
})
if err != nil {
    return err
}

rt, err := luban.New(luban.Options{
    Config: agentConfig,
    Models: modelInvokers,
    TraceWriter: writer,
})
// 按原方式 rt.Register(funcs, tools, skills)，成功后才发布 Runtime。
// 这里及 Register 如果失败，也要关闭已创建的 writer。
```

3. 业务继续 `rt.Run(ctx, agentKey, input)`；退出时先等 Run 完成，再排空：

```go
shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
if err := writer.Close(shutdownCtx); err != nil {
    // 交给宿主日志，并检查 writer.Stats()，不能声称已全部保存。
}
// 排空成功后才关闭 traceDB 和日志。
```

New 只校验参数和传入的 Gorm 句柄，不 Ping、不读取 schema，也不检查 Run 表或 20 张 Span 表；宿主必须提前完成建表。必须传基础连接，不能传事务或 DryRun。内部使用独立 Session，宿主查询条件和请求取消不会带入后台写入。缺表或字段不兼容会在实际异步写入时通过 OnError/Stats 暴露。

Close 等待超时不会停止后台排空，也不会关闭数据库；可用新的 Context 再次等待。所有已接受事件中只要有最终失败，Close 就返回可被 `errors.Is(err, tracegorm.ErrPersistence)` 识别的错误。队列拒绝的事件单独通过 Stats/OnError 表示。

## 同时保留公司日志

数据库 writer 不自动打印完整 Trace。业务可以组合两个已有接口实现：

```go
type traceSink struct {
    log luban.TraceWriter
    db  luban.TraceWriter
}

func (s traceSink) WriteTrace(ctx *gin.Context, e luban.TraceEvent) {
    s.log.WriteTrace(ctx, e)
    s.db.WriteTrace(ctx, e)
}
```

日志 writer 在同步回调中写 `luban_trace`、`runId` 和完整 `traceEvent`。数据库 writer 只保存序列化快照，不把 Gin Context 留给后台。

`Options.OnError` 可接入宿主日志；默认使用标准库 slog 记录错误摘要。回调要支持并发、快速返回，不能在回调中 Close 或递归写同一个 writer。宿主的 Gorm callbacks/plugins 仍属于宿主连接的一部分，须避免记录完整 SQL 载荷或执行额外副作用；私有行不执行 Model Hooks，默认 SQL logger 在存储 Session 中静默。

## 业务关联和默认值

可选 Metadata 回调仅在最终 Run 事件时、用当前 Gin Context 同步读取一次：

```go
Metadata: func(ctx *gin.Context) tracegorm.Metadata {
    return tracegorm.Metadata{
        BusinessType: "lesson",
        BusinessID: ctx.GetString("lessonID"),
        RequestID: ctx.GetString("requestId"),
    }
},
```

没有这些字段也可按 runId 查。回调异常或元数据超长时，保留事件、清空元数据并报告错误。Service 最多 128 个字符，BusinessType 最多 64，BusinessID/RequestID 最多 255。

| 参数 | 默认值 |
| --- | --- |
| 待处理数量 QueueSize | 1024，包含正在写入/重试的批次 |
| 待处理字节 MaxPendingBytes | 32 MiB，包含在途批次 |
| 单事件 MaxEventBytes | 8 MiB，超限明确拒绝，不截断 |
| 批次数量 / BatchBytes | 50 / 1 MiB |
| FlushInterval | 250 ms |
| 每次 WriteTimeout | 2 s |
| 每批尝试 | 最多 3 次，间隔 100 ms、200 ms |

数值 0 使用默认，负数报错；批次和单事件限制不能大于总预算。大于 BatchBytes 的合法事件独占一批。预算计算序列化事件与元数据，不等于进程内存上限。

Stats 区分 Received、Accepted、Rejected、Persisted、Failed、Pending、PendingBytes 和 WriteFailures。Persisted 是成功处理的事件数量，包含重复投递，不等于数据库行数。

## 存储、故障和导出

- span 表保存所有完整事件，包括最终 run 事件；run 表是完成运行的检索摘要。
- 分表固定为 `CRC32 IEEE(完整 runId 原始字节) % 20`，包含 `run_` 前缀、区分大小写。`tracegorm.SpanTable(runID)` 返回 `tblLubanTraceSpan0`～`19`，同一次 Run 的所有事件在同一张表。跨进程、重试与查询使用同一路由。
- 空模型输入的外层 Agent span 保存为 `status=skipped`、`skipReason=empty_input`；Run 仍按后续函数实际结果记录。此状态不增加模型、Tool 或 Skill 的虚假 span。`status` 列为 varchar(32)。
- 公共字段与 record 表一致：自增 `id`、Unix 秒 `create_time/update_time`、默认 0 的 `deleted`。`run_id/span_id` 为 varchar(64)，`kind` 为 varchar(64)。每列和每张表都有中文 `COMMENT`；普通字段有明确 `DEFAULT`。自增 `id` 由数据库生成；`longtext` 类型的 `error_msg/event_json` 按规范必须可空且禁止默认值，SDK 每次写入事件时仍显式赋值。
- `run_id` 是 Run 表唯一键，`(run_id, span_id)` 是 Span 表唯一键；它们继续负责异步重试幂等。
- span 身份是 `(run_id, span_id)`；重复投递保留首次成功内容。一批数据按分表归组，在同一个事务保存所有分组及 Run 摘要；任一表写入失败则整批回滚。
- 当前是完成事件：进程中断可能只有部分 span，没有最终 run 行。run 行存在也不能证明没有丢失其他 span。
- 数据库故障、队列满和事件过大不改变业务 Run 的结果。进程强杀或持续故障可能丢数据；这是尽力持久化，不是可靠任务队列。
- 保留完整日志作为兜底；错误回调只有摘要，不能恢复被拒绝的载荷。容量、保留天数和清理由宿主负责，本期无自动清理任务。
- 使用默认 8 MiB 上限前，目标库的 max_allowed_packet 至少 64 MiB，并验证转义字符、错误摘要的边界写入；真实 MySQL/OceanBase 验收不能由 SQLite 测试替代。

按 runId 查询只访问一个分表；使用 SDK 路由表名，值继续使用参数绑定：

```go
var rows []struct{ EventJSON string }
err := db.Table(tracegorm.SpanTable(runID)).
    Select("event_json").Where("run_id = ?", runID).
    Order("started_at, span_id").Scan(&rows).Error
```

每行原样导出 event_json 为 NDJSON，导入 [离线 Trace 查看页](../trace-viewer/index.html)。顺序仅便于展示，父子关系仍按事件 ID 建立。SDK 不增加在线查询接口。

表名是固定规则的输出，不包含原始 runId；该函数只计算名称，不验证 ID 或查询数据库。按时间清理需遍历全部 20 张表。本版不自动迁移旧单表或做双写；若之前已创建旧表，需要先迁移旧数据再切换。分表数或哈希规则改变同样需要数据迁移，不能只改配置。

## 本地验证

仓库根目录运行，无需模型 Key、业务配置或远端数据库：

```bash
go run ./sdk/examples/trace-gorm > /tmp/luban-trace.ndjson
go test -race ./sdk/tracegorm/...
node --test sdk/trace-viewer/trace-data.test.js
```

示例使用假模型和临时 SQLite，输出结构化结果到 stderr，原始 Trace 导出到 stdout，结束删除临时数据库。

开发契约见 [Spec](../../docs/specs/record-sdk-trace.md)；record 的宿主适配另见 [接入指南](../../docs/integration/record-sdk.md)。
