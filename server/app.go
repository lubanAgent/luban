package server

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"reflect"
	"sort"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/lubanAgent/luban/models"
	luban "github.com/lubanAgent/luban/sdk"
	"gorm.io/gorm"
)

// Options supplies assets and optional company adapters. Injected resources
// remain owned by the caller; Close only closes resources created by New.
type Options struct {
	Assets          fs.FS
	Logger          Logger
	HTTPDoer        luban.HTTPDoer
	ProjectModels   map[string]map[string]luban.ModelInvoker
	ProjectDefaults map[string]luban.ModelInvoker
	Databases       map[string]*gorm.DB
	// Resources are shared business clients, retrieved with luban.Resource[T].
	// Their owner handles initialization, shutdown and concurrency safety.
	Resources map[string]any
	// SetupHTTP replaces default probes/middleware. Register infrastructure
	// before applying authentication to subsequently registered business routes.
	SetupHTTP func(*gin.Engine) error
	// ServeHTTP replaces the default TCP listener and signal handling together.
	ServeHTTP func(address string, engine *gin.Engine) error
	// RegisterShutdown joins a host's existing signal mechanism. App.Close
	// also invokes the same callbacks, before closing owned connections.
	RegisterShutdown  func(name string, stop func())
	Renderer          Renderer
	TraceWriter       luban.TraceWriter
	Stdout            io.Writer
	Stderr            io.Writer
	ExcludeHTTPAgents []string
}

// App owns one Runtime shared by every entry point in the service.
type App struct {
	runtime          *luban.Runtime
	config           serviceConfig
	logger           Logger
	render           Renderer
	stdout           io.Writer
	stderr           io.Writer
	closers          []io.Closer
	closeOnce        sync.Once
	closeErr         error
	routesMu         sync.Mutex
	registered       bool
	databases        map[string]*gorm.DB
	excludedHTTP     map[string]bool
	setupHTTP        func(*gin.Engine) error
	serveHTTP        func(string, *gin.Engine) error
	httpSetup        bool
	registerShutdown func(string, func())
	shutdownMu       sync.Mutex
	stops            []func()
	closed           bool
}

// New loads a startup snapshot and validates it. It does not listen on a port
// or send model requests, and pure configuration Agents need no database.
func New(options Options) (_ *App, err error) {
	for name, value := range map[string]any{
		"Logger": options.Logger, "HTTPDoer": options.HTTPDoer,
		"TraceWriter": options.TraceWriter,
	} {
		if value != nil && typedNil(value) {
			return nil, fmt.Errorf("server: injected %s must not be nil", name)
		}
	}
	snapshot, err := loadAssets(options.Assets)
	if err != nil {
		return nil, err
	}
	app := &App{config: snapshot.service, logger: options.Logger, render: options.Renderer, stdout: options.Stdout, stderr: options.Stderr}
	app.setupHTTP, app.serveHTTP = options.SetupHTTP, options.ServeHTTP
	app.registerShutdown = options.RegisterShutdown
	app.excludedHTTP = make(map[string]bool)
	for _, key := range append(snapshot.service.HTTP.ExcludeAgents, options.ExcludeHTTPAgents...) {
		if _, exists := snapshot.agents.Agents[key]; !exists {
			return nil, fmt.Errorf("http excluded Agent %q was not loaded", key)
		}
		app.excludedHTTP[key] = true
	}
	seen := make(map[string]bool)
	for _, key := range snapshot.service.BackgroundAgents {
		if _, exists := snapshot.agents.Agents[key]; !exists || seen[key] {
			return nil, fmt.Errorf("background Agent %q is missing or duplicated", key)
		}
		seen[key] = true
		app.excludedHTTP[key] = true
	}
	if app.stdout == nil {
		app.stdout = os.Stdout
	}
	if app.stderr == nil {
		app.stderr = os.Stderr
	}
	if app.logger == nil {
		app.logger = NewJSONLogger(app.stderr)
	}
	if app.render == nil {
		app.render = defaultRenderer
	}
	defer func() {
		if err != nil {
			_ = app.Close()
		}
	}()
	databases := make(map[string]*gorm.DB, len(options.Databases))
	for name, db := range options.Databases {
		databases[name] = db
	}
	names := make([]string, 0, len(snapshot.service.Databases))
	for name := range snapshot.service.Databases {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if _, exists := databases[name]; exists {
			continue // an injected database overrides the configured connection
		}
		config := snapshot.service.Databases[name]
		config.DSN, err = expandEnvironment(config.DSN)
		if err != nil {
			return nil, fmt.Errorf("database %q: %w", name, err)
		}
		db, connection, openErr := models.OpenDatabase(name, config)
		if openErr != nil {
			return nil, openErr
		}
		app.closers = append(app.closers, connection)
		databases[name] = db
	}
	trace := options.TraceWriter
	if trace == nil {
		trace = NewJSONTraceWriter(app.stdout)
	}
	projects, err := resolveConnections(snapshot, options.ProjectModels, options.ProjectDefaults)
	if err != nil {
		return nil, err
	}
	app.runtime, err = luban.New(luban.Options{
		Config: snapshot.agents, Projects: projects, HTTPDoer: options.HTTPDoer,
		ProjectModels:   options.ProjectModels,
		ProjectDefaults: options.ProjectDefaults,
		Databases:       databases, Resources: options.Resources, Skills: snapshot.skills, TraceWriter: trace,
	})
	if err != nil {
		return nil, err
	}
	if err = app.runtime.Register(nil, nil, nil); err != nil {
		return nil, err
	}
	if err = app.runtime.Validate(); err != nil {
		return nil, err
	}
	app.databases = databases
	return app, nil
}

