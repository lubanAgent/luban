package tracegorm

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	luban "github.com/lubanAgent/luban/sdk"
	"gorm.io/gorm"
)

// Writer queues immutable snapshots and persists them with one background
// worker. Obtain one through New; its zero value is not usable. The host owns
// the database and must stop Runs, Close the writer, then close the database.
type Writer struct {
	options Options
	store   store
	ctx     context.Context
	cancel  context.CancelFunc

	mu           sync.Mutex
	queue        []entry
	stats        Stats
	closing      bool
	firstFailure error
	wake         chan struct{}
	done         chan struct{}
}

var _ luban.TraceWriter = (*Writer)(nil)

// New validates local options and the injected base Gorm handle, then starts a
// worker. It performs no ping, schema inspection, migration, or table creation;
// the host guarantees the Trace tables are ready. Cancelling ctx after New
// succeeds does not stop persistence; use Close for shutdown.
func New(ctx context.Context, db *gorm.DB, options Options) (*Writer, error) {
	if ctx == nil {
		return nil, fmt.Errorf("tracegorm: startup context must not be nil")
	}
	options, err := options.normalized()
	if err != nil {
		return nil, err
	}
	store, err := newGormStore(db)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("tracegorm: startup context: %w", err)
	}
	return startWriter(store, options), nil
}

// startWriter is a private seam for deterministic fault/concurrency tests.
func startWriter(store store, options Options) *Writer {
	ctx, cancel := context.WithCancel(context.Background())
	w := &Writer{store: store, options: options, ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1), done: make(chan struct{})}
	go w.work()
	return w
}

// WriteTrace serializes before returning and never waits for queue capacity or
// database I/O. It may run concurrently. Failures are visible in Stats/OnError;
// they do not change a Runtime's business result.
func (w *Writer) WriteTrace(ctx *gin.Context, event luban.TraceEvent) {
	w.mu.Lock()
	w.stats.Received++
	closing := w.closing
	w.mu.Unlock()
	if closing {
		w.reject(event, ErrClosed)
		return
	}
	if err := validateEvent(event); err != nil {
		w.reject(event, err)
		return
	}
	var metadata Metadata
	if event.Type == "run" && ctx != nil && w.options.Metadata != nil {
		var err error
		metadata, err = readMetadata(w.options.Metadata, ctx)
		if err != nil {
			w.report(WriteError{Operation: "metadata", RunID: event.RunID, SpanID: event.SpanID, EventCount: 1, Err: err})
		}
	}
	item, err := encode(event, w.options.Service, metadata)
	if err != nil {
		w.reject(event, err)
		return
	}
	if item.bytes > w.options.MaxEventBytes {
		w.reject(event, ErrEventTooLarge)
		return
	}
	w.mu.Lock()
	switch {
	case w.closing:
		err = ErrClosed
	case w.stats.Pending >= uint64(w.options.QueueSize) ||
		item.bytes > w.options.MaxPendingBytes-w.stats.PendingBytes:
		err = ErrQueueFull
	default:
		w.queue = append(w.queue, item)
		w.stats.Accepted++
		w.stats.Pending++
		w.stats.PendingBytes += item.bytes
	}
	w.mu.Unlock()
	if err != nil {
		w.reject(event, err)
		return
	}
	w.signal()
}

func (w *Writer) reject(event luban.TraceEvent, err error) {
	w.mu.Lock()
	w.stats.Rejected++
	w.mu.Unlock()
	w.report(WriteError{Operation: "enqueue", RunID: event.RunID, SpanID: event.SpanID, EventCount: 1, Err: err})
}

func (w *Writer) report(err WriteError) {
	defer func() {
		if recovered := recover(); recovered != nil {
			// Do not call the failing callback again or log an arbitrary panic
			// value that might contain business data.
			slog.Error("luban_trace_storage", "operation", "error_callback", "panicType", fmt.Sprintf("%T", recovered))
		}
	}()
	w.options.OnError(err)
}

func (w *Writer) signal() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// Stats returns counters and serialized-byte usage, not process RSS.
func (w *Writer) Stats() Stats {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.stats
}

