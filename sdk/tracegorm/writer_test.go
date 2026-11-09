package tracegorm

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	luban "github.com/lubanAgent/luban/sdk"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

//go:embed testdata/schema.sqlite.sql
var sqliteSchema string

func testDB(t *testing.T, create bool) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "trace.sqlite")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
		// Verify that host table naming cannot redirect private trace rows.
		NamingStrategy: schema.NamingStrategy{TablePrefix: "business_", SingularTable: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if create {
		if err := db.Exec(sqliteSchema).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func testOptions(t *testing.T, change func(*Options)) Options {
	t.Helper()
	o := Options{Service: "test", OnError: func(WriteError) {}}
	if change != nil {
		change(&o)
	}
	o, err := o.normalized()
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func testWriter(t *testing.T, db *gorm.DB, change func(*Options)) *Writer {
	t.Helper()
	w, err := New(context.Background(), db, testOptions(t, change))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = closeWriter(t, w) })
	return w
}

func closeWriter(t *testing.T, w *Writer) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := w.Close(ctx)
	if errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, ErrPersistence) {
		t.Fatal("writer did not settle")
	}
	return err
}

func exampleEvent(id string) luban.TraceEvent {
	return luban.TraceEvent{
		Version: 1, RunID: "run_test", SpanID: id, ParentSpanID: "s_root", Type: "function",
		Name: "Prepare", Description: "准备材料😀", AgentName: "agent",
		StartedAt:  time.Date(2026, 11, 2, 12, 30, 0, 123456789, time.FixedZone("local", 8*3600)),
		DurationNs: 987654321, Status: "success",
		Input:  json.RawMessage(`{"text":"中文😀","number":9007199254740993}`),
		Output: json.RawMessage(`{"ok":true}`),
	}
}

func rootEvent() luban.TraceEvent {
	e := exampleEvent("s_root")
	e.Type, e.Name, e.ParentSpanID = "run", "review", ""
	return e
}

func readEvents(t *testing.T, db *gorm.DB) []luban.TraceEvent {
	t.Helper()
	rows := readShardedEvents(t, db)
	events := make([]luban.TraceEvent, len(rows))
	for i, row := range rows {
		if err := json.Unmarshal([]byte(row.EventJSON), &events[i]); err != nil {
			t.Fatal(err)
		}
	}
	return events
}

func assertStats(t *testing.T, w *Writer) Stats {
	t.Helper()
	s := w.Stats()
	if s.Received != s.Accepted+s.Rejected || s.Accepted != s.Persisted+s.Failed+s.Pending {
		t.Fatalf("counter invariants: %+v", s)
	}
	return s
}

func TestDefaultsAndValidation(t *testing.T) {
	o, err := (Options{Service: "record"}).normalized()
	if err != nil || o.QueueSize != 1024 || o.BatchSize != 50 || o.MaxEventBytes != 8<<20 ||
		o.MaxPendingBytes != 32<<20 || o.BatchBytes != 1<<20 || o.FlushInterval != 250*time.Millisecond ||
		o.WriteTimeout != 2*time.Second || o.OnError == nil {
		t.Fatalf("defaults=%+v err=%v", o, err)
	}
	for name, mutate := range map[string]func(*Options){
		"empty service": func(o *Options) { o.Service = " " },
		"service size":  func(o *Options) { o.Service = strings.Repeat("中", 129) },
		"utf8 service":  func(o *Options) { o.Service = "\xff" },
		"queue":         func(o *Options) { o.QueueSize = -1 },
		"batch":         func(o *Options) { o.BatchSize = -1 },
		"bytes":         func(o *Options) { o.MaxPendingBytes = -1 },
		"event bytes":   func(o *Options) { o.MaxEventBytes = -1 },
		"batch bytes":   func(o *Options) { o.BatchBytes = -1 },
		"flush":         func(o *Options) { o.FlushInterval = -1 },
		"timeout":       func(o *Options) { o.WriteTimeout = -1 },
		"count fit":     func(o *Options) { o.QueueSize = 1 },
		"event fit":     func(o *Options) { o.MaxPendingBytes = 1 << 20 },
		"batch fit":     func(o *Options) { o.BatchBytes = 33 << 20 },
	} {
		t.Run(name, func(t *testing.T) {
			o := Options{Service: "test"}
			mutate(&o)
			if _, err := o.normalized(); err == nil {
				t.Fatal("invalid option accepted")
			}
		})
	}
	if o, err := (Options{Service: strings.Repeat("中", 128)}).normalized(); err != nil {
		t.Fatalf("expanded Service limit rejected: %v", err)
	} else if o.Service == "" {
		t.Fatal("Service was not preserved")
	}
}

