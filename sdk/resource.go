package luban

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
)

// Resource retrieves a service-owned dependency registered in Options.Resources.
// T is the original client type or a business-defined interface it implements.
// Luban never initializes or closes these objects. Their methods may be called
// concurrently by Runs, Tools and Batch items; the owner ensures that is safe.
func Resource[T any](ctx *gin.Context, name string) (T, error) {
	var zero T
	if strings.TrimSpace(name) == "" {
		return zero, fmt.Errorf("luban: resource name must not be empty")
	}
	data := dataFromGin(ctx)
	if data == nil {
		return zero, fmt.Errorf("luban: Resource requires an active Run")
	}
	data.mu.RLock()
	defer data.mu.RUnlock()
	if data.closed {
		return zero, fmt.Errorf("luban: Resource called after Run completed")
	}
	value, ok := data.resources[name]
	if !ok {
		return zero, fmt.Errorf("luban: resource %q was not configured", name)
	}
	result, ok := value.(T)
	if !ok {
		return zero, fmt.Errorf("luban: resource %q has type %T, requested %v", name, value, reflect.TypeFor[T]())
	}
	return result, nil
}

func copyResources(source map[string]any) (map[string]any, error) {
	resources := make(map[string]any, len(source))
	for name, value := range source {
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("luban: resource name must not be empty")
		}
		if isNilInterface(value) {
			return nil, fmt.Errorf("luban: resource %q is nil", name)
		}
		resources[name] = value
	}
	return resources, nil
}
