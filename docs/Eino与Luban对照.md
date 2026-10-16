# 同一个通话摘要：Eino 与 Luban

**Eino 能简洁地完成这个任务。Luban 提供的是固定接入规则：用配置排列节点，统一函数签名、模型适配和业务入口。**

这个最小案例帮助理解两种写法，不用它宣称节省了多少代码或研发时间。

## 跑同一个需求

两边均执行：HTTP JSON → 准备通话文本 → 模拟模型 → `{"summary":"…"}`。输入、固定提示词、固定回复、请求校验和 HTTP 响应规则一致；都不调用真实模型、数据库或监听端口。

在 `sdk` 目录执行：

```sh
go run ./examples/call-summary
go run ./examples/eino-call-summary
```

两边都应输出：

```text
输出：{"summary":"客户想了解周末课程，顾问将于明天下午发送课程安排并电话确认。"}
```

`make check-sdk` 会执行并校验两份结果。这是离线接线验证，不是模型质量、性能或人工上手时间测试。

## 重点看哪里

| 工作 | Eino 原生实现 | Luban 实现 |
| --- | --- | --- |
| 表达流程 | Go 中使用 Chain、Lambda 和 ChatModel 节点 | JSON `flow` 引用已注册函数和模型 |
| 准备动态材料 | 函数返回 `[]*schema.Message` | Gin 业务函数返回 `luban.Request` |
| 接收模型答案 | 后函数接收 `*schema.Message` 并取 Content | 后函数直接接收 string |
| 固定提示词 | 本例在准备函数中写 SystemMessage；也可以自行配置化 | JSON 中的 `systemPrompt` |
| 模型适配 | 本例实现 `model.BaseChatModel`；项目也可复用 Eino 现成组件 | 实现 `luban.ModelInvoker`；复用自己的网关客户端 |
| 启动 | 组装、Compile，复用 Runnable | 读 JSON、New、Register，复用 Runtime |
| HTTP 入口 | 调用 Runnable.Invoke，把结果交给 render | 绑定 SDK HTTP Handler，内部调用 Run |
| 共同工作 | 请求校验、业务规则、网关鉴权与响应规范都需要业务定义 | 同左 |

两边完整源码都保留了初始化、输入处理、错误处理和模拟模型，没有只展示一方的注册片段：

- Eino：[完整入口与节点](../sdk/examples/eino-call-summary/main.go)、[模拟模型](../sdk/examples/eino-call-summary/model.go)。
- Luban：[完整入口与节点](../sdk/examples/call-summary/main.go)、[配置](../sdk/examples/call-summary/agent.json)、[模拟模型](../sdk/examples/call-summary/model.go)。

原生 Eino 的核心连接只有几行，**不需要为三个普通步骤手写三套 ADK Agent**：

```go
compose.NewChain[*gin.Context, CallSummary]().
    AppendLambda(compose.InvokableLambda(PrepareCall)).
    AppendChatModel(demoModel{}).
    AppendLambda(compose.InvokableLambda(BuildResult)).
    Compile(context.Background())
```

Luban 将这份连接关系写进配置，业务函数登记一次：

```json
{
  "callSummary": {
    "flow": [
      {"func": "PrepareCall"},
      {"modelName": "demo", "systemPrompt": "用一句话总结通话中的诉求和后续安排。"},
      {"func": "BuildResult"}
    ]
  }
}
```

```go
rt.Register(map[string]any{
    "PrepareCall": PrepareCall,
    "BuildResult": BuildResult,
}, nil, nil)
```

上面是阅读片段；可运行版本包含 JSON 加载、New 和全部错误处理。

## 新增或调整一个 Agent

| 改动 | Eino | Luban |
| --- | --- | --- |
| 调整提示词或模型 | 修改当前应用的装配代码或其自建配置 | 修改已有字段；模型调用器需已登记 |
| 插入一个业务步骤 | 新函数接入 Chain/Lambda，处理输入输出类型 | 登记符合签名的函数，再在 flow 中引用 |
| 第二个类似流程 | 可复用模型、函数、Chain 构建器和入口助手 | 可复用模型、函数、Tool、Skill、Writer，增加配置和入口绑定 |

Luban 没有消除业务开发。它把团队重复选择的部分做成固定规则，使新增类似需求时可以沿用同一套接入方式；代价是接受 Gin 首参、顺序 flow、运行时引用检查等约束。配置在本地启动时加载，修改 JSON 后需要重新初始化，当前不承诺热更新。

只有一次模型调用时，直接使用 Eino 也很直接。需要自由设计 Graph、分支、流式或恢复方案时，应评估原生 Eino；已有 Go/Gin 项目希望统一顺序 Agent、Tool/Skill、结果交付和日志接入时，可以评估 Luban。

## 本例没有证明什么

本例不包含 Tool、Skill、OutputTool、子 Agent 或 Trace，不据此给这些组合的开发量下结论。后续若比较完整组合，两边都应允许复用组件，采用相同的模型最终提交协议、工具错误规则和日志范围；不能用 JSON Parser 冒充 OutputTool 的完整终止与取值语义。

也没有做陌生研发接入试用、真实网关验收或性能测试，暂不提供节省行数、百分比或时间的承诺。

本例依据 SDK 当前锁定的 Eino v0.8.13，使用其公开 `Chain.AppendLambda`、`AppendChatModel`、`Compile` 和 `model.BaseChatModel`。上游源码核对地址：

```text
https://github.com/cloudwego/eino/blob/v0.8.13/compose/chain.go
https://github.com/cloudwego/eino/blob/v0.8.13/components/model/interface.go
```
