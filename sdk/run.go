package luban

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync/atomic"

	"github.com/gin-gonic/gin"
	internaleino "github.com/lubanAgent/luban/sdk/internal/eino"
	"github.com/lubanAgent/luban/sdk/internal/executor"
)

var errStopCurrentFlow = errors.New("luban: stop current flow")

type nodeValue = internaleino.NodeValue

type runState struct {
	stopped  atomic.Bool
	stopNode nodeValue
}

// Stop requests that the current flow ends after the calling business function
// returns. The function must return normally; an error or cancellation wins.
func Stop(ctx *gin.Context) {
	if ctx == nil {
		return
	}
	value, exists := ctx.Get(runStateGinKey)
	if !exists {
		return
	}
	state, ok := value.(*runState)
	if ok && state != nil {
		state.stopped.Store(true)
	}
}

const runStateGinKey = "__luban_runtime_state_v1"

// Run executes the Agent identified by appKey. The initial input is passed to
// the first flow node; later nodes receive the preceding node's business
// output.
func (rt *Runtime) Run(ctx *gin.Context, appKey string, input any) (result any, resultErr error) {
	return rt.runFrom(ctx, appKey, "", input)
}

// RunFrom executes the Agent identified by appKey beginning at the registered
// business-function node named startFunc. The selected node and every
// following node execute in their configured order. This is useful when one
// Agent flow has multiple external entry points, such as a user conversation
// that stops while awaiting confirmation and a later confirmed-generation
// request that starts at a downstream business node.
//
// startFunc must identify exactly one function node in the Agent flow. The
// caller supplies that node's input; RunFrom does not restore or retain any
// state from an earlier Run.
func (rt *Runtime) RunFrom(ctx *gin.Context, appKey, startFunc string, input any) (result any, resultErr error) {
	if startFunc == "" {
		return nil, fmt.Errorf("luban: start function must not be empty")
	}
	return rt.runFrom(ctx, appKey, startFunc, input)
}

func (rt *Runtime) runFrom(ctx *gin.Context, appKey, startFunc string, input any) (result any, resultErr error) {
	activeNode := "Run setup"
	defer func() {
		if recovered := recover(); recovered != nil {
			var panicErr error
			if recoveredErr, ok := recovered.(error); ok {
				panicErr = fmt.Errorf("luban: appKey %q %s panicked: %w", appKey, activeNode, recoveredErr)
			} else {
				panicErr = fmt.Errorf("luban: appKey %q %s panicked: %v", appKey, activeNode, recovered)
			}
			if ctx != nil && ctx.Request != nil && ctx.Request.Context() != nil {
				if cancelErr := ctx.Request.Context().Err(); cancelErr != nil {
					panicErr = errors.Join(panicErr, cancelErr)
				}
			}
			result = nil
			resultErr = panicErr
		}
	}()
	if ctx == nil {
		return nil, fmt.Errorf("luban: Gin context must not be nil")
	}
	if appKey == "" {
		return nil, fmt.Errorf("luban: appKey must not be empty")
	}
	stdCtx, err := contextForGin(ctx)
	if err != nil {
		return nil, err
	}
	if err := stdCtx.Err(); err != nil {
		return nil, err
	}
	snapshot, err := rt.loadSnapshot(appKey)
	if err != nil {
		return nil, err
	}
	if err := stdCtx.Err(); err != nil {
		return nil, err
	}
	if err := rt.validateFlowSignatures(snapshot.config.Flow); err != nil {
		return nil, fmt.Errorf("luban: appKey %q flow: %w", appKey, err)
	}
	startIndex := 0
	if startFunc != "" {
		matches := 0
		for index, node := range snapshot.config.Flow {
			if node.Func != startFunc {
				continue
			}
			startIndex = index
			matches++
		}
		switch {
		case matches == 0:
			return nil, fmt.Errorf("luban: appKey %q start function %q was not found in flow", appKey, startFunc)
		case matches > 1:
			return nil, fmt.Errorf("luban: appKey %q start function %q appears %d times in flow", appKey, startFunc, matches)
		}
	}

	state := &runState{}
	ctx.Set(runStateGinKey, state)
	defer clearGinKey(ctx, runStateGinKey)

	steps := make([]internaleino.Step, 0, len(snapshot.config.Flow)-startIndex)
	for index := startIndex; index < len(snapshot.config.Flow); index++ {
		node := snapshot.config.Flow[index]
		index, node := index, node
		steps = append(steps, func(current context.Context, incoming nodeValue) (nodeValue, error) {
			activeNode = fmt.Sprintf("flow[%d]", index)
			if err := current.Err(); err != nil {
				return nodeValue{}, err
			}
			ginCtx, err := ginContextFrom(current)
			if err != nil {
				return nodeValue{}, err
			}

			var output nodeValue
			var callbackErr error
			nodeName := node.Func
			if node.Func == "" {
				nodeName = node.ModelName
				output, callbackErr = rt.runAgentNode(current, ginCtx, appKey, index, node, incoming)
			} else {
				output, callbackErr = rt.callFunction(appKey, index, node.Func, ginCtx, incoming)
			}
			activeNode = fmt.Sprintf("flow[%d] node %q", index, nodeName)
			if cancelErr := current.Err(); cancelErr != nil {
				if callbackErr != nil {
					return nodeValue{}, errors.Join(
						fmt.Errorf("luban: appKey %q flow[%d] node %q: %w", appKey, index, nodeName, callbackErr),
						cancelErr,
					)
				}
				return nodeValue{}, cancelErr
			}
			if callbackErr != nil {
				return nodeValue{}, fmt.Errorf("luban: appKey %q flow[%d] node %q: %w", appKey, index, nodeName, callbackErr)
			}
			if state.stopped.Load() {
				state.stopNode = output
				return nodeValue{}, errStopCurrentFlow
			}
			return output, nil
		})
	}

	output, runErr := internaleino.RunChain(stdCtx, nodeValue{Value: input, HasValue: true}, steps)
	if cancelErr := stdCtx.Err(); cancelErr != nil {
		if errors.Is(runErr, errStopCurrentFlow) {
			return nil, fmt.Errorf("luban: appKey %q: %w", appKey, cancelErr)
		}
		if runErr != nil && !errors.Is(runErr, cancelErr) {
			return nil, fmt.Errorf("luban: appKey %q: %w", appKey, errors.Join(runErr, cancelErr))
		}
		if runErr != nil {
			return nil, fmt.Errorf("luban: appKey %q: %w", appKey, runErr)
		}
		return nil, fmt.Errorf("luban: appKey %q: %w", appKey, cancelErr)
	}
	if errors.Is(runErr, errStopCurrentFlow) {
		output, runErr = state.stopNode, nil
	}
	if runErr != nil {
		return nil, fmt.Errorf("luban: appKey %q: %w", appKey, runErr)
	}
	if !output.HasValue {
		return map[string]any{}, nil
	}
	return output.Value, nil
}

