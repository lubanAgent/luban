package main

import (
	"context"
	"log"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/lubanAgent/luban/agents"
	"github.com/lubanAgent/luban/server"
)

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	// Parse commands before loading configuration or initializing resources.
	command, err := server.ParseCommand(args)
	if err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n" + server.Usage + "\n")
		return 2
	}
	if command.Help {
		_, _ = os.Stdout.WriteString(server.Usage)
		return 0
	}

	gin.SetMode(gin.ReleaseMode)
	app, err := server.New(server.Options{
		Assets: os.DirFS("."),
		// Keep machine-readable CLI output on stdout. Trace stays searchable on
		// stderr and can be redirected to a file by the deployment environment.
		TraceWriter: server.NewJSONTraceWriter(os.Stderr),
	})
	if err != nil {
		log.Printf("start Luban: %v", err)
		return 1
	}
	defer app.Close()
	if !command.IsHTTP() {
		return app.RunCommand(command)
	}

	engine := gin.New()
	if err := app.SetupHTTP(engine); err != nil {
		log.Printf("setup HTTP: %v", err)
		return 1
	}
	if err := app.RegisterHTTP(engine); err != nil {
		log.Printf("register HTTP: %v", err)
		return 1
	}
	stopping, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := agents.Mount(stopping, engine, app); err != nil {
		log.Printf("mount business: %v", err)
		return 1
	}
	if err := app.Serve(engine); err != nil {
		log.Printf("HTTP service: %v", err)
		return 1
	}
	return 0
}
