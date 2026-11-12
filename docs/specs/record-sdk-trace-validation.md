# Luban Trace 持久化：实施与验证

- 日期：2026-11-02
- 分支：`codex/record-sdk-trace`，基于 `632b45e`。
- 范围：只开发 Luban；未修改 record，未连接真实模型或数据库。

## 2026-11-03：驼峰表名与 20 张 Span 分表

已将 Run 表命名为 `tblLubanTraceRun`，Span 表固定为 `tblLubanTraceSpan0`～`tblLubanTraceSpan19`。完整 runId 的 CRC32 IEEE 值取模 20；`tracegorm.SpanTable(runID)` 供读取复用。同 Run 的事件同表，批次跨表写入和 Run 摘要仍在同一事务。New 当前只装配参数和 Gorm 句柄，不检查全部 21 张表，不自动创建或迁移。

新增分表测试先出现编译失败（尚无分表函数），接入路由后确认旧单表 DDL 导致 `no such table: tblLubanTraceSpan0`；再更新 DDL 和写入查询。没有连接目标库或执行真实建表 SQL。

| 检查 | 实际结果 |
| --- | --- |
| `go test ./sdk/tracegorm -run 'TestSpanShard\|TestCrossShard' -count=1` | 通过；独立 CRC32 固定样例、20 表路由、同 Run 聚合、重复投递、缺第 19 号表拒绝启动 |
| `go test -race ./sdk/tracegorm -count=1` | 通过；包含真实 Runtime 的并发 Run、Batch、Tool/Skill/跳过事件及持久化 |
| `TestCrossShardFailureRollsBackWholeBatch` | 第 17 号分表或 Run 摘要失败均整批回滚；解除故障后重复重试不丢失、不重复 |
| `TestMySQLWriteUsesExactDuplicateNoop` | MySQL DryRun SQL 依次写入 0、17、Run；保留精确重复键语义 |
| `go run ./sdk/examples/trace-gorm > /tmp/luban-trace-sharded.ndjson` | 6 条事件全部 Persisted，Rejected/Failed/Pending 均为 0；按 runId 只读一个分表 |
| 使用 Node 的 `parseLogs/buildRun` 解析上述导出 | 6 条事件、1 次 Run、无缺失父节点 |
| `make check` | 通过；全仓普通/race 测试、格式、依赖差异、vet、构建 |
| `node --test sdk/trace-viewer/trace-data.test.js`、`git diff --check` | 通过；查看器 14 项测试 |

完整建表 SQL、SQLite 离线 schema、Spec、接入说明和示例已同步。下面 2026-11-02 的记录是分表调整前的历史验证。真实 MySQL/OceanBase、旧表数据迁移和 record 联调仍未执行；不会将本地 SQLite、MySQL DryRun 当作目标库验收。

## 2026-11-09：参考 record 表规范扩容字段

只读分析了 Ship `example_record` 中现有的 `tblAiRuntimeTraceEvent`、`tblAiRuntimeRun`、`tblAiRuntimeModelCall` 和 `tblAiPracticeLLMCallLog`。参考库中 Runtime ID 为 `varchar(64)`，状态/类型通常为 `varchar(32)`，Go 类型指纹可到 `varchar(255)`；`create_time`、`created_at`、`started_at` 等时间字段普遍使用 `bigint(20)` Unix 秒，Runtime 表带 `deleted` 软删标记并使用 InnoDB Dynamic。

新的 Luban 表结构将业务 ID 扩到 `varchar(64)`、kind 扩到 `varchar(64)`、status 扩到 `varchar(32)`、名称和业务关联值扩到 `varchar(255)`。所有表补齐 record 公共字段：自增 `id`、Unix 秒 `create_time/update_time`、默认 0 的 `deleted`；`run_id` 和 `(run_id, span_id)` 改为唯一键继续保证幂等。DDL 的普通字段都有明确 `DEFAULT`，每列和每张表都有中文 `COMMENT`；自增 `id` 由数据库生成，`longtext` 类型的 `error_msg/event_json` 按数据库规范必须可空且禁止设置默认值，SDK 每次写入事件时仍显式赋值。DDL 只声明表级 `utf8mb4`，不逐列指定字符集/排序规则或行格式。`started_at` 保留 `datetime(6)` 以支持精确时间索引，默认哨兵值为 epoch，正常写入使用真实事件时间，完整时间仍保存在 event_json。DDL、Spec、README 和离线契约测试已同步；New 不读取目标库 schema，没有执行目标库 DDL。

## 已交付

| 内容 | 位置 |
| --- | --- |
| 可选 writer、Options/Stats/Close、错误与元数据接口 | `sdk/tracegorm/` |
| 事务批写、精确重复键处理、UTC 索引时间 | `sdk/tracegorm/store.go`、`event.go` |
| MySQL 建表 SQL | `sql/trace/001_create_tables.mysql.sql` |
| 接入、日志组合、退出、故障、导出说明 | `sdk/tracegorm/README.md` |
| 假模型 + 临时 SQLite 完整示例 | `sdk/examples/trace-gorm/` |
| record 的独立接入指南 | `docs/integration/record-sdk.md` |