func (rt *Runtime) validateFlowSignatures(flow []NodeConfig) error {
	for index := 0; index+1 < len(flow); index++ {
		current, next := flow[index], flow[index+1]
		if current.Func == "" {
			continue
		}
		out := rt.funcs[current.Func]
		if next.Func == "" {
			if !out.HasOutput {
				return fmt.Errorf("flow[%d] function %q has no output for Agent input at flow[%d]",
					index, current.Func, index+1)
			}
			if out.OutputType.Kind() == reflect.Interface {
				continue
			}
			if out.OutputType != reflect.TypeFor[Request]() && out.OutputType != reflect.TypeFor[Batch]() {
				return fmt.Errorf("flow[%d] function %q output %v is not Request or Batch required by Agent at flow[%d]",
					index, current.Func, out.OutputType, index+1)
			}
			continue
		}
		in := rt.funcs[next.Func]
		if !in.HasInput {
			continue
		}
		if !out.HasOutput {
			return fmt.Errorf("flow[%d] function %q has no output for input of flow[%d] function %q",
				index, current.Func, index+1, next.Func)
		}
		if out.OutputType.Kind() == reflect.Interface {
			continue
		}
		if !out.OutputType.AssignableTo(in.InputType) {
			return fmt.Errorf("flow[%d] function %q output %v is not assignable to flow[%d] function %q input %v",
				index, current.Func, out.OutputType, index+1, next.Func, in.InputType)
		}
	}
	return nil
}

func contextForGin(ctx *gin.Context) (context.Context, error) {
	if ctx == nil {
		return nil, fmt.Errorf("luban: Gin context must not be nil")
	}
	parent := context.Background()
	if ctx.Request != nil && ctx.Request.Context() != nil {
		parent = ctx.Request.Context()
	}
	return internaleino.WithGinContext(parent, ctx)
}

func ginContextFrom(ctx context.Context) (*gin.Context, error) {
	return internaleino.GinContextFrom(ctx)
}

func clearGinKey(ctx *gin.Context, key string) {
	if ctx == nil {
		return
	}
	ctx.Set(key, nil)
}

func (rt *Runtime) callFunction(appKey string, index int, key string, ctx *gin.Context, incoming nodeValue) (out nodeValue, err error) {
	info, exists := rt.funcs[key]
	if !exists {
		return nodeValue{}, fmt.Errorf("luban: appKey %q flow[%d] function %q was not registered", appKey, index, key)
	}
	result, err := executor.Invoke(key, info, ctx, incoming.Value, incoming.HasValue)
	if err != nil {
		return nodeValue{}, fmt.Errorf("luban: appKey %q flow[%d] function %q: %w", appKey, index, key, err)
	}
	return nodeValue{Value: result.Value, HasValue: result.HasValue}, nil
}

func adaptInput(incoming nodeValue, target reflect.Type) (reflect.Value, error) {
	return executor.AdaptInput(incoming.Value, incoming.HasValue, target)
}
