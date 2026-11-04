# 空输入跳过模型节点：开发验证

- 日期：2026-11-03
- 分支：`codex/record-sdk-trace`
- 范围：只修改 Luban；保留该分支已有的 Trace Gorm 改动，未修改 record，未部署。

## 行为与边界

- 空 `Request.Messages` 且提供有效 `Output[T]()`：跳过当前模型节点，向下一业务函数交付精确类型 T 的零值。指针、命名 slice/map 的 typed nil 仍作为有值的节点结果传递。
- 空 `Batch.Items` 且提供有效 `Output[T]()`：交付 `BatchResult[T]`，`Items` 为非 nil 空切片；后续业务函数继续执行。缺少有效 Output 时返回包含 `ErrEmptyBatch` 的错误，负并发依然报错。
- 只有整个 Request/Batch 的消息或项数为零才跳过。非空 Batch 内的空消息项仍走原执行路径；非空结构化输出继续使用内置 `__runtime_output`。
- 跳过仅发生在该模型节点：不构造模型、Tool、Skill、子 Agent 或 OutputTool 调用。Trace 的外层 flow Agent 事件写 `status=skipped`、`skipReason=empty_input`，不产生虚假的内部 span；Run 完成事件由后续执行结果决定。`tracegorm` 的状态校验和离线查看器已接受并显示此状态，原始事件 JSON 保留跳过原因，表结构无需变更。
- 下一业务函数必须根据自己的业务目标清单判断空结果是否合理；SDK 不负责推断评分或修复规则。

## 验证

新测试先于实现运行时，空 Batch 返回 `ErrEmptyBatch`，空 Request 调用了模型，观察到预期失败；实现后新增正向链路和原有负向路径均通过。新增用例位于 `sdk/empty_input_test.go`；数据库 Trace 与查看器兼容性用例也已补充。

| 实际执行的命令 | 结果 |
| --- | --- |
| `go test -race ./sdk/...` | 通过；包含真实 Runtime 条件链、连续跳过、并发 Run、取消和原有 SDK 回归 |
| `node --test sdk/trace-viewer/trace-data.test.js` | 通过，14 项；含跳过事件解析 |
| `go vet ./sdk/...`、`git diff --check` | 通过 |
| `make check` | 通过；格式、依赖差异、全仓普通/race 测试、vet、构建 |

用例还覆盖 nil 与非 nil 空切片、struct/指针/命名 slice/map 的输出类型、最终节点返回、RunFrom、空输入非法 OutputSpec 与负并发、错误输入与相邻类型不匹配、空文本但非空消息、非空 Batch 项、Tool/Skill/子 Agent 不执行，以及非空请求的非法输出/缺失 OutputTool 报错。

未调用真实模型网关，未在 record 或目标数据库联调。已有 Trace Gorm 的 SQLite/DryRun 验证不能替代 trace_store 的 MySQL/OceanBase 验收；接入方还需验证实际业务判断及部署环境。
