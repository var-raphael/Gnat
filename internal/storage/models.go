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

// Event is a single raw ingested event — either an automatic pageview
// or a custom event fired via track().
type Event struct {
	ID         uint   `gorm:"primaryKey"`
	SiteID     uint   `gorm:"index;not null"`
	EventName  string `gorm:"index;not null"` // e.g. "pageview", "signup_completed"
	DistinctID string `gorm:"index;not null"` // anonymous visitor identifier
	Properties string `gorm:"type:text"`       // JSON-encoded arbitrary props
	Timestamp  time.Time `gorm:"index;not null"`
	CreatedAt  time.Time
}

// PathSummary stores precomputed multi-branch path analysis results,
// written by a background job rather than computed per-request.
type PathSummary struct {
	ID          uint   `gorm:"primaryKey"`
	SiteID      uint   `gorm:"index;not null"`
	AnchorEvent string `gorm:"index;not null"`
	Path        string `gorm:"type:text;not null"` // normalized, JSON-encoded step list
	Count       int    `gorm:"not null"`
	ComputedAt  time.Time
}

// AutoMigrate runs GORM's schema migration for all models. Called once
// at startup after the DB connection is established.
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(&Site{}, &Event{}, &PathSummary{})
}
