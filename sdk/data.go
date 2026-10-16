package luban

import (
	"fmt"
	"sync"

	"github.com/gin-gonic/gin"
)

const sharedDataGinKey = "__luban_shared_data_v1"

// runData is shared by Gin copies, but belongs to exactly one Run. Closing it
// also invalidates copies retained by callbacks after that Run has finished.
type runData struct {
	mu       sync.RWMutex
	values   map[string]any
	readonly bool
	closed   bool
}

// Set stores a value for this Run. Business functions and ordinary Agent Tools
// share these values even when the SDK copies their Gin contexts.
//
// It returns an error outside an active Run or inside a readonly Batch scope.
// The lock protects map access, not mutations of stored pointers, maps or
// slices, nor a compound Get/modify/Set operation.
func Set(ctx *gin.Context, key string, value any) error {
	data := dataFromGin(ctx)
	if data == nil {
		return fmt.Errorf("luban: Set requires an active Run")
	}
	data.mu.Lock()
	defer data.mu.Unlock()
	if data.closed {
		return fmt.Errorf("luban: Set called after Run completed")
	}
	if data.readonly {
		return fmt.Errorf("luban: Set is not allowed in a readonly Batch scope; return item results instead")
	}
	data.values[key] = value
	return nil
}

// Get retrieves a value from this Run. A stored nil returns (nil, true); a
// missing key, absent Run or completed Run returns (nil, false).
// Batch callbacks read a shallow snapshot taken when the batch starts.
func Get(ctx *gin.Context, key string) (any, bool) {
	data := dataFromGin(ctx)
	if data == nil {
		return nil, false
	}
	data.mu.RLock()
	defer data.mu.RUnlock()
	if data.closed {
		return nil, false
	}
	value, exists := data.values[key]
	return value, exists
}

func dataFromGin(ctx *gin.Context) *runData {
	if ctx == nil {
		return nil
	}
	value, _ := ctx.Get(sharedDataGinKey)
	data, _ := value.(*runData)
	return data
}

func (data *runData) close() {
	data.mu.Lock()
	defer data.mu.Unlock()
	data.closed = true
	data.values = nil
}

func (data *runData) readonlySnapshot() *runData {
	snapshot := &runData{values: make(map[string]any), readonly: true}
	if data == nil {
		return snapshot
	}
	data.mu.RLock()
	defer data.mu.RUnlock()
	for key, value := range data.values {
		snapshot.values[key] = value
	}
	return snapshot
}
