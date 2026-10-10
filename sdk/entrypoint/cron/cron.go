// Package lubancron adapts multi-item work to an existing Cron scheduler.
package lubancron

import (
	"errors"
	"fmt"

	"github.com/gin-gonic/gin"
	luban "github.com/lubanAgent/luban/sdk"
)

// Loader loads the business items handled by one scheduled execution.
type Loader[T any] func(ctx *gin.Context, args ...string) ([]T, error)

// Job returns a synchronous Cron callback. Each loaded item runs with a copied
// Gin Context, failures are collected by item index, and processing continues.
func Job[T any](rt *luban.Runtime, appKey string, loader Loader[T]) func(ctx *gin.Context, args ...string) error {
	if rt == nil {
		panic("lubancron: Runtime must not be nil")
	}
	if appKey == "" {
		panic("lubancron: appKey must not be empty")
	}
	if loader == nil {
		panic("lubancron: loader must not be nil")
	}
	return func(ctx *gin.Context, args ...string) error {
		if ctx == nil {
			return fmt.Errorf("lubancron: appKey %q Gin context must not be nil", appKey)
		}
		items, err := loadItems(loader, ctx, args...)
		if err != nil {
			return fmt.Errorf("lubancron: appKey %q load items: %w", appKey, err)
		}
		if len(items) == 0 {
			return nil
		}

		var runErrors []error
		for index, item := range items {
			itemCtx := ctx.Copy()
			if _, err := rt.Run(itemCtx, appKey, item); err != nil {
				runErrors = append(runErrors, fmt.Errorf("lubancron: appKey %q item[%d]: %w", appKey, index, err))
			}
		}
		return errors.Join(runErrors...)
	}
}

func loadItems[T any](loader Loader[T], ctx *gin.Context, args ...string) (items []T, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			items = nil
			err = fmt.Errorf("loader panicked: %v", recovered)
		}
	}()
	return loader(ctx, args...)
}
