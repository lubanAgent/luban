package server

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lubanAgent/luban/router"
)

// Renderer chooses the HTTP response protocol for an Agent invocation.
type Renderer func(ctx *gin.Context, output any, err error)

func defaultRenderer(ctx *gin.Context, output any, err error) {
	if err == nil {
		ctx.JSON(http.StatusOK, gin.H{"errNo": 0, "errMsg": "", "data": output})
		return
	}
	status, message := http.StatusInternalServerError, "Agent execution failed"
	if _, ok := err.(router.InputError); ok {
		status, message = http.StatusBadRequest, "Invalid request input"
	}
	ctx.JSON(status, gin.H{"errNo": 1, "errMsg": message, "data": gin.H{}})
}

// HTTPHandler creates a synchronous handler with automatic first-node binding.
func (app *App) HTTPHandler(agentKey string) (gin.HandlerFunc, error) {
	found := false
	for _, key := range app.runtime.AgentKeys() {
		if app.excludedHTTP[key] {
			continue
		}
		if key == agentKey {
			found = true
		}
	}
	if !found {
		return nil, fmt.Errorf("Agent %q was not loaded", agentKey)
	}
	return router.AgentHandler(app.runtime, agentKey, app.render, func(ctx *gin.Context, err error) {
		app.logger.Errorf(ctx, "Agent %s execution failed: %v", agentKey, err)
	}), nil
}

// RegisterHTTP mounts all configured Agents after a separate Gin route preflight.
// Conflicts return an error before the caller's router is modified.
func (app *App) RegisterHTTP(engine *gin.Engine) error {
	if engine == nil {
		return fmt.Errorf("Gin engine must not be nil")
	}
	app.routesMu.Lock()
	defer app.routesMu.Unlock()
	if app.registered {
		return fmt.Errorf("Agent HTTP routes already registered")
	}
	if app.config.HTTP.AutoRegister != nil && !*app.config.HTTP.AutoRegister {
		return nil
	}
	handlers := make(map[string]gin.HandlerFunc)
	for _, key := range app.runtime.AgentKeys() {
		if app.excludedHTTP[key] {
			continue
		}
		handler, err := app.HTTPHandler(key)
		if err != nil {
			return err
		}
		handlers[app.config.HTTP.RoutePrefix+"/"+key] = handler
	}
	if err := preflightRoutes(engine, handlers); err != nil {
		return err
	}
	for _, key := range app.runtime.AgentKeys() {
		path := app.config.HTTP.RoutePrefix + "/" + key
		if app.excludedHTTP[key] {
			continue
		}
		engine.POST(path, handlers[path])
	}
	app.registered = true
	return nil
}

func preflightRoutes(engine *gin.Engine, handlers map[string]gin.HandlerFunc) (err error) {
	defer func() {
		if recover() != nil {
			err = fmt.Errorf("Agent HTTP route conflicts with an existing route")
		}
	}()
	probe := gin.New()
	noop := func(*gin.Context) {}
	for _, route := range engine.Routes() {
		probe.Handle(route.Method, route.Path, noop)
	}
	for path := range handlers {
		probe.POST(path, noop)
	}
	return nil
}
