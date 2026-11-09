// Package tracegorm stores completed Luban Trace events in a host-owned Gorm
// database. It is optional: importing sdk does not enable database persistence.
package tracegorm

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

var (
	ErrClosed        = errors.New("tracegorm: writer is closed")
	ErrQueueFull     = errors.New("tracegorm: pending budget exhausted")
	ErrEventTooLarge = errors.New("tracegorm: event exceeds size limit")
	ErrInvalidEvent  = errors.New("tracegorm: invalid event")
	ErrPersistence   = errors.New("tracegorm: persistence failed")
)

// Options controls bounded, best-effort persistence. Zero numeric values use
// defaults. Callbacks must be concurrency-safe, return promptly and never call
// Close or recursively write to this writer.
type Options struct {
	Service         string
	Metadata        func(*gin.Context) Metadata
	OnError         func(WriteError)
	QueueSize       int
	MaxPendingBytes int64
	MaxEventBytes   int64
	BatchSize       int
	BatchBytes      int64
	FlushInterval   time.Duration
	WriteTimeout    time.Duration
}

// Metadata associates a completed Run with a host business entity.
// It is read synchronously, only for the final run event with a non-nil Context.
type Metadata struct {
	BusinessType string
	BusinessID   string
	RequestID    string
}

// WriteError reports diagnostics without retaining payloads or Gin contexts.
// RunID and SpanID are empty for batch/worker failures.
type WriteError struct {
	Operation  string
	RunID      string
	SpanID     string
	EventCount int
	Err        error
}

// Stats is a point-in-time snapshot. Pending includes queued and in-flight
// events. Persisted counts handled events (including duplicates), not SQL rows.
type Stats struct {
	Received      uint64
	Accepted      uint64
	Persisted     uint64
	Rejected      uint64
	Failed        uint64
	Pending       uint64
	PendingBytes  int64
	WriteFailures uint64
}

func (o Options) normalized() (Options, error) {
	if strings.TrimSpace(o.Service) == "" || !validText(o.Service, 128) {
		return o, fmt.Errorf("tracegorm: Service requires 1..128 Unicode characters")
	}
	if o.QueueSize < 0 || o.BatchSize < 0 || o.MaxPendingBytes < 0 ||
		o.MaxEventBytes < 0 || o.BatchBytes < 0 || o.FlushInterval < 0 || o.WriteTimeout < 0 {
		return o, fmt.Errorf("tracegorm: numeric options must not be negative")
	}
	if o.QueueSize == 0 {
		o.QueueSize = 1024
	}
	if o.MaxPendingBytes == 0 {
		o.MaxPendingBytes = 32 << 20
	}
	if o.MaxEventBytes == 0 {
		o.MaxEventBytes = 8 << 20
	}
	if o.BatchSize == 0 {
		o.BatchSize = 50
	}
	if o.BatchBytes == 0 {
		o.BatchBytes = 1 << 20
	}
	if o.FlushInterval == 0 {
		o.FlushInterval = 250 * time.Millisecond
	}
	if o.WriteTimeout == 0 {
		o.WriteTimeout = 2 * time.Second
	}
	if o.BatchSize > o.QueueSize || o.BatchBytes > o.MaxPendingBytes || o.MaxEventBytes > o.MaxPendingBytes {
		return o, fmt.Errorf("tracegorm: batch/event limits must fit the pending budget")
	}
	if o.OnError == nil {
		o.OnError = logWriteError
	}
	return o, nil
}

func validText(value string, max int) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= max
}

func logWriteError(e WriteError) {
	slog.Error("luban_trace_storage", "operation", e.Operation, "runId", e.RunID,
		"spanId", e.SpanID, "eventCount", e.EventCount, "error", e.Err)
}
