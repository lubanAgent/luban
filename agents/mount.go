package agents

import (
	"context"
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/lubanAgent/luban/agents/admissions/worker"
	"github.com/lubanAgent/luban/server"
)

// Mount enables only the background Agents listed in conf/luban.yaml.
// Most Agents need no mount code: JSON-only Agents get automatic HTTP/CLI,
// and ordinary functions/Tools only need an import in imports.go.
func Mount(ctx context.Context, engine *gin.Engine, app *server.App) error {
	if ctx == nil || engine == nil || app == nil {
		return fmt.Errorf("Agent mount requires context, Gin engine and server app")
	}
	for _, key := range app.BackgroundAgents() {
		switch key {
		case "admissions":
			if err := worker.Mount(ctx, engine, app); err != nil {
				return err
			}
		default:
			return fmt.Errorf("background Agent %q has no worker implementation", key)
		}
	}
	return nil
}
