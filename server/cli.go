package server

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
)

const Usage = "Usage:\n  luban                             Start HTTP service\n  luban run <agentKey> --input '<json>'\n  luban --help"

// Command is parsed before resources are initialized. An empty command starts
// HTTP; help and syntax errors need neither configuration nor network clients.
type Command struct {
	Help     bool
	agentKey string
	input    string
}

func (command Command) IsHTTP() bool { return !command.Help && command.agentKey == "" }

func ParseCommand(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, nil
	}
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h" || args[0] == "help") {
		return Command{Help: true}, nil
	}
	if len(args) == 2 && args[0] == "run" && (args[1] == "--help" || args[1] == "-h") {
		return Command{Help: true}, nil
	}
	if args[0] != "run" || len(args) < 2 || args[1] == "" || args[1][0] == '-' {
		return Command{}, fmt.Errorf("expected run <agentKey> or no arguments")
	}
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	inputJSON := flags.String("input", "{}", "Agent JSON input")
	if err := flags.Parse(args[2:]); err != nil {
		if err == flag.ErrHelp {
			return Command{Help: true}, nil
		}
		return Command{}, err
	}
	if flags.NArg() != 0 {
		return Command{}, fmt.Errorf("unexpected positional arguments")
	}
	if !json.Valid([]byte(*inputJSON)) {
		return Command{}, fmt.Errorf("--input must be valid JSON")
	}
	return Command{agentKey: args[1], input: *inputJSON}, nil
}

// RunCLI is a convenience wrapper for callers that already own an App.
// Executables should ParseCommand first and only create an App for real work.
func (app *App) RunCLI(args []string) (handled bool, exitCode int) {
	command, err := ParseCommand(args)
	if err != nil {
		fmt.Fprintln(app.stderr, err)
		fmt.Fprintln(app.stderr, Usage)
		return true, 2
	}
	if command.IsHTTP() {
		return false, 0
	}
	return true, app.RunCommand(command)
}

// RunCommand executes one parsed CLI invocation, without HTTP middleware or
// listeners. Agent-dependent input validation happens against the loaded App.
func (app *App) RunCommand(command Command) int {
	if command.Help {
		fmt.Fprintln(app.stdout, Usage)
		return 0
	}
	if command.IsHTTP() {
		fmt.Fprintln(app.stderr, "HTTP service command cannot be executed as an Agent")
		return 2
	}
	input, err := app.runtime.DecodeInput(command.agentKey, []byte(command.input))
	if err != nil {
		fmt.Fprintln(app.stderr, err)
		return 1
	}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/", bytes.NewBufferString(command.input))
	ctx.Request.Header.Set("Content-Type", "application/json")
	output, err := app.runtime.Run(ctx, command.agentKey, input)
	if err != nil {
		fmt.Fprintln(app.stderr, err)
		return 1
	}
	if json.NewEncoder(app.stdout).Encode(output) != nil {
		return 1
	}
	return 0
}
