package luban

import (
	"context"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRegisterAcceptsEightBusinessFunctionShapes(t *testing.T) {
	functions := map[string]any{
		"no_args_no_output": func(*gin.Context) {},
		"no_args_error":     func(*gin.Context) error { return nil },
		"no_args_output":    func(*gin.Context) int { return 1 },
		"no_args_both":      func(*gin.Context) (int, error) { return 1, nil },
		"input_no_output":   func(*gin.Context, string) {},
		"input_error":       func(*gin.Context, string) error { return nil },
		"input_output":      func(*gin.Context, string) int { return 1 },
		"input_both":        func(*gin.Context, string) (int, error) { return 1, nil },
	}
	rt, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Register(functions, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := len(rt.funcs); got != len(functions) {
		t.Fatalf("registered %d functions, want %d", got, len(functions))
	}
}

func TestRegisterRejectsInvalidBusinessFunctionSignatures(t *testing.T) {
	tests := []struct {
		name string
		fn   any
	}{
		{name: "missing Gin first argument", fn: func(context.Context) {}},
		{name: "two business arguments", fn: func(*gin.Context, string, int) {}},
		{name: "variadic", fn: func(*gin.Context, ...string) {}},
		{name: "multiple outputs", fn: func(*gin.Context) (int, string, error) { return 0, "", nil }},
		{name: "wrong second return", fn: func(*gin.Context) (int, string) { return 0, "" }},
		{name: "concrete error return", fn: func(*gin.Context) runtimeConcreteError { return runtimeConcreteError{} }},
		{name: "concrete error in second slot", fn: func(*gin.Context) (int, runtimeConcreteError) { return 0, runtimeConcreteError{} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rt, err := New(Options{})
			if err != nil {
				t.Fatal(err)
			}
			if err := rt.Register(map[string]any{"bad": test.fn}, nil, nil); err == nil {
				t.Fatal("expected invalid function signature to fail")
			}
			if rt.registered.Load() || len(rt.funcs) != 0 {
				t.Fatal("invalid function registry was published")
			}
		})
	}
}
