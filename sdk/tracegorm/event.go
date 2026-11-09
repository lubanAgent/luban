package tracegorm

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	luban "github.com/lubanAgent/luban/sdk"
)

// Row types stay private. Table and column names are explicit so a host's Gorm
// NamingStrategy cannot redirect writes to business tables.
type spanRow struct {
	ID           uint64  `gorm:"column:id;primaryKey;autoIncrement"`
	RunID        string  `gorm:"column:run_id"`
	SpanID       string  `gorm:"column:span_id"`
	ParentSpanID string  `gorm:"column:parent_span_id"`
	Kind         string  `gorm:"column:kind"`
	Status       string  `gorm:"column:status"`
	StartedAt    utcTime `gorm:"column:started_at"`
	DurationNs   int64   `gorm:"column:duration_ns"`
	EventJSON    string  `gorm:"column:event_json"`
	CreateTime   int64   `gorm:"column:create_time"`
	UpdateTime   int64   `gorm:"column:update_time"`
	Deleted      uint8   `gorm:"column:deleted"`
}

type runRow struct {
	ID           uint64  `gorm:"column:id;primaryKey;autoIncrement"`
	RunID        string  `gorm:"column:run_id"`
	Service      string  `gorm:"column:service"`
	AgentKey     string  `gorm:"column:agent_key"`
	BusinessType string  `gorm:"column:business_type"`
	BusinessID   string  `gorm:"column:business_id"`
	RequestID    string  `gorm:"column:request_id"`
	Status       string  `gorm:"column:status"`
	StartedAt    utcTime `gorm:"column:started_at"`
	DurationNs   int64   `gorm:"column:duration_ns"`
	ErrorMsg     string  `gorm:"column:error_msg"`
	CreateTime   int64   `gorm:"column:create_time"`
	UpdateTime   int64   `gorm:"column:update_time"`
	Deleted      uint8   `gorm:"column:deleted"`
}

func (runRow) TableName() string { return runTable }

// A MySQL driver converts time.Time parameters to the host DSN's loc before
// writing DATETIME. Bind a UTC wall-clock string instead, keeping UTC indexes
// correct even when the host connection uses loc=Local.
type utcTime struct{ time.Time }

const dateTimeLayout = "2006-01-02 15:04:05.000000"

func (t utcTime) Value() (driver.Value, error) {
	return t.UTC().Format(dateTimeLayout), nil
}

func (t *utcTime) Scan(value any) error {
	switch v := value.(type) {
	case time.Time:
		// DATETIME has no timezone; the driver's loc only labels its wall clock.
		t.Time = time.Date(v.Year(), v.Month(), v.Day(), v.Hour(), v.Minute(), v.Second(), v.Nanosecond(), time.UTC)
		return nil
	case []byte:
		return t.Scan(string(v))
	case string:
		parsed, err := time.ParseInLocation(dateTimeLayout, v, time.UTC)
		if err != nil {
			return fmt.Errorf("tracegorm: invalid database datetime: %w", err)
		}
		t.Time = parsed
		return nil
	default:
		return fmt.Errorf("tracegorm: unsupported datetime value %T", value)
	}
}

type entry struct {
	span     spanRow
	run      *runRow
	bytes    int64
	queuedAt time.Time
}

func encode(event luban.TraceEvent, service string, metadata Metadata) (entry, error) {
	if len(event.Input) == 0 {
		event.Input = json.RawMessage("null")
	}
	if len(event.Output) == 0 {
		event.Output = json.RawMessage("null")
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return entry{}, fmt.Errorf("%w: cannot encode event: %v", ErrInvalidEvent, err)
	}
	meta, err := json.Marshal(struct {
		Service string
		Metadata
	}{service, metadata})
	if err != nil {
		return entry{}, fmt.Errorf("%w: cannot encode metadata: %v", ErrInvalidEvent, err)
	}
	now := time.Now()
	row := spanRow{
		RunID: event.RunID, SpanID: event.SpanID, ParentSpanID: event.ParentSpanID,
		Kind: event.Type, Status: event.Status,
		StartedAt:  utcTime{event.StartedAt.UTC().Truncate(time.Microsecond)},
		DurationNs: event.DurationNs, EventJSON: string(raw),
		CreateTime: now.Unix(),
		UpdateTime: now.Unix(),
	}
	item := entry{span: row, bytes: int64(len(raw)) + int64(len(meta)), queuedAt: now}
	if event.Type == "run" {
		item.run = &runRow{
			RunID: event.RunID, Service: service, AgentKey: event.Name,
			BusinessType: metadata.BusinessType, BusinessID: metadata.BusinessID,
			RequestID: metadata.RequestID, Status: row.Status, StartedAt: row.StartedAt,
			DurationNs: row.DurationNs, ErrorMsg: event.Error,
			CreateTime: row.CreateTime, UpdateTime: row.UpdateTime,
		}
	}
	// Only immutable serialized data and scalar fields survive this call.
	// In particular, no Gin Context, RawMessage or FlowIndex pointer is retained.
	return item, nil
}

