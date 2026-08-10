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

// postgresDSN builds a URL-style connection string (postgres://...)
// rather than the traditional space-separated "key=value key=value"
// form. Both are valid libpq syntax on paper, but the pgx-based driver
// GORM's postgres package uses (see gorm.io/driver/postgres) has been
// observed — on some platforms/builds, this one included — to silently
// drop the dbname key from the space-separated form during parsing,
// while every other key is picked up fine. The failure is silent: no
// parse error, just a DSN that connects with dbname empty, which
// libpq/the server then defaults to the connecting user's name. That
// produces the exact confusing symptom of "FATAL: database <username>
// does not exist" even though dbname was clearly present and correct
// in the original string. The URL form does not exhibit this; every
// field is delimited unambiguously by the URL syntax itself, so there
// is no key/value pair for the parser to lose track of.
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
	// parseTime=true so GORM can scan MySQL DATETIME columns into
	// Go time.Time directly, needed for Event.Timestamp and CreatedAt.
	return fmt.Sprintf(
		"%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		cfg.User, cfg.Password, cfg.Host, port, cfg.DBName,
	)
}
