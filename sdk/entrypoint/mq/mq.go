// Package lubanmq adapts typed messages from an existing MQ consumer.
package lubanmq

import (
	"github.com/gin-gonic/gin"
	luban "github.com/lubanAgent/luban/sdk"
)

// Handler returns a synchronous callback for an existing message consumer.
// The consumer remains responsible for acknowledgements and retries.
func Handler[T any](rt *luban.Runtime, appKey string) func(ctx *gin.Context, message T) error {
	if rt == nil {
		panic("lubanmq: Runtime must not be nil")
	}
	if appKey == "" {
		panic("lubanmq: appKey must not be empty")
	}
	return func(ctx *gin.Context, message T) error {
		_, err := rt.Run(ctx, appKey, message)
		return err
	}
}
