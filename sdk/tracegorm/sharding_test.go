package tracegorm

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestSpanShardStableAndSameRunCoLocated(t *testing.T) {
	// Independently calculated CRC32 IEEE fixtures freeze the on-disk routing.
	for runID, want := range map[string]string{
		"run_test":                             "tblLubanTraceSpan0",
		"run_00000000000000000000000000000000": "tblLubanTraceSpan17",
		"run_ffffffffffffffffffffffffffffffff": "tblLubanTraceSpan4",
		"run_8b051387e4dbc0851bdf258a617eb9f6": "tblLubanTraceSpan11",
	} {
		if got := SpanTable(runID); got != want {
			t.Fatalf("SpanTable(%q)=%s want %s", runID, got, want)
		}
	}
	seen := map[string]bool{}
	db := testDB(t, true)
	w := testWriter(t, db, func(o *Options) {
		o.QueueSize, o.BatchSize, o.FlushInterval = 100, 100, time.Hour
	})
	runIDs := map[string]string{}
	for i := range 1000 {
		runID := fmt.Sprintf("run_%032x", i)
		name := SpanTable(runID)
		const prefix = "tblLubanTraceSpan"
		suffix := strings.TrimPrefix(name, prefix)
		index, err := strconv.Atoi(suffix)
		if !strings.HasPrefix(name, prefix) || err != nil ||
			index < 0 || index >= spanShardCount || strconv.Itoa(index) != suffix {
			t.Fatalf("invalid shard name %q", name)
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		runIDs[name] = runID
		a, b := exampleEvent("s_2"), exampleEvent("s_3")
		a.RunID, b.RunID = runID, runID
		w.WriteTrace(nil, a)
		w.WriteTrace(nil, b)
		// Retry retains the original event; it must not land in a new shard.
		w.WriteTrace(nil, a)
	}
	if err := closeWriter(t, w); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 20 {
		t.Fatalf("shard distribution: %v", seen)
	}
	for name, runID := range runIDs {
		var count int64
		if err := db.Table(name).Where("run_id = ?", runID).Count(&count).Error; err != nil || count != 2 {
			t.Fatalf("%s: count=%d err=%v", name, count, err)
		}
	}
	if rows := readShardedEvents(t, db); len(rows) != 40 || w.Stats().Persisted != 60 {
		t.Fatalf("rows=%d stats=%+v", len(rows), w.Stats())
	}
}

func TestMissingUnusedShardDoesNotBlockNew(t *testing.T) {
	db := testDB(t, true)
	shard := "tblLubanTraceSpan19"
	if err := db.Exec("DROP TABLE " + shard).Error; err != nil {
		t.Fatal(err)
	}
	w, err := New(context.Background(), db, Options{Service: "test"})
	if err != nil {
		t.Fatalf("New inspected unused shard schema: %v", err)
	}
	if err := closeWriter(t, w); err != nil {
		t.Fatal(err)
	}
}

func TestSpanShardAtomicWrite(t *testing.T) {
	db := testDB(t, true)
	w := testWriter(t, db, nil)
	root := rootEvent()
	w.WriteTrace(nil, root)
	if err := closeWriter(t, w); err != nil {
		t.Fatal(err)
	}
	var runCount, spanCount int64
	if err := db.Table("tblLubanTraceRun").Count(&runCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table(SpanTable(root.RunID)).Count(&spanCount).Error; err != nil {
		t.Fatal(err)
	}
	if runCount != 1 || spanCount != 1 {
		t.Fatalf("run=%d span=%d", runCount, spanCount)
	}
}

func TestCrossShardFailureRollsBackWholeBatch(t *testing.T) {
	for _, failureTable := range []string{"tblLubanTraceSpan17", "tblLubanTraceRun"} {
		t.Run(failureTable, func(t *testing.T) {
			db := testDB(t, true)
			store, err := newGormStore(db)
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Exec("CREATE TRIGGER reject_trace BEFORE INSERT ON " + failureTable +
				" BEGIN SELECT RAISE(ABORT, 'injected failure'); END;").Error; err != nil {
				t.Fatal(err)
			}
			first, second := rootEvent(), rootEvent()
			second.RunID = "run_00000000000000000000000000000000"
			one, err := encode(first, "test", Metadata{})
			if err != nil {
				t.Fatal(err)
			}
			two, err := encode(second, "test", Metadata{})
			if err != nil {
				t.Fatal(err)
			}
			batch := []entry{one, two}
			if err := store.write(context.Background(), batch); err == nil {
				t.Fatal("injected failure succeeded")
			}
			var count int64
			if err := db.Table("tblLubanTraceRun").Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if count != 0 || len(readShardedEvents(t, db)) != 0 {
				t.Fatal("failed transaction partially committed")
			}
			if err := db.Exec("DROP TRIGGER reject_trace").Error; err != nil {
				t.Fatal(err)
			}
			// An uncertain commit can be retried with the same routing.
			for range 2 {
				if err := store.write(context.Background(), batch); err != nil {
					t.Fatal(err)
				}
			}
			if err := db.Table("tblLubanTraceRun").Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if count != 2 || len(readShardedEvents(t, db)) != 2 {
				t.Fatal("cross-shard retry lost or duplicated events")
			}
		})
	}
}

func readShardedEvents(t *testing.T, db *gorm.DB) []spanRow {
	t.Helper()
	var rows []spanRow
	for i := range spanShardCount {
		var shardRows []spanRow
		if err := db.Table(spanTableByIndex(i)).Order("started_at, span_id").Find(&shardRows).Error; err != nil {
			t.Fatal(err)
		}
		rows = append(rows, shardRows...)
	}
	return rows
}
