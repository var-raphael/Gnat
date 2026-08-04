package query

import (
	"encoding/json"
	"net/http"
	"time"

	"gorm.io/gorm"
)

// pageviewPoint is one bucket in the pageviews-over-time series.
type pageviewPoint struct {
	Date  string `json:"date"`
	Count int64  `json:"count"`
}

// PageviewsHandler returns GET /api/stats/pageviews?from=...&to=...
// Both params are optional ISO 8601 dates; defaults to the last 7 days.
// Locked down by API key regardless of CORS, since this endpoint returns
// real data, not just accepts it.
func PageviewsHandler(db *gorm.DB, apiKey string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if !authorized(r, apiKey) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		from, to, err := parseRange(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		var results []pageviewPoint

		// Group by calendar day. strftime is SQLite syntax; this will need
		// a dialect switch (to date_trunc for postgres, DATE() for mysql)
		// once those backends are exercised here, GORM doesn't abstract
		// date functions for us.
		err = db.Table("events").
			Select("strftime('%Y-%m-%d', timestamp) as date, count(*) as count").
			Where("event_name = ? AND timestamp BETWEEN ? AND ?", "pageview", from, to).
			Group("date").
			Order("date").
			Scan(&results).Error

		if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(results)
	}
}

// parseRange reads from/to query params, defaulting to the last 7 days
// if either is missing.
func parseRange(r *http.Request) (time.Time, time.Time, error) {
	now := time.Now().UTC()
	from := now.AddDate(0, 0, -7)
	to := now

	if v := r.URL.Query().Get("from"); v != "" {
		parsed, err := time.Parse("2006-01-02", v)
		if err != nil {
			return from, to, errBadDate("from")
		}
		from = parsed
	}

	if v := r.URL.Query().Get("to"); v != "" {
		parsed, err := time.Parse("2006-01-02", v)
		if err != nil {
			return from, to, errBadDate("to")
		}
		to = parsed
	}

	return from, to, nil
}

func errBadDate(field string) error {
	return &badDateError{field}
}

type badDateError struct {
	field string
}

func (e *badDateError) Error() string {
	return "invalid " + e.field + " date, expected format YYYY-MM-DD"
}

// authorized mirrors the ingest package's check. Query endpoints use the
// same key scheme for now; splitting into a separate read-only admin key
// is a later hardening step.
func authorized(r *http.Request, apiKey string) bool {
	if key := r.Header.Get("X-API-Key"); key != "" {
		return key == apiKey
	}
	auth := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(auth) > len(prefix) && auth[:len(prefix)] == prefix {
		return auth[len(prefix):] == apiKey
	}
	return false
}
