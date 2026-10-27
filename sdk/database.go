package luban

import (
	"context"
	"fmt"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// DB returns a connection injected into this Run. The caller owns its schema
// and transactions; Luban does not migrate or close injected databases.
func DB(ctx *gin.Context, name string) (*gorm.DB, error) {
	data := dataFromGin(ctx)
	if data == nil {
		return nil, fmt.Errorf("luban: DB requires an active Run")
	}
	data.mu.RLock()
	defer data.mu.RUnlock()
	if data.closed {
		return nil, fmt.Errorf("luban: DB called after Run completed")
	}
	db := data.databases[name]
	if db == nil {
		return nil, fmt.Errorf("luban: database %q was not configured", name)
	}
	return db, nil
}

// GinContext retrieves the Gin context attached to an outgoing model request.
// HTTPDoer implementations can use it to forward request-scoped log metadata.
func GinContext(ctx context.Context) (*gin.Context, bool) {
	current, err := ginContextFrom(ctx)
	return current, err == nil
}

const taskContextGinKey = "__luban_task_context_v1"

// BindContext supplies cancellation for a non-HTTP task without fabricating an
// HTTP request. HTTP Runs use their real Request.Context instead.
func BindContext(ctx *gin.Context, parent context.Context) error {
	if ctx == nil || parent == nil {
		return fmt.Errorf("luban: task Gin context and standard context must not be nil")
	}
	ctx.Set(taskContextGinKey, parent)
	return nil
}

// Context returns the request/task cancellation context of a Gin invocation.
func Context(ctx *gin.Context) context.Context {
	if ctx != nil {
		if ctx.Request != nil {
			return ctx.Request.Context()
		}
		if value, ok := ctx.Get(taskContextGinKey); ok {
			if parent, ok := value.(context.Context); ok {
				return parent
			}
		}
	}
	return context.Background()
}
