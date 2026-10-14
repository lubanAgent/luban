package trace

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Event is an immutable snapshot of one completed invocation.
type Event struct {
	Version            int             `json:"version"`
	RunID              string          `json:"runId"`
	SpanID             string          `json:"spanId"`
	ParentSpanID       string          `json:"parentSpanId,omitempty"`
	TransferFromSpanID string          `json:"transferFromSpanId,omitempty"`
	Type               string          `json:"type"`
	Name               string          `json:"name"`
	Description        string          `json:"description,omitempty"`
	FlowIndex          *int            `json:"flowIndex,omitempty"`
	AgentName          string          `json:"agentName,omitempty"`
	StartedAt          time.Time       `json:"startedAt"`
	DurationNs         int64           `json:"durationNs"`
	Status             string          `json:"status"`
	Input              json.RawMessage `json:"input"`
	Output             json.RawMessage `json:"output"`
	Error              string          `json:"error,omitempty"`
}

type recorderKey struct{}
type parentKey struct{}

type recorder struct {
	runID  string
	next   atomic.Uint64
	write  func(context.Context, Event)
	mu     sync.Mutex
	agents map[string]*Span
}

// Begin attaches a per-Run recorder. The writer must not retain a mutable
// reference to the Context: concurrent Tool calls may emit at the same time.
func Begin(ctx context.Context, runID string, write func(context.Context, Event)) context.Context {
	return context.WithValue(ctx, recorderKey{}, &recorder{
		runID: runID, write: write, agents: make(map[string]*Span),
	})
}

type Span struct {
	ctx     context.Context
	event   Event
	started time.Time
	done    atomic.Bool
}

func Start(ctx context.Context, kind, name string, index *int, agentName string, input any, description ...string) (context.Context, *Span) {
	rec, _ := ctx.Value(recorderKey{}).(*recorder)
	if rec == nil {
		return ctx, nil
	}
	id := fmt.Sprintf("s_%d", rec.next.Add(1))
	parent, _ := ctx.Value(parentKey{}).(string)
	now := time.Now()
	s := &Span{ctx: ctx, started: now, event: Event{
		Version: 1, RunID: rec.runID, SpanID: id, ParentSpanID: parent,
		Type: kind, Name: name, FlowIndex: index, AgentName: agentName,
		StartedAt: now, Input: Snapshot(input),
	}}
	if len(description) > 0 {
		s.event.Description = description[0]
	}
	return context.WithValue(ctx, parentKey{}, id), s
}

func agentKey(parent, name string) string { return parent + "\x00" + name }

// StartAgent adds each ChatModelAgent invocation under its flow (or Batch item).
// The selecting Agent finishes on transfer; its chosen child is the next
// sibling, with a separate transfer link instead of a false parent span.
// Batch items use distinct flow parent IDs, preventing name collisions.
func StartAgent(ctx context.Context, name, parentName string, input any, description string) context.Context {
	rec, _ := ctx.Value(recorderKey{}).(*recorder)
	if rec == nil {
		return ctx
	}
	flowParent, _ := ctx.Value(parentKey{}).(string)
	rec.mu.Lock()
	transferFrom := ""
	if parentName != "" {
		if parent := rec.agents[agentKey(flowParent, parentName)]; parent != nil {
			transferFrom = parent.event.SpanID
		}
	}
	nextCtx, span := Start(ctx, "agent", name, nil, name, input, description)
	span.event.TransferFromSpanID = transferFrom
	rec.agents[agentKey(flowParent, name)] = span
	rec.mu.Unlock()
	return nextCtx
}

// FinishAgent is driven by the Eino event stream: transfer closes the parent,
// while draining the iterator closes whichever Agent actually ran last.
func FinishAgent(ctx context.Context, name string, output any, status string, err error) {
	rec, _ := ctx.Value(recorderKey{}).(*recorder)
	if rec == nil {
		return
	}
	flowParent, _ := ctx.Value(parentKey{}).(string)
	rec.mu.Lock()
	span := rec.agents[agentKey(flowParent, name)]
	rec.mu.Unlock()
	// Keep the completed parent's identity until the whole Agent run ends.
	// Eino starts the transferred child after emitting the transfer event.
	span.End(output, status, err)
}

func FinishAgents(ctx context.Context, output any, err error) {
	rec, _ := ctx.Value(recorderKey{}).(*recorder)
	if rec == nil {
		return
	}
	flowParent, _ := ctx.Value(parentKey{}).(string)
	prefix := flowParent + "\x00"
	rec.mu.Lock()
	var spans []*Span
	for key, span := range rec.agents {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			spans = append(spans, span)
			delete(rec.agents, key)
		}
	}
	rec.mu.Unlock()
	for _, span := range spans {
		span.End(output, "", err)
	}
}

func (s *Span) End(output any, status string, err error) {
	if s == nil || !s.done.CompareAndSwap(false, true) {
		return
	}
	s.event.DurationNs = time.Since(s.started).Nanoseconds()
	s.event.Output = Snapshot(output)
	s.event.Status = status
	if err != nil {
		s.event.Error = err.Error()
		if status == "" {
			s.event.Status = "error"
		}
	}
	if s.event.Status == "" {
		s.event.Status = "success"
	}
	rec, _ := s.ctx.Value(recorderKey{}).(*recorder)
	if rec == nil || rec.write == nil {
		return
	}
	// Diagnostics cannot change an application result.
	defer func() { _ = recover() }()
	rec.write(s.ctx, s.event)
}

func Snapshot(value any) (result json.RawMessage) {
	if value == nil {
		return json.RawMessage("null")
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			result = unavailable(value, fmt.Sprintf("JSON encoder panic: %v", recovered))
		}
	}()
	// encoding/json writes an error interface as {}, losing each Batch item's
	// actual failure. This conversion affects Trace only, not the Run result.
	if batch, ok := batchSnapshot(value); ok {
		value = batch
	}
	data, err := json.Marshal(value)
	if err != nil {
		return unavailable(value, err.Error())
	}
	return json.RawMessage(data)
}

func batchSnapshot(value any) (any, bool) {
	v := reflect.ValueOf(value)
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil, false
		}
		v = v.Elem()
	}
	t := v.Type()
	if t.PkgPath() != "github.com/lubanAgent/luban/sdk" ||
		!strings.HasPrefix(t.Name(), "BatchResult[") {
		return nil, false
	}
	items := v.FieldByName("Items")
	if !items.IsValid() || items.Kind() != reflect.Slice {
		return nil, false
	}
	copied := make([]any, items.Len())
	for i := 0; i < items.Len(); i++ {
		item := items.Index(i)
		fieldValue, fieldErr := item.FieldByName("Value"), item.FieldByName("Err")
		if !fieldValue.IsValid() || !fieldErr.IsValid() {
			return nil, false
		}
		var errText any
		if !fieldErr.IsNil() {
			errText = fieldErr.Interface().(error).Error()
		}
		copied[i] = map[string]any{"Value": fieldValue.Interface(), "Err": errText}
	}
	return map[string]any{"Items": copied}, true
}

func unavailable(value any, reason string) json.RawMessage {
	typ := "<nil>"
	if t := reflect.TypeOf(value); t != nil {
		typ = t.String()
	}
	data, _ := json.Marshal(map[string]any{
		"_traceUnavailable": map[string]string{"type": typ, "reason": reason},
	})
	return data
}