func TestExpandedTraceFieldLimits(t *testing.T) {
	event := exampleEvent(strings.Repeat("s", 64))
	event.RunID = strings.Repeat("r", 64)
	event.Type = strings.Repeat("t", 64)
	if err := validateEvent(event); err != nil {
		t.Fatalf("valid expanded fields rejected: %v", err)
	}
	metadata, err := readMetadata(func(*gin.Context) Metadata {
		return Metadata{
			BusinessType: strings.Repeat("类", 64),
			BusinessID:   strings.Repeat("业", 255),
			RequestID:    strings.Repeat("请", 255),
		}
	}, nil)
	if err != nil || metadata.BusinessID == "" {
		t.Fatalf("expanded metadata limit rejected: %+v %v", metadata, err)
	}
}

func TestNewChecksWithoutCreatingOrClosingDatabase(t *testing.T) {
	db := testDB(t, false)
	for name, handle := range map[string]*gorm.DB{
		"nil": nil, "zero": {},
		"errored": db.Session(&gorm.Session{}), "dry run": db.Session(&gorm.Session{DryRun: true}),
	} {
		t.Run(name, func(t *testing.T) {
			if name == "errored" {
				handle.Error = errors.New("host error")
			}
			if _, err := New(context.Background(), handle, Options{Service: "test"}); err == nil {
				t.Fatal("invalid connection accepted")
			}
		})
	}
	emptyWriter, err := New(context.Background(), db, Options{Service: "test"})
	if err != nil {
		t.Fatalf("empty database without Trace tables rejected: %v", err)
	}
	if err := closeWriter(t, emptyWriter); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Raw(`SELECT COUNT(*) FROM sqlite_master WHERE type='table'`).Scan(&count).Error; err != nil || count != 0 {
		t.Fatalf("constructor mutated DB: count=%d err=%v", count, err)
	}
	if err := db.Exec(sqliteSchema).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := New(nil, db, Options{Service: "test"}); err == nil {
		t.Fatal("nil context accepted")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New(cancelled, db, Options{Service: "test"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("startup cancellation: %v", err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if _, err := New(context.Background(), tx, Options{Service: "test"}); err == nil {
			t.Error("transaction accepted")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	w := testWriter(t, db, nil)
	if err := closeWriter(t, w); err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	if err := sqlDB.Ping(); err != nil {
		t.Fatalf("writer closed host database: %v", err)
	}
}

func TestSessionClearsHostConditionsAndCancelledContext(t *testing.T) {
	db := testDB(t, true)
	parent, cancel := context.WithCancel(context.Background())
	cancel()
	handle := db.WithContext(parent).Table("not_trace").Where("missing_column = ?", 1).
		Scopes(func(db *gorm.DB) *gorm.DB { panic("host business scope must not run") })
	w := testWriter(t, handle, nil)
	w.WriteTrace(nil, exampleEvent("s_2"))
	if err := closeWriter(t, w); err != nil {
		t.Fatal(err)
	}
	if len(readEvents(t, db)) != 1 || handle.Statement.Table != "not_trace" || handle.Statement.Context != parent {
		t.Fatal("session isolation failed")
	}
}

func TestRoundtripMetadataDuplicatesAndSnapshot(t *testing.T) {
	db := testDB(t, true)
	var calls atomic.Int32
	w := testWriter(t, db, func(o *Options) {
		o.FlushInterval = time.Hour // enqueue snapshot can be mutated before drain
		o.Metadata = func(ctx *gin.Context) Metadata {
			calls.Add(1)
			return Metadata{BusinessType: "lesson", BusinessID: ctx.GetString("id"), RequestID: "req_1"}
		}
	})
	ctx := &gin.Context{}
	ctx.Set("id", "123")
	event := exampleEvent("s_2")
	index := 3
	event.FlowIndex = &index
	w.WriteTrace(ctx, event)
	copy(event.Input, []byte(strings.Repeat(" ", len(event.Input))))
	index = 99
	root := rootEvent()
	w.WriteTrace(ctx, root)
	duplicate := root
	duplicate.Status, duplicate.Output = "error", json.RawMessage(`"later"`)
	w.WriteTrace(ctx, duplicate)
	ctx.Set("id", "changed")
	if err := closeWriter(t, w); err != nil {
		t.Fatal(err)
	}
	s := assertStats(t, w)
	if s.Persisted != 3 || s.Pending != 0 || s.PendingBytes != 0 || calls.Load() != 2 {
		t.Fatalf("stats=%+v metadataCalls=%d", s, calls.Load())
	}
	events := readEvents(t, db)
	if len(events) != 2 || *events[0].FlowIndex != 3 || string(events[0].Input) != string(exampleEvent("s_2").Input) ||
		events[0].StartedAt.Format(time.RFC3339Nano) != exampleEvent("s_2").StartedAt.Format(time.RFC3339Nano) ||
		events[1].Status != "success" {
		t.Fatalf("roundtrip/snapshot/first-write failed: %+v", events)
	}
	var row runRow
	if err := db.First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.BusinessID != "123" || row.RequestID != "req_1" || row.AgentKey != "review" ||
		row.Service != "test" || row.Status != "success" ||
		!row.StartedAt.Equal(root.StartedAt.UTC().Truncate(time.Microsecond)) || row.DurationNs != root.DurationNs ||
		row.CreateTime <= 0 || row.Deleted != 0 {
		t.Fatalf("bad run projection: %+v", row)
	}
}

func TestUTCDatetimeIsIndependentOfHostDSNLocation(t *testing.T) {
	local := time.FixedZone("host", 8*3600)
	timestamp := time.Date(2026, 11, 2, 12, 30, 0, 123456789, local)
	value, err := (utcTime{timestamp}).Value()
	if err != nil || value != "2026-11-02 04:30:00.123456" {
		t.Fatalf("driver parameter must be a UTC string: %v %v", value, err)
	}
	// parseTime with loc=Local labels the stored UTC wall clock with local
	// timezone. The scanner must not subtract the offset a second time.
	driverValue := time.Date(2026, 11, 2, 4, 30, 0, 123456000, local)
	var read utcTime
	if err := read.Scan(driverValue); err != nil || !read.Equal(timestamp.UTC().Truncate(time.Microsecond)) {
		t.Fatalf("driver timezone affected UTC index: %v %v", read, err)
	}
	if err := read.Scan([]byte(value.(string))); err != nil || !read.Equal(timestamp.UTC().Truncate(time.Microsecond)) {
		t.Fatalf("parseTime=false scan: %v %v", read, err)
	}
}

func TestDefaultEightMiBBoundaryPreservesFullEvent(t *testing.T) {
	o := testOptions(t, nil)
	e := exampleEvent("boundary")
	e.Input = json.RawMessage(`""`)
	base, _ := encode(e, o.Service, Metadata{})
	payload := strings.Repeat("x", int(o.MaxEventBytes-base.bytes))
	e.Input, _ = json.Marshal(payload)
	length := make(chan int, 1)
	w := startWriter(storeFunc(func(_ context.Context, entries []entry) error {
		var stored luban.TraceEvent
		if err := json.Unmarshal([]byte(entries[0].span.EventJSON), &stored); err != nil {
			return err
		}
		var restored string
		if err := json.Unmarshal(stored.Input, &restored); err != nil {
			return err
		}
		length <- len(restored)
		return nil
	}), o)
	w.WriteTrace(nil, e)
	if got := await(t, length); got != len(payload) {
		t.Fatalf("large payload truncated: %d != %d", got, len(payload))
	}
	e.Input, _ = json.Marshal(payload + "x")
	w.WriteTrace(nil, e)
	if err := closeWriter(t, w); err != nil {
		t.Fatal(err)
	}
	if s := assertStats(t, w); s.Persisted != 1 || s.Rejected != 1 {
		t.Fatalf("8MiB boundary: %+v", s)
	}
}

func TestInvalidEventsAreVisibleAndNotTruncated(t *testing.T) {
	db := testDB(t, true)
	var reports []WriteError
	w := testWriter(t, db, func(o *Options) {
		o.OnError = func(e WriteError) { reports = append(reports, e) }
	})
	mutations := []func(*luban.TraceEvent){
		func(e *luban.TraceEvent) { e.Version = 2 },
		func(e *luban.TraceEvent) { e.RunID = "" },
		func(e *luban.TraceEvent) { e.SpanID = strings.Repeat("s", 65) },
		func(e *luban.TraceEvent) { e.ParentSpanID = "尾" },
		func(e *luban.TraceEvent) { e.TransferFromSpanID = strings.Repeat("s", 65) },
		func(e *luban.TraceEvent) { e.Type = "" },
		func(e *luban.TraceEvent) { e.Status = "running" },
		func(e *luban.TraceEvent) { e.Status = "skipped"; e.SkipReason = "empty_input" }, // only a flow Agent may skip
		func(e *luban.TraceEvent) { e.SkipReason = "empty_input" },                       // successful nodes cannot claim a skip reason
		func(e *luban.TraceEvent) { e.StartedAt = time.Time{} },
		func(e *luban.TraceEvent) { e.StartedAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC) },
		func(e *luban.TraceEvent) { e.DurationNs = -1 },
		func(e *luban.TraceEvent) { n := -1; e.FlowIndex = &n },
		func(e *luban.TraceEvent) { e.Name = "\xff" },
		func(e *luban.TraceEvent) { e.Input = json.RawMessage("{") },
		func(e *luban.TraceEvent) { e.Output = json.RawMessage{'"', 0xff, '"'} },
		func(e *luban.TraceEvent) { e.Type = "run" }, // parent not empty
		func(e *luban.TraceEvent) { *e = rootEvent(); e.Name = strings.Repeat("中", 256) },
	}
	for _, mutate := range mutations {
		event := exampleEvent("invalid")
		mutate(&event)
		w.WriteTrace(nil, event)
	}
	emptyKey := rootEvent()
	emptyKey.Name, emptyKey.Status, emptyKey.Error = "", "error", "missing Agent key"
	emptyKey.Input, emptyKey.Output = nil, nil
	w.WriteTrace(nil, emptyKey)
	future := exampleEvent("future")
	future.Type, future.Name = "futureKind", strings.Repeat("中文", 200)
	w.WriteTrace(nil, future)
	if err := closeWriter(t, w); err != nil {
		t.Fatal(err)
	}
	s := assertStats(t, w)
	if s.Rejected != uint64(len(mutations)) || s.Persisted != 2 || len(reports) != len(mutations) {
		t.Fatalf("stats=%+v reports=%v", s, reports)
	}
	for _, report := range reports {
		if !errors.Is(report.Err, ErrInvalidEvent) {
			t.Fatalf("unclassified error: %v", report)
		}
	}
	for _, e := range readEvents(t, db) {
		if e.Type == "run" && (string(e.Input) != "null" || string(e.Output) != "null") {
			t.Fatalf("null not normalized: %+v", e)
		}
	}
}

func TestMetadataFailureAndErrorCallbackPanicPreserveEvent(t *testing.T) {
	for _, mode := range []string{"panic", "oversize", "nil context"} {
		t.Run(mode, func(t *testing.T) {
			db := testDB(t, true)
			var called atomic.Int32
			w := testWriter(t, db, func(o *Options) {
				o.Metadata = func(*gin.Context) Metadata {
					called.Add(1)
					if mode == "panic" {
						panic("bad metadata")
					}
					return Metadata{BusinessID: strings.Repeat("x", 256)}
				}
				o.OnError = func(WriteError) { panic("bad logger") }
			})
			ctx := &gin.Context{}
			if mode == "nil context" {
				ctx = nil
			}
			w.WriteTrace(ctx, rootEvent())
			if err := closeWriter(t, w); err != nil {
				t.Fatal(err)
			}
			var row runRow
			if err := db.First(&row).Error; err != nil || row.BusinessID != "" {
				t.Fatalf("metadata failure lost event: %+v %v", row, err)
			}
			if mode == "nil context" && called.Load() != 0 {
				t.Fatal("callback invoked with nil Gin context")
			}
		})
	}
}

type storeFunc func(context.Context, []entry) error

func (f storeFunc) write(ctx context.Context, entries []entry) error { return f(ctx, entries) }

func await[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("synchronization timed out")
		var zero T
		return zero
	}
}

func TestBudgetsIncludeInFlightAndCloseTimeoutKeepsDraining(t *testing.T) {
	for _, mode := range []string{"count", "bytes"} {
		t.Run(mode, func(t *testing.T) {
			began := make(chan struct{}, 1)
			release := make(chan struct{})
			o := testOptions(t, func(o *Options) { o.QueueSize = 1; o.BatchSize = 1 })
			item, _ := encode(exampleEvent("one"), o.Service, Metadata{})
			if mode == "bytes" {
				o.QueueSize, o.MaxPendingBytes, o.MaxEventBytes, o.BatchBytes = 10, item.bytes, item.bytes, item.bytes
			}
			reports := make(chan WriteError, 4)
			o.OnError = func(e WriteError) { reports <- e }
			w := startWriter(storeFunc(func(ctx context.Context, batch []entry) error {
				began <- struct{}{}
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}), o)
			t.Cleanup(func() { _ = closeWriter(t, w) })
			w.WriteTrace(nil, exampleEvent("one"))
			await(t, began)
			w.WriteTrace(nil, exampleEvent("two"))
			if e := await(t, reports); !errors.Is(e.Err, ErrQueueFull) {
				t.Fatalf("budget rejected wrong: %v", e)
			}
			timeout, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
			defer cancel()
			if err := w.Close(timeout); !errors.Is(err, context.DeadlineExceeded) || w.Stats().Pending != 1 {
				t.Fatalf("Close cancelled drain: %v %+v", err, w.Stats())
			}
			w.WriteTrace(nil, exampleEvent("three"))
			if e := await(t, reports); !errors.Is(e.Err, ErrClosed) {
				t.Fatalf("closing writer accepted: %v", e)
			}
			close(release)
			if err := closeWriter(t, w); err != nil {
				t.Fatal(err)
			}
			s := assertStats(t, w)
			if s.Persisted != 1 || s.Rejected != 2 || s.PendingBytes != 0 {
				t.Fatalf("bad final stats: %+v", s)
			}
		})
	}
}

func TestOversizeRejectionAndLargeSingleBatch(t *testing.T) {
	o := testOptions(t, nil)
	e := exampleEvent("large")
	e.Input, _ = json.Marshal(strings.Repeat("😀", 2048))
	item, _ := encode(e, o.Service, Metadata{})
	o.MaxEventBytes, o.BatchBytes, o.FlushInterval = item.bytes, 100, time.Hour
	batches := make(chan int, 2)
	w := startWriter(storeFunc(func(_ context.Context, batch []entry) error {
		batches <- len(batch)
		return nil
	}), o)
	w.WriteTrace(nil, e)
	if n := await(t, batches); n != 1 {
		t.Fatalf("large event did not form its own batch: %d", n)
	}
	e.Input, _ = json.Marshal(strings.Repeat("😀", 2049))
	w.WriteTrace(nil, e)
	if err := closeWriter(t, w); err != nil {
		t.Fatal(err)
	}
	if s := assertStats(t, w); s.Persisted != 1 || s.Rejected != 1 {
		t.Fatalf("oversize stats: %+v", s)
	}
}

func TestRetryFailureContinuesAndCloseRetainsFailure(t *testing.T) {
	transient := errors.New("temporary failure")
	var attempts sync.Map
	reports := make(chan WriteError, 4)
	o := testOptions(t, func(o *Options) {
		o.BatchSize = 1
		o.OnError = func(e WriteError) { reports <- e }
	})
	w := startWriter(storeFunc(func(_ context.Context, batch []entry) error {
		id := batch[0].span.SpanID
		counter, _ := attempts.LoadOrStore(id, &atomic.Int32{})
		n := counter.(*atomic.Int32).Add(1)
		if id == "fail" || (id == "recover" && n < 3) {
			return transient
		}
		return nil
	}), o)
	for _, id := range []string{"fail", "recover", "ok"} {
		w.WriteTrace(nil, exampleEvent(id))
	}
	err := closeWriter(t, w)
	if !errors.Is(err, ErrPersistence) || !errors.Is(err, transient) {
		t.Fatalf("failure lost: %v", err)
	}
	s := assertStats(t, w)
	if s.Persisted != 2 || s.Failed != 1 || s.WriteFailures != 5 || s.PendingBytes != 0 {
		t.Fatalf("retry stats: %+v", s)
	}
	for id, want := range map[string]int32{"fail": 3, "recover": 3, "ok": 1} {
		counter, _ := attempts.Load(id)
		if counter.(*atomic.Int32).Load() != want {
			t.Errorf("%s attempts wrong", id)
		}
	}
	if e := await(t, reports); e.Operation != "persist" || e.RunID != "" || e.EventCount != 1 ||
		!errors.Is(e.Err, transient) || len(reports) != 0 {
		t.Fatalf("unexpected failure reports: %+v", e)
	}
	if err := closeWriter(t, w); !errors.Is(err, ErrPersistence) {
		t.Fatal("repeat Close lost failure")
	}
}

func TestWorkerPanicFailsAllUnresolvedAndDoesNotHang(t *testing.T) {
	o := testOptions(t, func(o *Options) { o.FlushInterval = time.Hour })
	w := startWriter(storeFunc(func(context.Context, []entry) error { panic("store crash") }), o)
	for i := range 3 {
		w.WriteTrace(nil, exampleEvent(fmt.Sprintf("s_%d", i)))
	}
	if err := closeWriter(t, w); !errors.Is(err, ErrPersistence) {
		t.Fatalf("panic not reported: %v", err)
	}
	w.WriteTrace(nil, exampleEvent("later"))
	if s := assertStats(t, w); s.Failed != 3 || s.Pending != 0 || s.PendingBytes != 0 || s.Rejected != 1 {
		t.Fatalf("panic counters: %+v", s)
	}
}

func TestConcurrentCloseAndWrite(t *testing.T) {
	o := testOptions(t, nil)
	w := startWriter(storeFunc(func(context.Context, []entry) error { return nil }), o)
	w.WriteTrace(nil, exampleEvent("first"))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			w.WriteTrace(nil, exampleEvent(fmt.Sprintf("s_%d", i)))
		}(i)
	}
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if err := closeWriter(t, w); err != nil {
				t.Error(err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if err := closeWriter(t, w); err != nil {
		t.Fatal(err)
	}
	s := assertStats(t, w)
	if s.Received != 101 || s.Accepted != s.Persisted || s.Pending != 0 {
		t.Fatalf("concurrent counters: %+v", s)
	}
}

func TestFlushTimerAndIndependentWriteDeadline(t *testing.T) {
	began := make(chan bool, 3)
	o := testOptions(t, func(o *Options) {
		o.FlushInterval = time.Millisecond
		o.WriteTimeout = 5 * time.Millisecond
	})
	w := startWriter(storeFunc(func(ctx context.Context, _ []entry) error {
		_, bounded := ctx.Deadline()
		began <- bounded
		<-ctx.Done()
		return ctx.Err()
	}), o)
	w.WriteTrace(nil, exampleEvent("timer"))
	if !await(t, began) {
		t.Fatal("database attempt has no timeout")
	}
	if err := closeWriter(t, w); !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, ErrPersistence) {
		// This DeadlineExceeded is the retained DB error, not a Close timeout.
		t.Fatalf("deadline not retained: %v", err)
	}
}

func TestTransactionRollbackAndIncompleteRun(t *testing.T) {
	db := testDB(t, true)
	if err := db.Exec(`CREATE TRIGGER fail_run BEFORE INSERT ON tblLubanTraceRun BEGIN
		SELECT RAISE(ABORT, 'run projection failed'); END;`).Error; err != nil {
		t.Fatal(err)
	}
	w := testWriter(t, db, func(o *Options) { o.FlushInterval = time.Hour })
	w.WriteTrace(nil, exampleEvent("s_2"))
	w.WriteTrace(nil, rootEvent())
	if err := closeWriter(t, w); !errors.Is(err, ErrPersistence) {
		t.Fatalf("transaction unexpectedly succeeded: %v", err)
	}
	if len(readEvents(t, db)) != 0 || w.Stats().Failed != 2 {
		t.Fatal("span writes committed without run projection")
	}
	if err := db.Exec("DROP TRIGGER fail_run").Error; err != nil {
		t.Fatal(err)
	}
	next := testWriter(t, db, nil)
	next.WriteTrace(nil, exampleEvent("s_2"))
	if err := closeWriter(t, next); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&runRow{}).Count(&count).Error; err != nil || count != 0 || len(readEvents(t, db)) != 1 {
		t.Fatalf("incomplete Run fabricated summary: %d %v", count, err)
	}
}
