package models

import (
	"fmt"
	"io"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// DatabaseConfig describes an optional business database connection.
// Keeping connections here also leaves room for a future Luban-owned database.
type DatabaseConfig struct {
	Driver string `yaml:"driver"`
	DSN    string `yaml:"dsn"`
}

// OpenDatabase creates a connection owned by the service. A host-injected
// *gorm.DB is managed by the host and does not pass through this function.
func OpenDatabase(name string, config DatabaseConfig) (*gorm.DB, io.Closer, error) {
	if config.Driver != "mysql" || config.DSN == "" {
		return nil, nil, fmt.Errorf("database %q requires driver mysql and dsn", name)
	}
	db, err := gorm.Open(mysql.Open(config.DSN), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent), DisableAutomaticPing: true,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("database %q connection failed", name)
	}
	raw, err := db.DB()
	if err != nil {
		return nil, nil, fmt.Errorf("database %q connection failed", name)
	}
	if err := raw.Ping(); err != nil {
		_ = raw.Close()
		return nil, nil, fmt.Errorf("database %q ping failed", name)
	}
	return db, raw, nil
}
