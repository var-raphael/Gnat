package storage

import (
	"time"

	"gorm.io/gorm"
)

// Site represents a single tracked property (one instance can support
// multiple sites even though v1 only exposes a single-site config path).
type Site struct {
	ID uint `gorm:"primaryKey"`

	Name      string `gorm:"uniqueIndex;not null;size:255"`
	CreatedAt time.Time
}


type Event struct {
	ID     uint `gorm:"primaryKey"`
	SiteID uint `gorm:"index;not null"`
	// The size tags below are all for the same MySQL indexed-TEXT
	// reason as Site.Name — GORM's MySQL dialector defaults a bare
	// `string` to LONGTEXT, which MySQL/InnoDB can't put any index
	// (unique or not) on without an explicit key-length prefix.
	// SQLite and Postgres both allow indexing unbounded text directly,
	// so this only surfaces on MySQL.
	EventName   string    `gorm:"index;not null;size:255"`
	DistinctID  string    `gorm:"index;not null;size:255"`
	Properties  string    `gorm:"type:text"`
	Country     string    `gorm:"index;size:255"`
	VisitorHash string    `gorm:"index;size:255"`
	Browser     string    `gorm:"index;size:255"`
	OS          string    `gorm:"index;size:255"`
	DeviceType  string    `gorm:"index;size:255"` // "desktop", "mobile", "tablet", "bot", "unknown"
	Timestamp   time.Time `gorm:"index;not null"`
	CreatedAt   time.Time
}

// PathSummary stores precomputed multi-branch path analysis results,
// written by a background job rather than computed per-request.
type PathSummary struct {
	ID     uint `gorm:"primaryKey"`
	SiteID uint `gorm:"index;not null"`
	// size:255 for the same MySQL indexed-TEXT reason as Event above.
	AnchorEvent string `gorm:"index;not null;size:255"`
	Path        string `gorm:"type:text;not null"`
	Count       int    `gorm:"not null"`
	ComputedAt  time.Time
}

// Funnel is an operator-defined conversion funnel: an ordered sequence
// of event names to track visitors through. Steps are stored as JSON
// text (list of {event_name, label}) rather than a separate table,
// since they're always read/written as a whole ordered unit, never
// queried individually.
type Funnel struct {
	ID          uint   `gorm:"primaryKey"`
	SiteID      uint   `gorm:"index;not null"`
	Name        string `gorm:"not null"`
	Steps       string `gorm:"type:text;not null"`
	WindowHours int    `gorm:"not null;default:168"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type McpToken struct {
	ID uint `gorm:"primaryKey"`
	// size:64 for the same MySQL indexed-TEXT reason as Site.Name
	// above — and a SHA-256 hex digest is always exactly 64 chars
	// regardless of driver, so this is also just the correct type,
	// not merely a MySQL workaround.
	TokenHash string `gorm:"uniqueIndex;not null;size:64"`
	CreatedAt time.Time
}

// AutoMigrate runs GORM's schema migration for all models.
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(&Site{}, &Event{}, &PathSummary{}, &Funnel{}, &McpToken{})
}
