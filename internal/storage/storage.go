package storage

import (
	"fmt"
	"net/url"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/var-raphael/gnat/internal/config"
)

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
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(cfg.User, cfg.Password),
		Host:   fmt.Sprintf("%s:%d", cfg.Host, port),
		Path:   "/" + cfg.DBName,
	}
	q := u.Query()
	q.Set("sslmode", sslmode)
	u.RawQuery = q.Encode()
	return u.String()
}

func mysqlDSN(cfg config.DatabaseConfig) string {
	port := cfg.Port
	if port == 0 {
		port = 3306
	}

	return fmt.Sprintf(
		"%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		cfg.User, cfg.Password, cfg.Host, port, cfg.DBName,
	)
}
