package luban

import (
	"fmt"

	"github.com/gin-gonic/gin"
)

// Tool is a business capability that may be exposed to an Agent.
type Tool interface {
	Name() string
	Description() string
	Input() any
	Invoke(ctx *gin.Context, input any) (any, error)
}

func readToolInput(businessTool Tool) (input any, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			input = nil
			err = fmt.Errorf("panicked: %v", recovered)
		}
	}()
	return businessTool.Input(), nil
}
