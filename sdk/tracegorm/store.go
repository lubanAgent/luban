package tracegorm

import (
	"context"
	"fmt"
	"reflect"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

type store interface {
	write(context.Context, []entry) error
}

type gormStore struct {
	db      *gorm.DB
	dialect string
}

func newGormStore(db *gorm.DB) (*gormStore, error) {
	if db == nil || db.Config == nil || db.Statement == nil || nilValue(db.Dialector) ||
		nilValue(db.Statement.ConnPool) || nilValue(db.Config.ConnPool) {
		return nil, fmt.Errorf("tracegorm: a valid base Gorm connection is required")
	}
	if db.Error != nil {
		return nil, fmt.Errorf("tracegorm: database handle: %w", db.Error)
	}
	if db.DryRun {
		return nil, fmt.Errorf("tracegorm: DryRun connections cannot persist Trace")
	}
	if _, ok := db.Statement.ConnPool.(gorm.TxCommitter); ok {
		return nil, fmt.Errorf("tracegorm: inject a base connection, not a transaction")
	}
	if _, ok := db.Config.ConnPool.(gorm.TxCommitter); ok {
		return nil, fmt.Errorf("tracegorm: inject a base connection, not a transaction")
	}
	dialect := db.Dialector.Name()
	if dialect != "mysql" && dialect != "sqlite" {
		return nil, fmt.Errorf("tracegorm: unsupported driver %q", dialect)
	}
	// NewDB clears host query clauses. Silent SQL logging prevents failed INSERTs
	// from exposing entire prompts; OnError reports a bounded diagnostic instead.
	// SkipHooks applies only to these private rows, not to the host's DB handle.
	session := db.Session(&gorm.Session{
		NewDB: true, Initialized: true, Context: context.Background(), SkipHooks: true,
		Logger: logger.Default.LogMode(logger.Silent),
	})
	return &gormStore{db: session, dialect: dialect}, nil
}

func nilValue(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func:
		return rv.IsNil()
	default:
		return false
	}
}

func (s *gormStore) write(ctx context.Context, batch []entry) error {
	var spans [spanShardCount][]spanRow
	runs := make([]runRow, 0, len(batch))
	for _, item := range batch {
		shard := spanShard(item.span.RunID)
		spans[shard] = append(spans[shard], item.span)
		if item.run != nil {
			runs = append(runs, *item.run)
		}
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// MySQL generates a primary-key self-assignment on duplicate, not
		// INSERT IGNORE. SQLite targets the exact identity columns.
		// Each batch may span multiple tables. Keep all shard writes and Run
		// summaries in one transaction, in deterministic shard order.
		for index, rows := range spans {
			if len(rows) == 0 {
				continue
			}
			if err := tx.Table(spanTableByIndex(index)).Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "run_id"}, {Name: "span_id"}}, DoNothing: true,
			}).Create(&rows).Error; err != nil {
				return err
			}
		}
		if len(runs) != 0 {
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "run_id"}}, DoNothing: true,
			}).Create(&runs).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
