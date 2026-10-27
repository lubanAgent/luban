package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gin-gonic/gin"
)

// SetupHTTP installs server infrastructure before Agent/business routes.
// A host hook replaces the default setup, so its health routes and login scope
// remain under host control. This method must be called once per App.
func (app *App) SetupHTTP(engine *gin.Engine) error {
	if engine == nil {
		return fmt.Errorf("server: Gin engine must not be nil")
	}
	app.routesMu.Lock()
	defer app.routesMu.Unlock()
	if app.httpSetup {
		return fmt.Errorf("server: HTTP setup already called")
	}
	if app.registered {
		return fmt.Errorf("server: HTTP setup must precede Agent route registration")
	}
	app.httpSetup = true
	if app.setupHTTP != nil {
		return app.setupHTTP(engine)
	}
	engine.Use(gin.Recovery())
	engine.GET("/ready", func(ctx *gin.Context) { ctx.String(http.StatusOK, "success") })
	return nil
}

// Serve delegates listening and shutdown to the selected implementation.
// Only the default implementation installs Go signal handling; injected
// servers (such as golib) own their listener and signal lifecycle themselves.
func (app *App) Serve(engine *gin.Engine) error {
	if engine == nil {
		return fmt.Errorf("server: Gin engine must not be nil")
	}
	if app.serveHTTP != nil {
		return app.serveHTTP(app.Address(), engine)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGQUIT)
	defer cancel()
	app.logger.Infof(nil, "Luban listening on %s", app.Address())
	return Serve(ctx, app.Address(), engine)
}
