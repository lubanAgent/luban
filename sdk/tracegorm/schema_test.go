package tracegorm

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type unsupportedDialector struct{ gorm.Dialector }

func (unsupportedDialector) Name() string { return "unsupported" }

func TestUnsupportedDriverIsRejected(t *testing.T) {
	db := testDB(t, true).Session(&gorm.Session{})
	db.Dialector = unsupportedDialector{db.Dialector}
	if _, err := New(context.Background(), db, Options{Service: "test"}); err == nil ||
		!strings.Contains(err.Error(), "unsupported driver") {
		t.Fatalf("driver accepted: %v", err)
	}
}

func TestSQLiteSchemaRejectsMissingColumnsKeysAndIndexes(t *testing.T) {
	for name, change := range map[string]func(string) string{
		"missing column": func(s string) string { return strings.Replace(s, "event_json TEXT NOT NULL,", "", 1) },
		"nullable": func(s string) string {
			return strings.Replace(s, "duration_ns INTEGER NOT NULL", "duration_ns INTEGER", 1)
		},
		"wrong type": func(s string) string { return strings.Replace(s, "duration_ns INTEGER", "duration_ns TEXT", 1) },
		"wrong primary": func(s string) string {
			return strings.Replace(s, "PRIMARY KEY (run_id, span_id)", "PRIMARY KEY (span_id, run_id)", 1)
		},
		"wrong index": func(s string) string {
			return strings.Replace(s, "ON tblLubanTraceSpan00(started_at, run_id, span_id)", "ON tblLubanTraceSpan00(run_id, started_at, span_id)", 1)
		},
		"partial index": func(s string) string {
			return s + "DROP INDEX idx_trace_span_time00; CREATE INDEX idx_trace_span_time00 ON tblLubanTraceSpan00(started_at, run_id, span_id) WHERE status = 'success';"
		},
		"extra unique": func(s string) string { return s + "CREATE UNIQUE INDEX bad_unique ON tblLubanTraceSpan00(kind);" },
		"required extra": func(s string) string {
			return strings.Replace(s, "event_json TEXT NOT NULL,", "event_json TEXT NOT NULL, extra TEXT NOT NULL,", 1)
		},
		"missing index": func(s string) string { return s + "DROP INDEX idx_trace_run_time;" },
	} {
		t.Run(name, func(t *testing.T) {
			db := testDB(t, false)
			if err := db.Exec(change(sqliteSchema)).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := New(context.Background(), db, Options{Service: "test"}); err == nil {
				t.Fatal("incompatible schema accepted")
			}
		})
	}
}

func mysqlMetadata(table tableSpec) ([]mysqlColumn, []mysqlIndex) {
	var columns []mysqlColumn
	for _, c := range table.columns {
		m := mysqlColumn{Name: c.name, Kind: c.kind, Type: c.kind, Nullable: "NO"}
		if c.length != 0 {
			length := c.length
			m.Length = &length
		}
		if c.charset != "" {
			charset, collation := c.charset, c.charset+"_bin"
			m.Charset, m.Collation = &charset, &collation
		}
		if c.kind == "datetime" {
			precision := int64(6)
			m.Precision = &precision
		}
		columns = append(columns, m)
	}
	var indexes []mysqlIndex
	for i, col := range table.primary {
		indexes = append(indexes, mysqlIndex{Name: "PRIMARY", Sequence: i + 1, Column: col})
	}
	for name, cols := range table.indexes {
		for i, col := range cols {
			indexes = append(indexes, mysqlIndex{Name: name, NonUnique: 1, Sequence: i + 1, Column: col})
		}
	}
	return columns, indexes
}

func TestMySQLMetadataRejectsLossySchemas(t *testing.T) {
	table := tables[1]
	columns, indexes := mysqlMetadata(table)
	if err := checkMySQLColumns(table, columns); err != nil {
		t.Fatal(err)
	}
	if err := checkMySQLIndexes(table, indexes); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func([]mysqlColumn) []mysqlColumn{
		"missing": func(c []mysqlColumn) []mysqlColumn { return c[1:] },
		"short":   func(c []mysqlColumn) []mysqlColumn { n := int64(10); c[0].Length = &n; return c },
		"utf8":    func(c []mysqlColumn) []mysqlColumn { v := "utf8mb4"; c[0].Charset = &v; return c },
		"ci":      func(c []mysqlColumn) []mysqlColumn { v := "ascii_general_ci"; c[0].Collation = &v; return c },
		"null":    func(c []mysqlColumn) []mysqlColumn { c[0].Nullable = "YES"; return c },
		"precision": func(c []mysqlColumn) []mysqlColumn {
			n := int64(0)
			c[5].Precision = &n
			return c
		},
		"text capacity": func(c []mysqlColumn) []mysqlColumn { c[7].Kind = "text"; return c },
		"unsigned":      func(c []mysqlColumn) []mysqlColumn { c[6].Type = "bigint unsigned"; return c },
		"generated":     func(c []mysqlColumn) []mysqlColumn { c[0].Extra = "VIRTUAL GENERATED"; return c },
		"extra required": func(c []mysqlColumn) []mysqlColumn {
			return append(c, mysqlColumn{Name: "required", Nullable: "NO"})
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := checkMySQLColumns(table, mutate(slices.Clone(columns))); err == nil {
				t.Fatal("lossy columns accepted")
			}
		})
	}
	for name, mutate := range map[string]func([]mysqlIndex) []mysqlIndex{
		"missing primary": func(i []mysqlIndex) []mysqlIndex { return i[1:] },
		"wrong primary":   func(i []mysqlIndex) []mysqlIndex { i[0].Column = "span_id"; return i },
		"partial primary": func(i []mysqlIndex) []mysqlIndex {
			n := int64(10)
			i[0].SubPart = &n
			return i
		},
		"extra unique": func(i []mysqlIndex) []mysqlIndex {
			return append(i, mysqlIndex{Name: "bad_unique", NonUnique: 0, Sequence: 1, Column: "kind"})
		},
		"missing index": func(i []mysqlIndex) []mysqlIndex { return i[:2] },
	} {
		t.Run(name, func(t *testing.T) {
			if err := checkMySQLIndexes(table, mutate(slices.Clone(indexes))); err == nil {
				t.Fatal("incorrect indexes accepted")
			}
		})
	}
}

