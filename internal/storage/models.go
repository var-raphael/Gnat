package storage

import (
	"time"

	"gorm.io/gorm"
)

// Site represents a single tracked property (one instance can support
// multiple sites even though v1 only exposes a single-site config path).
type Site struct {
	ID        uint   `gorm:"primaryKey"`
	Name      string `gorm:"uniqueIndex;not null"`
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
	ID          uint      `gorm:"primaryKey"`
	SiteID      uint      `gorm:"index;not null"`
	EventName   string    `gorm:"index;not null"`
	DistinctID  string    `gorm:"index;not null"`
	Properties  string    `gorm:"type:text"`
	Country     string    `gorm:"index"`
	VisitorHash string    `gorm:"index"`
	Browser     string    `gorm:"index"`
	OS          string    `gorm:"index"`
	DeviceType  string    `gorm:"index"` // "desktop", "mobile", "tablet", "bot", "unknown"
	Timestamp   time.Time `gorm:"index;not null"`
	CreatedAt   time.Time
}

// PathSummary stores precomputed multi-branch path analysis results,
// written by a background job rather than computed per-request.
type PathSummary struct {
	ID          uint   `gorm:"primaryKey"`
	SiteID      uint   `gorm:"index;not null"`
	AnchorEvent string `gorm:"index;not null"`
	Path        string `gorm:"type:text;not null"`
	Count       int    `gorm:"not null"`
	ComputedAt  time.Time
}

// AutoMigrate runs GORM's schema migration for all models.
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(&Site{}, &Event{}, &PathSummary{})
}
