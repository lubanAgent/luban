# Luban Trace 查看页

本页随 SDK 源码维护：[index.html](index.html)。本机打开页面，粘贴业务日志中的 `luban_trace` JSON 行，点击“解析 Trace”，再按 runId 查看可展开的调用树、时间瀑布和节点输入输出。也可选择本地日志文件；普通业务日志行会被跳过。支持一行一个 `TraceEvent`、一行一个包含 `traceEvent` 字段的日志对象，以及完整的 JSON 数组。

“查看演示数据”会加载**明确标记的模拟事件**，只供预览布局，不代表真实 Agent 执行结果。页面会提示缺失父节点、重复 spanId 和缺失 Run 根事件，无法判断日志系统是否丢掉了未收到的其他事件。Token 没有来源时显示“未提供”。

新 Trace 事件会把配置 `description` 放在业务函数/Agent 节点上：调用树显示业务描述，下一行保留技术名称，详情保留完整说明。旧日志没有该字段时，可以展开导入区的“附加 Agent 配置 JSON”，粘贴或选择对应的 `luban_agents.json` 后重新解析；配置最外层直接是 appKey，不使用 `agents` 包装层。如果配置里有多个 Agent 且日志缺失 Run 根节点，页面无法判断该用哪个 appKey，须等待完整日志。缺失父节点会显示为明确标记的占位层级，例如 `s_12` 未收到时 Skill 归在其下，但占位不是一次真实执行事件。

父选择 Agent 移交后即结束；子 Agent 与父 Agent 同在外层 flow Agent 节点下，模型/Skill/Tool 在实际执行的 Agent 下。新版 SDK 用 `transferFromSpanId` 表示移交。旧日志若把子 Agent 记在父 Agent 下，页面仅在实际 `transferTo` 和时间顺序均匹配时按同级展示；详情仍显示原始 `parentSpanId` 和展示父级。

调用树会折叠成功且只包含实际 Agent 调用的外层 flow 适配节点：前置检查只显示一次，选择 Agent 与子 Agent 并列。原始事件不会删除，事件总数仍以日志为准；选中实际 Agent 可在详情中展开外层步骤的耗时和输入输出。外层步骤失败或还有其他类型子节点时保持可见，避免遮住错误和独立工作。

页面只读取本机粘贴或选取的内容，使用同目录的 CSS/JavaScript，没有远端脚本、上传请求或持久化缓存。Trace 可能包含用户消息、Tool 参数、Skill 正文；仅使用授权的测试日志，勿粘贴到公共网站。

解析器离线测试：

```sh
cd sdk/trace-viewer
node --test trace-data.test.js
```
