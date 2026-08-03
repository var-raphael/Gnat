package storage

import (
	"fmt"

	"github.com/glebarez/sqlite"
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
		return nil, fmt.Errorf("postgres driver not wired yet")
	case "mysql":
		return nil, fmt.Errorf("mysql driver not wired yet")
	default:
		return nil, fmt.Errorf("unknown database driver: %s", cfg.Driver)
	}
}
