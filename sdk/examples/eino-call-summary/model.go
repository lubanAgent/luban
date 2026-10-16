package main

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// 同一段固定回复，不接真实网关，不据此比较模型质量或性能。
type demoModel struct{}

var _ model.BaseChatModel = demoModel{}

func (demoModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	return schema.AssistantMessage("客户想了解周末课程，顾问将于明天下午发送课程安排并电话确认。", nil), nil
}

func (demoModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, fmt.Errorf("demo model does not support streaming")
}
