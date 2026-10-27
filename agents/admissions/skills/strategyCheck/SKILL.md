---
description: 检查需求、字段、算分组合、路径、学校和最终提交的一致性。
sourceCommit: 4c3f7b43fe39a00346680d47f8f91850bb736f8a
source: Main/S1/S2/S3/SchoolPicker 检查章节；本轮任务一致性契约
---
# 当前任务的检查与修复

这份检查表没有新增招生政策。完整规则、例子和反例分别在五份领域 Skill。
checkStrategyDraft 是服务端检查入口，必须使用当前 expectedRevision。
业务问题通过 issues 返回，可修改后重新检查；基础设施、权限和租约错误终止本轮。

| 检查域 | 完整来源 | 当前应检查的内容 |
| --- | --- | --- |
| 需求完整性 | requirementClarify §5、§7、§12 | 三段完整 brief、真正缺口、准确数值/数量、用户规则来源，最多追问两个问题 |
| 字段和资格 | formAndEligibility §4、§7～9 | 字段唯一、五类类型、合法数据源、选项真实值、条件引用和 action |
| 算分与分支 | scoreCalculation §5～10 | weighted 完整 inputs/offset、公式名、outputKey、区域×身份全部组合唯一覆盖 |
| 路径与板块 | resultDisplay §4、§8～15 | 节点引用、可达性、无环、公式匹配、板块原名、数组顺序、quota 与条件 |
| 学校规则 | schoolRecommendation 全文；resultDisplay §5～7 | 真实学校唯一解析、学校范围、分数/排名 diff 方向、闭区间、排序、去重、累积缺额 |

本轮新接入的一致性约束：

1. chat 只能完整替换 requirements；generate 不能改变确认需求。编辑只能使用本任务草稿。
2. editStrategyDraft 每次恰好替换一个部分。用 expectedRevision 检查并发冲突，成功后读取新 revision。
3. 任何编辑都使之前的 checkId 失效。检查通过不代表可继续修改后沿用旧凭据。
4. readyToConfirm 和 generated 必须先通过检查，再引用返回的 checkId 与相同 draftRevision。
5. __runtime_output 是终止工具。提交后才发现凭据错误时，本轮不能继续修复；不要提前提交。
6. 不在最终输出中再交另一份 DSL，避免检查的是 A、保存的是 B。
7. chat 最终只能 reply/readyToConfirm；generate 只能 generated/needsInput。没有公开发布工具。
8. needsInput 是业务缺口，不可让模型凭常识补政策后改报 generated。
9. simulateStudent 仅推演已检查草稿。后端解释器是待与学生页面对照的候选实现，不能称页面已验证。

当前明确不放行的消费者语义：

- 小数权重、平均数与小数 lookup：不能用 0.33 冒充 1/3，不做隐式取整。
- 跨字段 validation：旧条件真假方向未核对，不能反过来使用。
- 不同人群的成绩表单显隐/必填：不等于公式 inputs 不同；不能用全部必填代替。
- probability：没有算法时不能回退 score。

检查凭据只证明当前版本通过指定的确定性检查，不能证明模型百分之百理解用户原话。
最终需求仍要由人确认，完整复杂案例和真实页面效果仍要验收。