func typedNil(value any) bool {
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

// Runtime returns the shared execution engine for HTTP, MQ and task bindings.
func (app *App) Runtime() *luban.Runtime { return app.runtime }

// Address returns the configured HTTP listener address (default :8080).
func (app *App) Address() string { return app.config.HTTP.Address }

// BackgroundAgents lists explicitly enabled domain workers. The hosting
// business package supplies their implementation; Runtime does not schedule.
func (app *App) BackgroundAgents() []string {
	return append([]string(nil), app.config.BackgroundAgents...)
}

// Render lets a business submission route use the service's response protocol.
func (app *App) Render(ctx *gin.Context, output any, err error) { app.render(ctx, output, err) }

// ReportError uses the selected host logger for background task failures.
func (app *App) ReportError(err error) { app.logger.Errorf(nil, "%v", err) }

// OnShutdown registers business cleanup once for both host signals and Close.
// Stop callbacks must finish before their database/client resources are closed.
func (app *App) OnShutdown(name string, stop func()) {
	stop = sync.OnceFunc(stop)
	app.shutdownMu.Lock()
	if app.closed {
		app.shutdownMu.Unlock()
		stop()
		return
	}
	app.stops = append(app.stops, stop)
	app.shutdownMu.Unlock()
	if app.registerShutdown != nil {
		app.registerShutdown(name, stop)
	}
}

// Database returns a host-level connection for authorized task executors and
// business routers. Agent functions use luban.DB in their active Run instead.
func (app *App) Database(name string) (*gorm.DB, bool) {
	db, ok := app.databases[name]
	return db, ok
}

// Close releases host-owned resources once. Stop inputs and wait for active
// executions before closing. Injected resources are never closed here.
func (app *App) Close() error {
	app.closeOnce.Do(func() {
		app.shutdownMu.Lock()
		app.closed = true
		stops := append([]func(){}, app.stops...)
		app.shutdownMu.Unlock()
		for i := len(stops) - 1; i >= 0; i-- {
			stops[i]()
		}
		for i := len(app.closers) - 1; i >= 0; i-- {
			if err := app.closers[i].Close(); err != nil && app.closeErr == nil {
				app.closeErr = fmt.Errorf("close database connection")
			}
		}
	})
	return app.closeErr
}
