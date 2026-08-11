package query

import (
	"encoding/json"
	"net/http"
	"sort"
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

	// Raw referrers are full URLs (document.referrer straight from the
	// client, e.g. "https://www.facebook.com/"). Normalize each to a
	// bare host before aggregating — otherwise the same real-world
	// source shows up as many distinct rows (different paths/schemes
	// on the same domain), and internal navigation (e.g.
	// "https://yoursite.com/home") never matches up against anything
	// to get excluded as direct traffic.
	counts := make(map[string]int64)
	order := make([]string, 0, len(raw))
	for _, row := range raw {
		host := referrerHost(row.Referrer)
		if host == "" {
			continue
		}
		if _, seen := counts[host]; !seen {
			order = append(order, host)
		}
		counts[host] += row.Count
	}

	results := make([]ReferrerPoint, 0, len(order))
	for _, host := range order {
		results = append(results, ReferrerPoint{
			Referrer: host,
			Count:    counts[host],
			Category: "referral",
		})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Count > results[j].Count })
	return results, nil
}