func validateEvent(e luban.TraceEvent) error {
	switch {
	case e.Version != 1:
		return fmt.Errorf("%w: version must be 1", ErrInvalidEvent)
	case !asciiID(e.RunID, 64, false) || !asciiID(e.SpanID, 64, false) ||
		!asciiID(e.ParentSpanID, 64, true) || !asciiID(e.TransferFromSpanID, 64, true):
		return fmt.Errorf("%w: invalid run/span identity", ErrInvalidEvent)
	case !asciiID(e.Type, 64, false):
		return fmt.Errorf("%w: invalid event type", ErrInvalidEvent)
	case e.Status != "success" && e.Status != "error" && e.Status != "stopped" && e.Status != "skipped":
		return fmt.Errorf("%w: invalid status", ErrInvalidEvent)
	case (e.Status == "skipped" && e.SkipReason != "empty_input") ||
		(e.Status != "skipped" && e.SkipReason != ""):
		return fmt.Errorf("%w: skipped status requires empty_input reason", ErrInvalidEvent)
	case e.Status == "skipped" && (e.Type != "agent" || e.FlowIndex == nil):
		return fmt.Errorf("%w: only flow Agent nodes may be skipped", ErrInvalidEvent)
	case e.StartedAt.UTC().Year() < 1000 || e.StartedAt.UTC().Year() > 9999:
		return fmt.Errorf("%w: time outside DATETIME range", ErrInvalidEvent)
	case e.DurationNs < 0 || (e.FlowIndex != nil && *e.FlowIndex < 0):
		return fmt.Errorf("%w: negative duration or flow index", ErrInvalidEvent)
	case !utf8.ValidString(e.Name) || !utf8.ValidString(e.Description) ||
		!utf8.ValidString(e.AgentName) || !utf8.ValidString(e.Error):
		return fmt.Errorf("%w: invalid UTF-8 text", ErrInvalidEvent)
	case e.Type == "run" && (e.ParentSpanID != "" || !validText(e.Name, 255) || (e.Name == "" && e.Status != "error")):
		return fmt.Errorf("%w: invalid run parent or Agent key", ErrInvalidEvent)
	}
	for _, raw := range []json.RawMessage{e.Input, e.Output} {
		if len(raw) != 0 && (!utf8.Valid(raw) || !json.Valid(raw)) {
			return fmt.Errorf("%w: input/output must be valid UTF-8 JSON", ErrInvalidEvent)
		}
	}
	return nil
}

// Restrict IDs to printable non-space ASCII. This also avoids trailing-space
// comparisons collapsing different identifiers in MySQL VARCHAR indexes.
func asciiID(value string, max int, optional bool) bool {
	if len(value) > max || (!optional && len(value) == 0) {
		return false
	}
	for i := range len(value) {
		if value[i] < 33 || value[i] > 126 {
			return false
		}
	}
	return true
}

func readMetadata(callback func(*gin.Context) Metadata, ctx *gin.Context) (value Metadata, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			value = Metadata{}
			err = fmt.Errorf("tracegorm: Metadata callback panicked (%T)", recovered)
		}
	}()
	value = callback(ctx)
	if !validText(value.BusinessType, 64) || !validText(value.BusinessID, 255) || !validText(value.RequestID, 255) {
		return Metadata{}, fmt.Errorf("tracegorm: metadata contains invalid or oversized text")
	}
	return value, nil
}