没有改 Runtime 的业务执行语义，没有自动启用根服务数据库 Trace，没有新增依赖或修改 go.mod/go.sum。调用方创建 writer 后显式注入，基础 Gorm 连接仍由宿主管理。

## 已执行检查

| 检查 | 结果与范围 |
| --- | --- |
| `go test ./sdk/tracegorm -count=1` | 通过；普通测试及子测试 |
| `make check` | 通过：格式、`go mod tidy -diff`、全仓测试、全仓 race、vet、build |
| `node --test sdk/trace-viewer/trace-data.test.js` | 原查看页 13 项测试全部通过 |
| 离线示例 | 通过；6 条事件全部 Persisted，Rejected/Failed/Pending 为 0 |
| 示例导出交原解析器 | 通过；1 次 Run、6 条事件、无缺失父节点 |
| 独立 SDK 消费者 | 临时独立 module，Go 1.25.0、本地 replace，构建通过；未配置公司 replace，包依赖不含公司 integration、server 或 agents |
| MySQL SQL | 当前 MySQL dialector 的 DryRun SQL 检查通过；启动不再读取元数据，不代表真实 MySQL 验收 |

测试日志本机保存于 `/tmp/luban-tracegorm-make-check.log`、`/tmp/luban-tracegorm-consumer.log`、`/tmp/luban-tracegorm-example.log`、`/tmp/luban-tracegorm-viewer.log`；导出样例在 `/tmp/luban-tracegorm-example.ndjson`。它们是可重跑的临时证据，不属于发布依赖。

## Spec 验收对应

| 编号 | 自动化证据 |
| --- | --- |
| C01 | 原 SDK/根服务测试、原查看页测试全通过；生产入口未启用新 writer |
| C02 | New 的 nil/无效/事务/DryRun/取消/驱动测试；空库和缺分表不阻塞初始化 |
| C03 | `TestRuntimeToolSkillStructuredOutputDatabaseAndViewer`，真实 SDK/Eino，假模型，完整 SQLite 落库 |
| C04 | 同批两次 Tool 调用、六次并发 Run、每 Run 三项 Batch，验证父子关系及 race |
| C05 | 重复事件保留首次内容；已提交后返回未知错误再重试，行数与统计正确 |
| C06 | SQLite trigger 令 Run 摘要失败，span 与摘要事务全部回滚 |
| C07～C08 | 两次失败后成功、三次失败后继续后批；Runtime 返回值不变，Close 保留错误链 |
| C09 | 在途批次占据数量/字节预算；大事件独占一批；默认 8 MiB 精确边界与超限拒绝 |
| C10～C11 | 并发 WriteTrace/Close、多次 Close、等待超时后继续排空 |
| C12～C13 | 真 Runtime 的错误、Stop、panic、任务取消仍保存对应完成事件；启动 Context 取消不影响已建 writer |
| C14～C15 | Metadata 超长/panic、OnError panic、worker panic，事件/错误/终态均可观察 |
| C16 | 入队后修改 RawMessage、FlowIndex、Gin Keys，不改变数据库快照 |
| C17 | 真 Runtime 父无选择返回 nil；父子移交保存兄弟节点和 TransferFromSpanID |
| C18 | 只发送子 span，不生成成功 Run 摘要 |
| C19 | UTF-8/emoji/长输入、字段长度、UTC 时间与宿主 loc 差异；8 MiB 默认预算边界 |
| C20 | 从 SQLite 原样导出 event_json，用真实 `parseLogs/buildRun` 解析 |
| C21 | 临时独立 Go 1.25.0 消费工程构建及依赖检查 |
| C22 | DDL 与 Spec、私有行映射一致；MySQL 精确主键 no-op SQL，禁止 INSERT IGNORE |
| C23 | 默认值、负数/容量关系、元数据、计数不变量、重复事件不依赖 RowsAffected |

生产数据库功能使用当前驱动生成 SQL 的离线证据，事务行为用 SQLite 验证；New 不读取元数据。目标数据库的实际 SQL 方言、权限和边界容量仍须在首次写入时验证，不能将这份结果解读为已在 trace_store 验收。

## 尚未完成

- T1～T4 的本地实现与自动化已完成。T5 仅完成独立消费者和文档；本轮未发布远端版本，之前的代理/checksum 下载问题未在本轮处理。
- trace_store / MySQL / OceanBase 的 DDL、权限、驱动、事务、索引、packet 和大事件实测未执行。
- record 业务方案、公司 HTTP 装配、业务关联标识、退出挂载与实际调用没有改动。
- 线上容量、保留周期、清理、容器退出和真实模型效果仍待业务接入验证。

实现保持“尽力持久化”：Run 成功不等于 Trace 完整入库，队列拒绝、持续故障、强杀仍可能丢数据。宿主必须保留可见诊断，按实际需要保留全量日志。
