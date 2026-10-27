package server

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	luban "github.com/lubanAgent/luban/sdk"
)

// Logger receives ordinary service logs, separate from structured Trace events.
type Logger interface {
	Debugf(*gin.Context, string, ...any)
	Infof(*gin.Context, string, ...any)
	Warnf(*gin.Context, string, ...any)
	Errorf(*gin.Context, string, ...any)
}

type jsonLogger struct {
	mu     sync.Mutex
	output io.Writer
}

// NewJSONLogger creates a concurrency-safe JSON line logger.
func NewJSONLogger(output io.Writer) Logger { return &jsonLogger{output: output} }

func (l *jsonLogger) write(ctx *gin.Context, level, format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	_ = json.NewEncoder(l.output).Encode(map[string]any{"time": time.Now().Format(time.RFC3339Nano), "level": level, "msg": fmt.Sprintf(format, args...), "runId": luban.RunID(ctx)})
}
func (l *jsonLogger) Debugf(ctx *gin.Context, format string, args ...any) {
	l.write(ctx, "DEBUG", format, args...)
}
func (l *jsonLogger) Infof(ctx *gin.Context, format string, args ...any) {
	l.write(ctx, "INFO", format, args...)
}
func (l *jsonLogger) Warnf(ctx *gin.Context, format string, args ...any) {
	l.write(ctx, "WARN", format, args...)
}
func (l *jsonLogger) Errorf(ctx *gin.Context, format string, args ...any) {
	l.write(ctx, "ERROR", format, args...)
}

type jsonTraceWriter struct {
	mu     sync.Mutex
	output io.Writer
}

// NewJSONTraceWriter emits the existing offline viewer's traceEvent envelope.
func NewJSONTraceWriter(output io.Writer) luban.TraceWriter { return &jsonTraceWriter{output: output} }

func (w *jsonTraceWriter) WriteTrace(_ *gin.Context, event luban.TraceEvent) {
	w.mu.Lock()
	defer w.mu.Unlock()
	_ = json.NewEncoder(w.output).Encode(map[string]any{"level": "INFO", "msg": "luban_trace", "runId": event.RunID, "traceEvent": event})
}
