package luban

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
)

func newBusinessRuntime(t *testing.T, flow []NodeConfig, funcs map[string]any) *Runtime {
	t.Helper()
	rt, err := New(Options{Config: Config{Agents: map[string]AgentConfig{
		"test": {Flow: flow},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Register(funcs, nil, nil); err != nil {
		t.Fatal(err)
	}
	return rt
}

func TestRunPassesValuesBetweenBusinessNodes(t *testing.T) {
	rt := newBusinessRuntime(t, []NodeConfig{{Func: "Prepare"}, {Func: "Save"}}, map[string]any{
		"Prepare": func(ctx *gin.Context, input string) (int, error) {
			ctx.Set("business", "value")
			return len(input), nil
		},
		"Save": func(ctx *gin.Context, input int) (string, error) {
			value, exists := ctx.Get("business")
			if !exists || value != "value" {
				t.Fatalf("Gin Context did not persist between nodes: %v", value)
			}
			return "length=" + string(rune('0'+input)), nil
		},
	})

	result, err := rt.Run(&gin.Context{}, "test", "hello")
	if err != nil {
		t.Fatal(err)
	}
	if result != "length=5" {
		t.Fatalf("Run result = %#v", result)
	}
}

func TestRunAllowsDownstreamFunctionToIgnorePreviousValue(t *testing.T) {
	called := false
	rt := newBusinessRuntime(t, []NodeConfig{{Func: "Prepare"}, {Func: "Finish"}}, map[string]any{
		"Prepare": func(*gin.Context) int { return 9 },
		"Finish": func(*gin.Context) string {
			called = true
			return "done"
		},
	})
	result, err := rt.Run(&gin.Context{}, "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !called || result != "done" {
		t.Fatalf("called=%t result=%#v", called, result)
	}
}

func TestRunChecksStaticFunctionLinksBeforeCallingBusinessCode(t *testing.T) {
	called := false
	rt := newBusinessRuntime(t, []NodeConfig{{Func: "First"}, {Func: "Second"}}, map[string]any{
		"First": func(*gin.Context) int {
			called = true
			return 1
		},
		"Second": func(*gin.Context, string) {},
	})
	if _, err := rt.Run(&gin.Context{}, "test", nil); err == nil {
		t.Fatal("expected incompatible adjacent function types to fail")
	}
	if called {
		t.Fatal("first business function ran before a static link error was reported")
	}
}

func TestRunDoesNotPassMissingOutputAsNil(t *testing.T) {
	called := false
	rt := newBusinessRuntime(t, []NodeConfig{{Func: "Prepare"}, {Func: "Save"}}, map[string]any{
		"Prepare": func(*gin.Context) {},
		"Save": func(*gin.Context, any) {
			called = true
		},
	})
	if _, err := rt.Run(&gin.Context{}, "test", nil); err == nil {
		t.Fatal("expected missing business output to fail")
	}
	if called {
		t.Fatal("downstream function received a fabricated nil")
	}
}

func TestRunPreservesTrueNilAndTypedNilOutputs(t *testing.T) {
	t.Run("true nil", func(t *testing.T) {
		received := false
		rt := newBusinessRuntime(t, []NodeConfig{{Func: "Prepare"}, {Func: "Use"}}, map[string]any{
			"Prepare": func(*gin.Context) any { return nil },
			"Use": func(_ *gin.Context, value any) string {
				received = value == nil
				return "nil"
			},
		})
		result, err := rt.Run(&gin.Context{}, "test", nil)
		if err != nil {
			t.Fatal(err)
		}
		if !received || result != "nil" {
			t.Fatalf("received=%t result=%#v", received, result)
		}
	})

	t.Run("typed nil", func(t *testing.T) {
		received := false
		rt := newBusinessRuntime(t, []NodeConfig{{Func: "Prepare"}, {Func: "Use"}}, map[string]any{
			"Prepare": func(*gin.Context) *string { return nil },
			"Use": func(_ *gin.Context, value any) string {
				ptr, ok := value.(*string)
				received = ok && ptr == nil
				return "typed nil"
			},
		})
		result, err := rt.Run(&gin.Context{}, "test", nil)
		if err != nil {
			t.Fatal(err)
		}
		if !received || result != "typed nil" {
			t.Fatalf("received=%t result=%#v", received, result)
		}
	})
}

func TestRunReturnsEmptyMapWhenFinalFunctionHasNoBusinessOutput(t *testing.T) {
	rt := newBusinessRuntime(t, []NodeConfig{{Func: "Finish"}}, map[string]any{
		"Finish": func(*gin.Context) error { return nil },
	})
	result, err := rt.Run(&gin.Context{}, "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result, map[string]any{}) {
		t.Fatalf("result = %#v, want empty map", result)
	}
}

func TestRunPropagatesErrorsAndPanicWithoutPartialOutput(t *testing.T) {
	original := errors.New("business failure")
	for _, test := range []struct {
		name string
		fn   any
		want error
	}{
		{
			name: "error",
			fn: func(*gin.Context) (string, error) {
				return "partial", original
			},
			want: original,
		},
		{
			name: "panic",
			fn: func(*gin.Context) string {
				panic("boom")
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			rt := newBusinessRuntime(t, []NodeConfig{{Func: "Finish"}}, map[string]any{"Finish": test.fn})
			result, err := rt.Run(&gin.Context{}, "test", nil)
			if err == nil || result != nil {
				t.Fatalf("result=%#v err=%v", result, err)
			}
			if test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("error chain %v does not contain %v", err, test.want)
			}
		})
	}
}

func TestStopReturnsCurrentNodeOutputAndSkipsLaterNodes(t *testing.T) {
	called := false
	rt := newBusinessRuntime(t, []NodeConfig{{Func: "StopHere"}, {Func: "Never"}}, map[string]any{
		"StopHere": func(ctx *gin.Context) string {
			Stop(ctx)
			return "stopped"
		},
		"Never": func(*gin.Context) string {
			called = true
			return "wrong"
		},
	})
	result, err := rt.Run(&gin.Context{}, "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if called || result != "stopped" {
		t.Fatalf("laterCalled=%t result=%#v", called, result)
	}
}

func TestStopDoesNotCoverBusinessErrorWhenContextIsCancelled(t *testing.T) {
	requestContext, cancel := context.WithCancel(context.Background())
	request := &http.Request{Header: make(http.Header)}
	*request = *request.WithContext(requestContext)
	ctx := &gin.Context{Request: request}
	businessErr := errors.New("domain error")
	rt := newBusinessRuntime(t, []NodeConfig{{Func: "StopAndFail"}}, map[string]any{
		"StopAndFail": func(ctx *gin.Context) (string, error) {
			Stop(ctx)
			cancel()
			return "partial", businessErr
		},
	})
	result, err := rt.Run(ctx, "test", nil)
	if result != nil || !errors.Is(err, businessErr) || !errors.Is(err, context.Canceled) {
		t.Fatalf("result=%#v err=%v, want both business and cancellation errors", result, err)
	}
}

func TestStopStateDoesNotLeakAcrossRuns(t *testing.T) {
	calls := 0
	rt := newBusinessRuntime(t, []NodeConfig{{Func: "MaybeStop"}}, map[string]any{
		"MaybeStop": func(ctx *gin.Context) string {
			calls++
			if calls == 1 {
				Stop(ctx)
			}
			return "run"
		},
	})
	ctx := &gin.Context{}
	for i := 0; i < 2; i++ {
		if _, err := rt.Run(ctx, "test", nil); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatalf("function called %d times", calls)
	}
}

func TestRunRejectsInvalidEntrypoints(t *testing.T) {
	rt := newBusinessRuntime(t, []NodeConfig{{Func: "Finish"}}, map[string]any{
		"Finish": func(*gin.Context) {},
	})
	if _, err := rt.Run(nil, "test", nil); err == nil {
		t.Fatal("expected nil Gin context to fail")
	}
	if _, err := rt.Run(&gin.Context{}, "", nil); err == nil {
		t.Fatal("expected empty appKey to fail")
	}
	var unregistered *Runtime
	if _, err := unregistered.Run(&gin.Context{}, "test", nil); err == nil {
		t.Fatal("expected nil Runtime to fail")
	}
}
