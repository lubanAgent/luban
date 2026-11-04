package tracegorm

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

type columnSpec struct {
	name    string
	kind    string
	length  int64
	charset string
}

type tableSpec struct {
	name    string
	columns []columnSpec
	primary []string
	indexes map[string][]string
}

var tables = traceTables()

func traceTables() []tableSpec {
	run := tableSpec{
		name: runTable,
		columns: []columnSpec{
			{"run_id", "varchar", 36, "ascii"},
			{"service", "varchar", 64, "utf8mb4"},
			{"agent_key", "varchar", 191, "utf8mb4"},
			{"business_type", "varchar", 64, "utf8mb4"},
			{"business_id", "varchar", 191, "utf8mb4"},
			{"request_id", "varchar", 191, "utf8mb4"},
			{"status", "varchar", 16, "ascii"},
			{"started_at", "datetime", 0, ""},
			{"duration_ns", "bigint", 0, ""},
			{"error_msg", "longtext", 0, "utf8mb4"},
			{"create_time", "datetime", 0, ""},
		},
		primary: []string{"run_id"},
		indexes: map[string][]string{
			"idx_trace_run_agent_time": {"service", "agent_key", "started_at", "run_id"},
			"idx_trace_run_business":   {"service", "business_type", "business_id", "started_at"},
			"idx_trace_run_time":       {"started_at", "run_id"},
		},
	}
	span := tableSpec{
		columns: []columnSpec{
			{"run_id", "varchar", 36, "ascii"},
			{"span_id", "varchar", 32, "ascii"},
			{"parent_span_id", "varchar", 32, "ascii"},
			{"kind", "varchar", 32, "ascii"},
			{"status", "varchar", 16, "ascii"},
			{"started_at", "datetime", 0, ""},
			{"duration_ns", "bigint", 0, ""},
			{"event_json", "longtext", 0, "utf8mb4"},
			{"create_time", "datetime", 0, ""},
		},
		primary: []string{"run_id", "span_id"},
	}
	result := []tableSpec{run}
	for index := range spanShardCount {
		shard := span
		shard.name = spanTableByIndex(index)
		shard.indexes = map[string][]string{
			fmt.Sprintf("idx_trace_span_time%02d", index): {"started_at", "run_id", "span_id"},
		}
		result = append(result, shard)
	}
	return result
}

func (s *gormStore) check(ctx context.Context) error {
	for _, table := range tables {
		var err error
		if s.dialect == "mysql" {
			err = s.checkMySQL(ctx, table)
		} else {
			err = s.checkSQLite(ctx, table)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", table.name, err)
		}
	}
	return nil
}

type mysqlColumn struct {
	Name      string  `gorm:"column:column_name"`
	Kind      string  `gorm:"column:data_type"`
	Type      string  `gorm:"column:column_type"`
	Nullable  string  `gorm:"column:is_nullable"`
	Length    *int64  `gorm:"column:character_maximum_length"`
	Precision *int64  `gorm:"column:datetime_precision"`
	Charset   *string `gorm:"column:character_set_name"`
	Collation *string `gorm:"column:collation_name"`
	Default   *string `gorm:"column:column_default"`
	Extra     string  `gorm:"column:extra"`
}

type mysqlIndex struct {
	Name      string `gorm:"column:index_name"`
	NonUnique int    `gorm:"column:non_unique"`
	Sequence  int    `gorm:"column:seq_in_index"`
	Column    string `gorm:"column:column_name"`
	SubPart   *int64 `gorm:"column:sub_part"`
}

func (s *gormStore) checkMySQL(ctx context.Context, table tableSpec) error {
	db := s.db.WithContext(ctx)
	var tableInfo []struct{ Engine string }
	if err := db.Raw(`SELECT ENGINE AS engine FROM information_schema.TABLES
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?`, table.name).Scan(&tableInfo).Error; err != nil {
		return err
	}
	if len(tableInfo) != 1 || !strings.EqualFold(tableInfo[0].Engine, "InnoDB") {
		return fmt.Errorf("existing transactional InnoDB table required")
	}
	var columns []mysqlColumn
	if err := db.Raw(`SELECT COLUMN_NAME AS column_name, DATA_TYPE AS data_type,
		COLUMN_TYPE AS column_type, IS_NULLABLE AS is_nullable,
		CHARACTER_MAXIMUM_LENGTH AS character_maximum_length, DATETIME_PRECISION AS datetime_precision,
		CHARACTER_SET_NAME AS character_set_name, COLLATION_NAME AS collation_name,
		COLUMN_DEFAULT AS column_default, EXTRA AS extra FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?`, table.name).Scan(&columns).Error; err != nil {
		return err
	}
	if err := checkMySQLColumns(table, columns); err != nil {
		return err
	}
	var indexes []mysqlIndex
	if err := db.Raw(`SELECT INDEX_NAME AS index_name, NON_UNIQUE AS non_unique,
		SEQ_IN_INDEX AS seq_in_index, COLUMN_NAME AS column_name, SUB_PART AS sub_part
		FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = DATABASE()
		AND TABLE_NAME = ? ORDER BY INDEX_NAME, SEQ_IN_INDEX`, table.name).Scan(&indexes).Error; err != nil {
		return err
	}
	return checkMySQLIndexes(table, indexes)
}

