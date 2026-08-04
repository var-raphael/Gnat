package storage

import (
	"fmt"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/var-raphael/gnat/internal/config"
)

// Open connects to the configured database backend and returns a ready
// *gorm.DB. Only one backend is active per instance, selected via config.
func Open(cfg config.DatabaseConfig) (*gorm.DB, error) {
	switch cfg.Driver {
	case "sqlite":
		return gorm.Open(sqlite.Open(cfg.Path), &gorm.Config{})

	case "postgres":
		dsn := postgresDSN(cfg)
		return gorm.Open(postgres.Open(dsn), &gorm.Config{})

	case "mysql":
		dsn := mysqlDSN(cfg)
		return gorm.Open(mysql.Open(dsn), &gorm.Config{})

	default:
		return nil, fmt.Errorf("unknown database driver: %s", cfg.Driver)
	}
}

func postgresDSN(cfg config.DatabaseConfig) string {
	port := cfg.Port
	if port == 0 {
		port = 5432
	}
	sslmode := cfg.SSLMode
	if sslmode == "" {
		sslmode = "disable"
	}
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Host, port, cfg.User, cfg.Password, cfg.DBName, sslmode,
	)
}

func mysqlDSN(cfg config.DatabaseConfig) string {
	port := cfg.Port
	if port == 0 {
		port = 3306
	}
	// parseTime=true so GORM can scan MySQL DATETIME columns into
	// Go time.Time directly, needed for Event.Timestamp and CreatedAt.
	return fmt.Sprintf(
		"%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		cfg.User, cfg.Password, cfg.Host, port, cfg.DBName,
	)
}
