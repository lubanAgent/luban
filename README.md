# 鲁班（Luban）

鲁班是一套面向 Go 业务研发的 AI Agent 开发框架。目标是让常见的 AI 流程有一套简单、固定的写法：配置执行顺序，注册必要的业务函数、Tool 和 Skill，接入现有模型网关，就能在业务的 HTTP、MQ 或定时任务入口执行 Agent。业务不必先掌握 Eino 的各种编排方式；鲁班在内部使用 Eino，统一处理模型调用循环、工具调用和节点传值。

鲁班让业务保留自己熟悉的代码：取数、领域规则、持久化、对外发送与模型网关 HTTP 调用仍由业务实现。SDK 负责顺序调度、输入输出适配、Agent/子 Agent、Tool/Skill 接入和执行记录。它适合明确的顺序流程，不打算覆盖所有通用工作流能力。

> 当前是开发中的 SDK，尚未发布正式版本。公开模块为 `github.com/lubanAgent/luban/sdk`，Go 包名为 `luban`。仓库中的接入示例以源码和离线测试为准；正式版本号与开源许可证将在首次正式发布前确定。

## 能做什么

| 能力 | 当前行为 |
| --- | --- |
| Agent 编排 | 一套配置中的 `flow` 按顺序执行；业务函数节点负责业务逻辑，Agent 节点负责模型任务。 |
| 模型与工具 | Agent 可独立配置模型、`systemPrompt`、温度和 `maxLoopCount`；模型决定调用哪些已注册的 Tool。 |
| Skill | 静态 Skill 可由业务注册；也可通过 `SkillBackend` 在运行时提供；Agent 按需加载。 |
| 子 Agent | 父 Agent 负责选择并移交给一个子 Agent；子 Agent 使用自己的模型、提示词、Tool 和 Skill。 |
| 数据传递 | 相邻节点传递函数返回值；业务准备 `Request.Messages` 作为动态 userPrompt/媒体输入；`Output[T]()` 可声明结构化结果。 |
| Batch | 多条输入并发处理，按输入下标收集各项结果和错误。 |
| 业务入口 | 提供 Gin HTTP、MQ 回调、Cron 适配器，也可直接调用 `Runtime.Run`。 |
| Trace | 可选 `TraceWriter` 接收执行事件；本地查看页按 Run 展示节点、模型、Tool、Skill 与输入输出。 |

当前没有配置分支或回跳、跨请求暂停恢复、远端配置平台和 Trace 服务。模型请求由业务提供的 `ModelInvoker` 发往已有网关，SDK 不内置厂商 HTTP 客户端，也不依赖公司内部包。

## 快速体验

仓库带有**无需网络和模型密钥**的示例，展示 `PrepareReview → Agent → SaveReview` 的完整传值：

```sh
cd sdk
go test -run '^ExampleRuntime_Run$' -v
```

源码见 [可运行示例](sdk/example_test.go)。一个 Agent 的配置如下；第一个函数构造模型消息，模型输出再交给最后一个函数：

```json
{
  "lessonReview": {
    "flow": [
      { "func": "PrepareReview", "description": "准备课程资料" },
      { "modelName": "reviewModel", "systemPrompt": "请简要评价课程内容。" },
      { "func": "SaveReview", "description": "保存评价" }
    ]
  }
}
```

业务启动时加载配置，使用 `luban.New(luban.Options{Config: config, Models: models})` 创建 Runtime，再调用 `rt.Register(funcs, tools, skills)`；在业务入口执行 `rt.Run(ctx, "lessonReview", input)`。模型适配器只做一轮请求与响应协议转换，Tool 循环由框架处理。详细代码和接入步骤见 [快速接入](docs/快速接入.md)。

## 执行记录

业务可选实现 `TraceWriter`，把完成的 Run、函数、Agent、模型、Tool、Skill 和 Batch 事件写入现有日志；用 `luban.RunID(ctx)` 关联同一次执行。日志含实际输入输出，可能包含用户消息和 Skill 正文，应按业务环境管理。

[本地 Trace 查看页](sdk/trace-viewer/index.html)无需服务端，支持粘贴或选择日志、按 runId 查看执行树和时间线。它是诊断原型，不是持久化 Trace 服务；[查看页说明](sdk/trace-viewer/README.md)介绍日志格式与展示规则。

## 开发与参与

SDK 在独立的 `sdk/go.mod` 中，Go 最低版本以该文件为准。仓库根目录运行：

```sh
make check-sdk
```

命令检查格式、模块依赖、单元与竞态测试、`go vet` 和公司内部依赖边界。Trace 页面解析器另用 `node --test sdk/trace-viewer/trace-data.test.js` 检查。[参与开发](CONTRIBUTING.md)说明本地验证和提交要求。
