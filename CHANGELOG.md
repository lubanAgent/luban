# Changelog

## Unreleased

- 新增每次 Run/RunFrom 独立的 `luban.Set/Get` 数据容器；普通 Tool 与业务节点共享，Batch 只读批次快照，执行结束后清理。
- 修复 Tool 调用 `Stop` 可影响整个 flow 的问题。现在返回带工具名的错误；Tool 内自行 recover 也不能把它变成正常结束，Batch 沿单项 Err 处理。
- 将 flow 停止权限限制在当前业务函数调用期间，保留正常 Stop、错误与取消规则。
- 完善 README、接入指南和可运行通话摘要；新增原生 Eino 等价示例与对照说明，检查脚本校验两者输出。

以上为未发布工作区变更，不代表稳定版本已经发布。
