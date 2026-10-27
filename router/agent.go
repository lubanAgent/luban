package router

import (
	"errors"

	"github.com/gin-gonic/gin"
	luban "github.com/lubanAgent/luban/sdk"
	lubanhttp "github.com/lubanAgent/luban/sdk/entrypoint/http"
)

// InputError distinguishes an invalid HTTP request from a failed Agent Run.
type InputError = lubanhttp.InputError

// AgentHandler receives one request and invokes the configured Agent.
// The service selects its response protocol and logging policy.
func AgentHandler(rt *luban.Runtime, key string, render func(*gin.Context, any, error), onError func(*gin.Context, error)) gin.HandlerFunc {
	return lubanhttp.Handler(rt, key, func(ctx *gin.Context, output any, err error) {
		var invalid InputError
		if err != nil && !errors.As(err, &invalid) && onError != nil {
			onError(ctx, err)
		}
		render(ctx, output, err)
	})
}

// RegisterSubmission binds an asynchronous business entry using the SDK's
// HTTP adapter; its callback persists work without running the Agent inline.
func RegisterSubmission[Input, Output any](engine *gin.Engine, path string, submit func(*gin.Context, Input) (Output, error), render lubanhttp.Renderer) {
	engine.POST(path, lubanhttp.SubmitHandler(submit, render))
}
