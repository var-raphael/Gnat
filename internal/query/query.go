package query

import (
	"encoding/json"
	"net/http"
	"time"

	"gorm.io/gorm"

	"github.com/var-raphael/gnat/internal/dialect"
)

// pageviewPoint is one bucket in the pageviews-over-time series.
type pageviewPoint struct {
	Date  string `json:"date"`
	Count int64  `json:"count"`
}

// PageviewsHandler returns GET /api/stats/pageviews?from=...&to=...
// Both params are optional ISO 8601 dates; defaults to the last 7 days.
// Auth is handled by the caller: main.go wraps this in
// auth.DashboardAuth.RequireSession, so by the time this handler runs
// the request has already been verified.
func PageviewsHandler(db *gorm.DB) http.HandlerFunc {
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

		var results []pageviewPoint

		// Grouped by calendar day via dialect.DateTrunc, the one seam
		// isolated per internal/dialect's package doc — everything else
		// in this query is portable GORM as-is.
		dateExpr := dialect.DateTrunc(db.Dialector.Name(), "timestamp")
		err = db.Table("events").
			Select(dateExpr + " as date, count(*) as count").
			Where("event_name = ? AND timestamp BETWEEN ? AND ?", "pageview", from, to).
			Group("date").
			Order("date").
			Scan(&results).Error

		if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}

		byDate := make(map[string]int64, len(results))
		for _, r := range results {
			byDate[r.Date] = r.Count
		}

		filled := make([]pageviewPoint, 0)
		for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
			key := d.Format("2006-01-02")
			filled = append(filled, pageviewPoint{Date: key, Count: byDate[key]})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(filled)
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
		to = parsed.Add(24*time.Hour - time.Nanosecond)
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

// countRow is one raw group-by-count result before percentages are
// added.
type countRow struct {
	Value string
	Count int64
}

// groupByCount runs a pageview count grouped by column, then adds a pct
// of total to each row. Shared by countries/devices/browsers, which are
// otherwise identical queries against different columns.
func groupByCount(db *gorm.DB, column string, from, to time.Time) ([]countRow, error) {
	var rows []countRow
	err := db.Table("events").
		Select(column + " as value, count(*) as count").
		Where("event_name = ? AND timestamp BETWEEN ? AND ?", "pageview", from, to).
		Group(column).
		Order("count DESC").
		Scan(&rows).Error
	return rows, err
}

func pctOfTotal(count int64, total int64) float64 {
	if total == 0 {
		return 0
	}
	return roundTo2(float64(count) / float64(total) * 100)
}

