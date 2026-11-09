package tracegorm

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"

	"gorm.io/gorm"
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

func forbidsDefault(kind string) bool {
	switch kind {
	case "tinytext", "text", "mediumtext", "longtext",
		"tinyblob", "blob", "mediumblob", "longblob",
		"geometry", "json":
		return true
	default:
		return false
	}
}

func ddlColumnType(token string) string {
	switch {
	case strings.HasPrefix(token, "bigint"):
		return "bigint"
	case strings.HasPrefix(token, "varchar"):
		return "varchar"
	case strings.HasPrefix(token, "datetime"):
		return "datetime"
	case strings.HasPrefix(token, "tinyint"):
		return "tinyint"
	case strings.HasPrefix(token, "tinytext"):
		return "tinytext"
	case strings.HasPrefix(token, "text"):
		return "text"
	case strings.HasPrefix(token, "mediumtext"):
		return "mediumtext"
	case strings.HasPrefix(token, "longtext"):
		return "longtext"
	case strings.HasPrefix(token, "tinyblob"):
		return "tinyblob"
	case strings.HasPrefix(token, "blob"):
		return "blob"
	case strings.HasPrefix(token, "mediumblob"):
		return "mediumblob"
	case strings.HasPrefix(token, "longblob"):
		return "longblob"
	case strings.HasPrefix(token, "geometry"):
		return "geometry"
	case strings.HasPrefix(token, "json"):
		return "json"
	default:
		return ""
	}
}

func privateColumnNames(table string) []string {
	var row any = spanRow{}
	if table == runTable {
		row = runRow{}
	}
	typ := reflect.TypeOf(row)
	columns := make([]string, 0, typ.NumField())
	for index := range typ.NumField() {
		tag := typ.Field(index).Tag.Get("gorm")
		columns = append(columns, strings.TrimPrefix(strings.Split(tag, ";")[0], "column:"))
	}
	return columns
}

func TestProductionDDLMatchesSpecAndPrivateRows(t *testing.T) {
	sqlBytes, err := os.ReadFile("../../sql/trace/001_create_tables.mysql.sql")
	if err != nil {
		t.Fatal(err)
	}
	specBytes, err := os.ReadFile("../../docs/specs/record-sdk-trace.md")
	if err != nil {
		t.Fatal(err)
	}
	sql, spec := string(sqlBytes), string(specBytes)
	if got := strings.Count(sql, "CREATE TABLE "); got != 21 {
		t.Fatalf("expected 21 DDL tables, got %d", got)
	}

	tables := make([]string, 0, 21)
	tables = append(tables, runTable)
	for index := range spanShardCount {
		tables = append(tables, spanTableByIndex(index))
	}
	for _, table := range tables {
		start := strings.Index(sql, "CREATE TABLE "+table+" (")
		if start < 0 {
			t.Fatalf("missing DDL %s", table)
		}
		end := strings.Index(sql[start:], ";")
		if end < 0 {
			t.Fatalf("unterminated DDL %s", table)
		}
		statement := sql[start : start+end+1]
		if !strings.Contains(statement, ") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='") {
			t.Fatalf("DDL table comment missing: %s", table)
		}

		example := statement
		if table != runTable && table != "tblLubanTraceSpan0" {
			suffix := strings.TrimPrefix(table, "tblLubanTraceSpan")
			example = strings.ReplaceAll(example, table, "tblLubanTraceSpan0")
			example = strings.ReplaceAll(example, "uk_trace_span_identity"+suffix, "uk_trace_span_identity0")
			example = strings.ReplaceAll(example, "idx_trace_span_time"+suffix, "idx_trace_span_time0")
			example = strings.ReplaceAll(example, "Span分表"+suffix, "Span分表0")
		}
		if !strings.Contains(spec, example) {
			t.Fatalf("DDL differs from Spec: %s", table)
		}

		expectedColumns := privateColumnNames(table)
		actualColumns := make(map[string]string, len(expectedColumns))
		for _, line := range strings.Split(statement, "\n") {
			fields := strings.Fields(strings.TrimSpace(line))
			if len(fields) < 2 {
				continue
			}
			kind := ddlColumnType(fields[1])
			if kind != "" {
				actualColumns[fields[0]] = strings.TrimSpace(line)
			}
		}
		if len(actualColumns) != len(expectedColumns) {
			t.Fatalf("%s columns: DDL=%d row=%d", table, len(actualColumns), len(expectedColumns))
		}
		for _, column := range expectedColumns {
			line, ok := actualColumns[column]
			if !ok {
				t.Fatalf("missing row/DDL column %s.%s", table, column)
			}
			if !strings.Contains(line, " COMMENT '") {
				t.Fatalf("DDL column comment missing: %s.%s", table, column)
			}
			fields := strings.Fields(line)
			kind := ddlColumnType(fields[1])
			hasDefault := strings.Contains(line, " DEFAULT ")
			if forbidsDefault(kind) {
				if strings.Contains(line, " NOT NULL") || hasDefault {
					t.Fatalf("DDL text/blob/json column must be nullable without default: %s.%s", table, column)
				}
			} else if column != "id" && !hasDefault {
				t.Fatalf("DDL column default missing: %s.%s", table, column)
			}
		}
	}
}
