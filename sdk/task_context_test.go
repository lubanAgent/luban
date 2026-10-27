package luban

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRunJoinsTaskCancellationWithCallbackPanic(t *testing.T) {
	taskContext, cancel := context.WithCancel(context.Background())
	rt, err := New(Options{Config: Config{Agents: map[string]AgentConfig{
		"agent": {Flow: []NodeConfig{{Func: "Fail"}}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Register(map[string]any{
		"Fail": func(*gin.Context) error {
			cancel()
			panic("business panic")
		},
	}, nil, nil); err != nil {
		t.Fatal(err)
	}
	ctx := &gin.Context{}
	if err := BindContext(ctx, taskContext); err != nil {
		t.Fatal(err)
	}
	_, err = rt.Run(ctx, "agent", nil)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want panic and context cancellation", err)
	}
	if !strings.Contains(err.Error(), "business panic") {
		t.Fatalf("Run error lost callback panic: %v", err)
	}
}