// Close rejects new events and waits for accepted events to settle. A caller's
// timeout only ends its wait: draining continues and a later Close may wait
// again. Concurrent Close calls are safe. Database connections remain open.
func (w *Writer) Close(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("tracegorm: shutdown context must not be nil")
	}
	w.mu.Lock()
	w.closing = true
	w.mu.Unlock()
	w.signal()
	select {
	case <-w.done:
		return w.closeResult()
	default:
	}
	select {
	case <-w.done:
		return w.closeResult()
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *Writer) closeResult() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.firstFailure != nil {
		return fmt.Errorf("%w: %d accepted events unconfirmed: %w", ErrPersistence, w.stats.Failed, w.firstFailure)
	}
	return nil
}

func (w *Writer) work() {
	defer func() {
		if recovered := recover(); recovered != nil {
			err := fmt.Errorf("%w: worker panicked (%T)", ErrPersistence, recovered)
			w.mu.Lock()
			n := w.stats.Pending
			w.stats.Failed += n
			w.stats.Pending = 0
			w.stats.PendingBytes = 0
			w.queue = nil
			w.closing = true
			if w.firstFailure == nil {
				w.firstFailure = err
			}
			w.mu.Unlock()
			w.report(WriteError{Operation: "worker", EventCount: int(n), Err: err})
		}
		w.cancel()
		// All worker callbacks have returned when Close observes done.
		close(w.done)
	}()
	for {
		batch, wait, finished := w.nextBatch()
		if finished {
			return
		}
		if len(batch) != 0 {
			w.persist(batch)
			continue
		}
		if wait < 0 { // Empty queue: no timer or periodic database work.
			<-w.wake
			continue
		}
		timer := time.NewTimer(wait)
		select {
		case <-w.wake:
		case <-timer.C:
		}
		timer.Stop()
	}
}

func (w *Writer) nextBatch() (batch []entry, wait time.Duration, finished bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.queue) == 0 {
		return nil, -1, w.closing
	}
	n, size := 0, int64(0)
	for n < len(w.queue) && n < w.options.BatchSize {
		next := w.queue[n].bytes
		if n > 0 && next > w.options.BatchBytes-size {
			break
		}
		size += next
		n++
		if size >= w.options.BatchBytes {
			break
		}
	}
	wait = time.Until(w.queue[0].queuedAt.Add(w.options.FlushInterval))
	full := n == w.options.BatchSize || size >= w.options.BatchBytes || n < len(w.queue)
	if !w.closing && !full && wait > 0 {
		return nil, wait, false
	}
	batch = append([]entry(nil), w.queue[:n]...)
	clear(w.queue[:n])
	w.queue = w.queue[n:]
	if len(w.queue) == 0 {
		w.queue = nil
	}
	// Pending budgets are released only after commit or final failure, not here.
	return batch, 0, false
}

func (w *Writer) persist(batch []entry) {
	var err error
attempts:
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(time.Duration(attempt) * 100 * time.Millisecond)
			select {
			case <-timer.C:
			case <-w.ctx.Done():
				timer.Stop()
				err = w.ctx.Err()
				break attempts
			}
		}
		err = w.tryPersist(batch)
		if err == nil {
			break
		}
		w.mu.Lock()
		w.stats.WriteFailures++
		w.mu.Unlock()
	}
	var size int64
	for _, item := range batch {
		size += item.bytes
	}
	w.mu.Lock()
	w.stats.Pending -= uint64(len(batch))
	w.stats.PendingBytes -= size
	if err == nil {
		w.stats.Persisted += uint64(len(batch))
	} else {
		w.stats.Failed += uint64(len(batch))
		if w.firstFailure == nil {
			w.firstFailure = err
		}
	}
	w.mu.Unlock()
	if err != nil {
		w.report(WriteError{Operation: "persist", EventCount: len(batch), Err: fmt.Errorf("%w: %w", ErrPersistence, err)})
	}
}

func (w *Writer) tryPersist(batch []entry) error {
	ctx, cancel := context.WithTimeout(w.ctx, w.options.WriteTimeout)
	defer cancel()
	return w.store.write(ctx, batch)
}