func checkMySQLColumns(table tableSpec, columns []mysqlColumn) error {
	actual := make(map[string]mysqlColumn, len(columns))
	for _, col := range columns {
		actual[col.Name] = col
	}
	for _, want := range table.columns {
		got, ok := actual[want.name]
		if !ok || strings.ToLower(got.Kind) != want.kind || got.Nullable != "NO" ||
			strings.Contains(strings.ToLower(got.Type), "unsigned") {
			return fmt.Errorf("column %s must be non-null %s", want.name, want.kind)
		}
		if want.length != 0 && (got.Length == nil || *got.Length != want.length) {
			return fmt.Errorf("column %s length must be %d", want.name, want.length)
		}
		if want.kind == "datetime" && (got.Precision == nil || *got.Precision != 6) {
			return fmt.Errorf("column %s requires microsecond precision", want.name)
		}
		if want.charset != "" && (got.Charset == nil || *got.Charset != want.charset ||
			got.Collation == nil || *got.Collation != want.charset+"_bin") {
			return fmt.Errorf("column %s requires %s_bin collation", want.name, want.charset)
		}
		if got.Extra != "" {
			return fmt.Errorf("column %s must not be generated or auto-updated", want.name)
		}
		delete(actual, want.name)
	}
	for name, extra := range actual {
		if extra.Nullable != "YES" && extra.Default == nil && !strings.Contains(extra.Extra, "GENERATED") {
			return fmt.Errorf("extra required column %s has no default", name)
		}
	}
	return nil
}

func checkMySQLIndexes(table tableSpec, indexes []mysqlIndex) error {
	columns := make(map[string][]string)
	for _, index := range indexes {
		if index.Name != "PRIMARY" && index.NonUnique == 0 {
			// Additional unique keys could silently swallow unrelated events
			// under MySQL's ON DUPLICATE KEY UPDATE semantics.
			return fmt.Errorf("unexpected unique index %s", index.Name)
		}
		_, required := table.indexes[index.Name]
		if index.Name == "PRIMARY" || required {
			if index.SubPart != nil || index.Sequence != len(columns[index.Name])+1 ||
				(index.Name == "PRIMARY" && index.NonUnique != 0) {
				return fmt.Errorf("invalid index %s", index.Name)
			}
			columns[index.Name] = append(columns[index.Name], index.Column)
		}
	}
	if !slices.Equal(columns["PRIMARY"], table.primary) {
		return fmt.Errorf("primary key must be %v", table.primary)
	}
	for name, want := range table.indexes {
		if !slices.Equal(columns[name], want) {
			return fmt.Errorf("index %s must be %v", name, want)
		}
	}
	return nil
}

type sqliteColumn struct {
	Name       string
	Type       string
	NotNull    int     `gorm:"column:notnull"`
	Default    *string `gorm:"column:dflt_value"`
	PrimaryKey int     `gorm:"column:pk"`
}

type sqliteIndex struct {
	Name    string
	Unique  int
	Origin  string
	Partial int
}

func (s *gormStore) checkSQLite(ctx context.Context, table tableSpec) error {
	// Names are framework constants, never caller-controlled SQL.
	db := s.db.WithContext(ctx)
	var columns []sqliteColumn
	if err := db.Raw(`PRAGMA table_info("` + table.name + `")`).Scan(&columns).Error; err != nil {
		return err
	}
	actual := make(map[string]sqliteColumn, len(columns))
	for _, col := range columns {
		actual[col.Name] = col
	}
	for _, want := range table.columns {
		got, ok := actual[want.name]
		kind := strings.ToLower(got.Type)
		validKind := kind == "text" || strings.HasPrefix(kind, "varchar")
		switch want.kind {
		case "datetime":
			validKind = kind == "datetime" || kind == "datetime(6)"
		case "bigint":
			validKind = kind == "integer" || kind == "bigint"
		}
		position := slices.Index(table.primary, want.name) + 1
		if !ok || !validKind || got.NotNull != 1 || got.PrimaryKey != position {
			return fmt.Errorf("column %s has incompatible type/nullability/primary key", want.name)
		}
		delete(actual, want.name)
	}
	for name, col := range actual {
		if col.PrimaryKey != 0 || (col.NotNull != 0 && col.Default == nil) {
			return fmt.Errorf("extra required column %s", name)
		}
	}
	var indexes []sqliteIndex
	if err := db.Raw(`PRAGMA index_list("` + table.name + `")`).Scan(&indexes).Error; err != nil {
		return err
	}
	seen := make(map[string]bool)
	for _, index := range indexes {
		if index.Unique != 0 && index.Origin != "pk" {
			return fmt.Errorf("unexpected unique index %s", index.Name)
		}
		want, required := table.indexes[index.Name]
		if !required {
			continue
		}
		var parts []struct {
			Sequence int `gorm:"column:seqno"`
			Name     string
		}
		// Only the expected, constant index names are interpolated here.
		if err := db.Raw(`PRAGMA index_info("` + index.Name + `")`).Scan(&parts).Error; err != nil {
			return err
		}
		if index.Partial != 0 || len(parts) != len(want) {
			return fmt.Errorf("index %s is partial or has wrong columns", index.Name)
		}
		for i, part := range parts {
			if part.Sequence != i || part.Name != want[i] {
				return fmt.Errorf("index %s must be %v", index.Name, want)
			}
		}
		seen[index.Name] = true
	}
	for name := range table.indexes {
		if !seen[name] {
			return fmt.Errorf("missing index %s", name)
		}
	}
	return nil
}
