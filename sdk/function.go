package luban

import (
	"fmt"

	"github.com/lubanAgent/luban/sdk/internal/executor"
)

type registeredFunction = executor.Function

func inspectFunction(key string, fn any) (registeredFunction, error) {
	return executor.Inspect(key, fn)
}

func inspectTool(tool Tool) (name, description string, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("metadata method panicked: %v", recovered)
		}
	}()
	return tool.Name(), tool.Description(), nil
}
