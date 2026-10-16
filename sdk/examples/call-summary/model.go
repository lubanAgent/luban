package main

import (
	"github.com/gin-gonic/gin"
	luban "github.com/lubanAgent/luban/sdk"
)

// demoModel 只用于体验 SDK：忽略输入，返回固定文本，不调用真实模型。
// 接业务时替换成自己的 ModelInvoker；JSON 中的 modelName 对应 Models 的 key。
type demoModel struct{}

func (demoModel) Name() string { return "demo" }

func (demoModel) Invoke(_ *gin.Context, _ luban.ModelRequest) (luban.ModelResponse, error) {
	return luban.ModelResponse{
		Content: []luban.ModelPart{{
			Type: luban.PartText,
			Text: "客户想了解周末课程，顾问将于明天下午发送课程安排并电话确认。",
		}},
	}, nil
}
