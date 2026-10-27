// Package lubanhttp adapts a Luban Runtime to an existing Gin router.
package lubanhttp

import (
	"bytes"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	luban "github.com/lubanAgent/luban/sdk"
)

// Renderer writes the existing business module's success or failure response.
type Renderer func(ctx *gin.Context, output any, err error)

// InputError marks a request rejected before the Agent starts.
type InputError struct{ Err error }

func (e InputError) Error() string { return e.Err.Error() }
func (e InputError) Unwrap() error { return e.Err }

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
		if ctx.Request == nil {
			render(ctx, nil, InputError{Err: fmt.Errorf("lubanhttp: HTTP request is missing")})
			return
		}
		var raw []byte
		if ctx.Request.Body != nil {
			var err error
			raw, err = io.ReadAll(http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 16<<20))
			if err != nil {
				render(ctx, nil, InputError{Err: fmt.Errorf("request body is unreadable or too large")})
				return
			}
		}
		// A first function taking only Gin still owns its body parsing.
		ctx.Request.Body = io.NopCloser(bytes.NewReader(raw))
		input, err := rt.DecodeInput(appKey, raw)
		if err != nil {
			render(ctx, nil, InputError{Err: err})
			return
		}
		output, err := rt.Run(ctx, appKey, input)
		if err != nil {
			render(ctx, nil, err)
			return
		}
		render(ctx, output, nil)
	}
}

// SubmitHandler binds a business command that creates durable work and returns
// immediately. The caller owns persistence and the later Runtime.Run; this
// adapter only decodes the HTTP input and renders the submission result.
func SubmitHandler[Input, Output any](submit func(*gin.Context, Input) (Output, error), render Renderer) gin.HandlerFunc {
	if submit == nil {
		panic("lubanhttp: Submit must not be nil")
	}
	if render == nil {
		panic("lubanhttp: Renderer must not be nil")
	}
	return func(ctx *gin.Context) {
		if ctx == nil {
			render(nil, nil, fmt.Errorf("lubanhttp: Gin context must not be nil"))
			return
		}
		var input Input
		if err := ctx.ShouldBindJSON(&input); err != nil {
			render(ctx, nil, fmt.Errorf("lubanhttp: invalid JSON input: %w", err))
			return
		}
		output, err := submit(ctx, input)
		if err != nil {
			render(ctx, nil, err)
			return
		}
		render(ctx, output, nil)
	}
}
