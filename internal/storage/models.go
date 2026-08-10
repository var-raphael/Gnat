package storage

import (
	"time"

	"gorm.io/gorm"
)

// Site represents a single tracked property (one instance can support
// multiple sites even though v1 only exposes a single-site config path).
type Site struct {
	ID uint `gorm:"primaryKey"`
	// size:255 gives GORM's MySQL dialector a bounded VARCHAR instead
	// of its no-length default of LONGTEXT — MySQL/InnoDB can't put a
	// unique index on a TEXT/BLOB column without an explicit index
	// prefix length, so a bare `string` here fails AutoMigrate on
	// MySQL specifically (SQLite and Postgres both allow indexing an
	// unbounded text column directly, so this never surfaced there).
	Name      string `gorm:"uniqueIndex;not null;size:255"`
	CreatedAt time.Time
}

// Event is a single raw ingested event, either an automatic pageview
// or a custom event fired via track().
//
// Country is populated asynchronously after the initial write, via a
// background geo lookup keyed off the request IP. The IP itself is never
// stored, only held in memory for the duration of that one lookup.
//
// VisitorHash is a daily-salted, one-way hash of the request IP, used as
// a secondary signal for unique-visitor counts that is resistant to
// someone simply clearing localStorage to inflate numbers. It rotates
// daily, so it cannot be used to correlate the same visitor across days.
//
// Browser, OS, and DeviceType are parsed server-side from the request's
// User-Agent header at ingestion time. No client-side JS is needed for
// this, the header is already present on every HTTP request.
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

// McpToken is the single active credential that authorizes MCP server
// requests, kept entirely separate from DashboardPassword (env-config,
// static) and dashboard sessions (in-memory, expire on their own).
// Unlike a session, this has to survive a server restart — regenerating
// it, then restarting, must not silently revert to a stale token — so
// it lives in the DB rather than in memory.
//
// TokenHash stores a SHA-256 hash, never the raw token: this table is
// part of an on-disk file that could end up in a backup, a copy, or
// exposed by some unrelated bug, and a hash leaked that way is useless
// to an attacker whereas a plaintext token would grant live access. The
// plaintext value is generated, returned to the caller exactly once,
// and never persisted or logged anywhere. There is intentionally at
// most one row: regenerating replaces it rather than appending, so a
// compromised token can be fully invalidated by generating a new one.
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
