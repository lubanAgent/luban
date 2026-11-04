package tracegorm

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// This in-process SQL driver tests the actual MySQL metadata query/scanning
// path without credentials or a real MySQL server.
type metadataConnector struct {
	engine string
	fail   error
}

func (c metadataConnector) Connect(context.Context) (driver.Conn, error) {
	return &metadataConn{config: c}, nil
}
func (c metadataConnector) Driver() driver.Driver { return metadataDriver{config: c} }

type metadataDriver struct{ config metadataConnector }

func (d metadataDriver) Open(string) (driver.Conn, error) {
	return &metadataConn{config: d.config}, nil
}

type metadataConn struct{ config metadataConnector }

func (*metadataConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepared statement")
}
func (*metadataConn) Close() error { return nil }
func (*metadataConn) Begin() (driver.Tx, error) {
	return nil, errors.New("schema checks must not begin transactions")
}
func (c *metadataConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.config.fail != nil {
		return nil, c.config.fail
	}
	if !strings.HasPrefix(strings.TrimSpace(query), "SELECT") || len(args) != 1 {
		return nil, fmt.Errorf("unexpected schema query: %s", query)
	}
	var table tableSpec
	for _, candidate := range tables {
		if candidate.name == args[0].Value {
			table = candidate
		}
	}
	if table.name == "" {
		return nil, fmt.Errorf("unexpected metadata target: %v", args)
	}
	columns, indexes := mysqlMetadata(table)
	switch {
	case strings.Contains(query, "information_schema.TABLES"):
		return &metadataRows{columns: []string{"engine"}, rows: [][]driver.Value{{c.config.engine}}}, nil
	case strings.Contains(query, "information_schema.COLUMNS"):
		rows := &metadataRows{columns: []string{"column_name", "data_type", "column_type", "is_nullable",
			"character_maximum_length", "datetime_precision", "character_set_name", "collation_name", "column_default", "extra"}}
		for _, c := range columns {
			var length, precision, charset, collation driver.Value
			if c.Length != nil {
				length = *c.Length
			}
			if c.Precision != nil {
				precision = *c.Precision
			}
			if c.Charset != nil {
				charset = *c.Charset
			}
			if c.Collation != nil {
				collation = *c.Collation
			}
			rows.rows = append(rows.rows, []driver.Value{c.Name, c.Kind, c.Type, c.Nullable, length, precision, charset, collation, nil, c.Extra})
		}
		return rows, nil
	case strings.Contains(query, "information_schema.STATISTICS"):
		rows := &metadataRows{columns: []string{"index_name", "non_unique", "seq_in_index", "column_name", "sub_part"}}
		for _, i := range indexes {
			rows.rows = append(rows.rows, []driver.Value{i.Name, int64(i.NonUnique), int64(i.Sequence), i.Column, nil})
		}
		return rows, nil
	default:
		return nil, fmt.Errorf("unexpected metadata query: %s", query)
	}
}

type metadataRows struct {
	columns []string
	rows    [][]driver.Value
}

func (r *metadataRows) Columns() []string { return r.columns }
func (*metadataRows) Close() error        { return nil }
func (r *metadataRows) Next(dest []driver.Value) error {
	if len(r.rows) == 0 {
		return io.EOF
	}
	copy(dest, r.rows[0])
	r.rows = r.rows[1:]
	return nil
}

func TestMySQLReadOnlySchemaQueryPath(t *testing.T) {
	for _, tc := range []struct {
		name    string
		config  metadataConnector
		wantErr bool
	}{
		{"valid", metadataConnector{engine: "InnoDB"}, false},
		{"nontransactional", metadataConnector{engine: "MyISAM"}, true},
		{"no metadata permission", metadataConnector{fail: errors.New("metadata denied")}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			connection := sql.OpenDB(tc.config)
			defer connection.Close()
			db, err := gorm.Open(mysql.New(mysql.Config{Conn: connection, SkipInitializeWithVersion: true}), &gorm.Config{
				DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent),
			})
			if err != nil {
				t.Fatal(err)
			}
			w, err := New(context.Background(), db, Options{Service: "test"})
			if (err != nil) != tc.wantErr {
				t.Fatalf("New result: %v", err)
			}
			if w != nil {
				if err := closeWriter(t, w); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
