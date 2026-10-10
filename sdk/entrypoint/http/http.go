// Package lubanhttp adapts a Luban Runtime to an existing Gin router.
package lubanhttp

import (
	"fmt"

	"github.com/gin-gonic/gin"
	luban "github.com/lubanAgent/luban/sdk"
)

// Renderer writes the existing business module's success or failure response.
type Renderer func(ctx *gin.Context, output any, err error)

// Handler creates a synchronous Gin handler that runs one configured Agent.
// Invalid construction arguments panic because they indicate a startup error.
func Handler(rt *luban.Runtime, appKey string, render Renderer) gin.HandlerFunc {
	if rt == nil {
		panic("lubanhttp: Runtime must not be nil")
	}
	if appKey == "" {
		panic("lubanhttp: appKey must not be empty")
	}
	if render == nil {
		panic("lubanhttp: Renderer must not be nil")
	}
	return func(ctx *gin.Context) {
		if ctx == nil {
			render(nil, nil, fmt.Errorf("lubanhttp: Gin context must not be nil"))
			return
		}
		output, err := rt.Run(ctx, appKey, nil)
		if err != nil {
			render(ctx, nil, err)
			return
		}
		render(ctx, output, nil)
	}
}