func TestProductionDDLMatchesSpecAndPrivateRows(t *testing.T) {
	sql, err := os.ReadFile("../../sql/trace/001_create_tables.mysql.sql")
	if err != nil {
		t.Fatal(err)
	}
	spec, err := os.ReadFile("../../docs/specs/record-sdk-trace.md")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(sql), "CREATE TABLE ") != 21 || len(tables) != 21 {
		t.Fatal("expected one Run table and 20 span shards")
	}
	for index, table := range tables {
		start := strings.Index(string(sql), "CREATE TABLE "+table.name)
		if start < 0 {
			t.Fatalf("missing DDL %s", table.name)
		}
		end := strings.Index(string(sql)[start:], ";")
		statement := string(sql)[start : start+end+1]
		// The Spec shows the Run table and first shard; the executable SQL
		// supplies all 20 identical shard schemas.
		example := statement
		if index > 1 {
			example = strings.ReplaceAll(example, table.name, "tblLubanTraceSpan00")
			example = strings.ReplaceAll(example, fmt.Sprintf("idx_trace_span_time%02d", index-1), "idx_trace_span_time00")
		}
		if !strings.Contains(string(spec), example) {
			t.Fatalf("DDL differs from Spec: %s", table.name)
		}
		var row any = spanRow{}
		if table.name == "tblLubanTraceRun" {
			row = runRow{}
		}
		typ := reflect.TypeOf(row)
		for _, column := range table.columns {
			found := false
			for i := range typ.NumField() {
				tag := typ.Field(i).Tag.Get("gorm")
				if strings.Split(tag, ";")[0] == "column:"+column.name {
					found = true
				}
			}
			if !found || !strings.Contains(statement, column.name) {
				t.Fatalf("missing row/DDL column %s", column.name)
			}
		}
	}
}

func TestMySQLWriteUsesExactDuplicateNoop(t *testing.T) {
	// The MySQL dialector generates production SQL, while a temporary SQLite
	// connection supplies Begin/Commit only. DryRun executes no MySQL SQL.
	sqlDB, _ := testDB(t, false).DB()
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{
		DryRun: true, DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	var statements []string
	if err := db.Callback().Create().After("gorm:create").Register("trace_test_sql", func(tx *gorm.DB) {
		statements = append(statements, tx.Statement.SQL.String())
	}); err != nil {
		t.Fatal(err)
	}
	item, _ := encode(rootEvent(), "test", Metadata{})
	other := rootEvent()
	other.RunID = "run_00000000000000000000000000000000"
	second, _ := encode(other, "test", Metadata{})
	s := &gormStore{db: db, dialect: "mysql"}
	if err := s.write(context.Background(), []entry{second, item}); err != nil {
		t.Fatal(err)
	}
	if len(statements) != 3 {
		t.Fatalf("SQL=%v", statements)
	}
	// A different input order still writes physical shards in stable order,
	// then the unsharded Run projection. No host NamingStrategy is used.
	for i, table := range []string{"tblLubanTraceSpan00", "tblLubanTraceSpan17", "tblLubanTraceRun"} {
		sql := statements[i]
		if !strings.Contains(sql, "INSERT INTO `"+table+"`") {
			t.Fatalf("incorrect physical table: %s", sql)
		}
		if !strings.Contains(sql, "ON DUPLICATE KEY UPDATE `run_id`=`run_id`") ||
			strings.Contains(sql, "INSERT IGNORE") || strings.Contains(sql, "REPLACE INTO") {
			t.Fatalf("wrong duplicate semantics: %s", sql)
		}
	}
}

func TestUnknownCommitRetryIsIdempotent(t *testing.T) {
	db := testDB(t, true)
	s, err := newGormStore(db)
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	w := startWriter(storeFunc(func(ctx context.Context, items []entry) error {
		if err := s.write(ctx, items); err != nil {
			return err
		}
		var err error
		once.Do(func() { err = fmt.Errorf("connection lost after commit") })
		return err
	}), testOptions(t, nil))
	w.WriteTrace(nil, exampleEvent("s_2"))
	w.WriteTrace(nil, rootEvent())
	if err := closeWriter(t, w); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&runRow{}).Count(&count).Error; err != nil || count != 1 ||
		len(readEvents(t, db)) != 2 || w.Stats().WriteFailures != 1 || w.Stats().Persisted != 2 {
		t.Fatalf("unknown-commit retry duplicated/lost events: %v %+v", err, w.Stats())
	}
}
