// Package luban 提供构建 AI Agent 的简洁 Go 接口。服务提供配置和可选的业务
// 函数、Tool、Skill 与模型网关适配器；Runtime 内部使用 Eino 执行顺序流程。
//
// SDK 自带默认模型协议客户端，也允许服务按需注入业务模型客户端；业务数据
// 读写和外部副作用由宿主模块实现。参见仓库根目录的中文接入指南。
package luban
