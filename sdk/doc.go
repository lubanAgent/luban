// Package luban 提供构建 AI Agent 的简洁 Go 接口。业务模块提供配置、模型网关
// 适配器、业务函数、Tool 和 Skill；Runtime 内部使用 Eino 执行顺序流程。
//
// SDK 可以用离线模型独立验证。接入真实业务时，模型 HTTP 请求、业务数据读写
// 和外部副作用仍由宿主模块实现；参见仓库根目录的中文接入指南。
package luban
