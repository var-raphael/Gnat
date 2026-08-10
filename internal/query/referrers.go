package query

import (
	"encoding/json"
	"net/http"
	"time"

	"gorm.io/gorm"

	"github.com/var-raphael/gnat/internal/dialect"
)

// ReferrerPoint is one referring domain's pageview count for a range.
type ReferrerPoint struct {
	Referrer string `json:"referrer"`
	Count    int64  `json:"count"`
	Category string `json:"category"`
}

// ReferrersHandler returns GET /api/stats/referrers?from=...&to=...
// Excludes direct (blank referrer) — that's the donut's job, not this list.
func ReferrersHandler(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		from, to, err := parseRange(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		results, err := GetTopReferrers(db, from, to)
		if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(results)
	}
}

// GetTopReferrers returns referring domains ordered by pageview count
// (descending) for the given range, excluding direct (blank referrer).
func GetTopReferrers(db *gorm.DB, from, to time.Time) ([]ReferrerPoint, error) {
	referrerExpr := dialect.JSONExtract(db.Dialector.Name(), "properties", "referrer")

	var raw []struct {
		Referrer string
		Count    int64
	}
	// COALESCE(..., '') matters here beyond just "tidiness": the
	// tracker sends JSON null (not "") for direct traffic (see
	// tracker.js), and json_extract/JSON_EXTRACT both pass a JSON null
	// straight through as SQL NULL. Scanning SQL NULL into Raw.Referrer
	// — a plain, non-pointer string — is a hard error in Go's
	// database/sql on every driver (not a MySQL-specific quirk), which
	// would fail the whole query before the row.Referrer == "" filter
	// below ever got a chance to run. Coalescing to '' in SQL sidesteps
	// that entirely and keeps the existing blank-filtering logic
	// working as originally intended.
	err := db.Table("events").
		Select("COALESCE("+referrerExpr+", '') as referrer, count(*) as count").
		Where("event_name = ? AND timestamp BETWEEN ? AND ?", "pageview", from, to).
		Group("referrer").
		Order("count DESC").
		Scan(&raw).Error
	if err != nil {
		return nil, err
	}

	results := make([]ReferrerPoint, 0, len(raw))
	for _, row := range raw {
		if row.Referrer == "" {
			continue
		}
		results = append(results, ReferrerPoint{
			Referrer: row.Referrer,
			Count:    row.Count,
			Category: "referral",
		})
	}
	return results, nil
}
