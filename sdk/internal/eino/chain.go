// Package eino adapts Luban execution primitives to Eino.
package eino

import (
	"context"

	"github.com/cloudwego/eino/compose"
)

// NodeValue preserves the distinction between no output and an actual nil
// output while values pass between sequential nodes.
type NodeValue struct {
	Value    any
	HasValue bool
}

// Step is one synchronous business or Agent node in a sequential flow.
type Step func(context.Context, NodeValue) (NodeValue, error)

// RunChain builds and invokes a new Eino Chain for one Run.
func RunChain(ctx context.Context, initial NodeValue, steps []Step) (NodeValue, error) {
	chain := compose.NewChain[NodeValue, NodeValue]()
	for _, step := range steps {
		currentStep := step
		chain.AppendLambda(compose.InvokableLambda(func(ctx context.Context, input NodeValue) (NodeValue, error) {
			return currentStep(ctx, input)
		}))
	}
	runnable, err := chain.Compile(ctx)
	if err != nil {
		return NodeValue{}, err
	}
	return runnable.Invoke(ctx, initial)
}
