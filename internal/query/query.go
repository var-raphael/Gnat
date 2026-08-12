package query

import (
	"encoding/json"
	"net/http"
	"time"

	"gorm.io/gorm"

	"github.com/var-raphael/gnat/internal/dialect"
)

// PageviewPoint is one bucket in the visitors-over-time series
// (despite the type name — kept for backwards API compatibility with
// the JSON field names existing consumers already rely on). Count is
// unique visitors per bucket, not raw pageviews.
type PageviewPoint struct {
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

		filled, err := GetPageviewsOverTime(db, from, to)
		if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(filled)
	}
}

// GetPageviewsOverTime returns unique-visitor counts between from and
// to (inclusive), with zero-count buckets filled in rather than
// omitted. Named around "pageviews" for historical/API-compat
// reasons, but counts distinct visitors — the frontend chart is
// titled "Visitors Over Time".
//
// When from and to fall on the same calendar day, a daily bucketing
// would produce a single point — not a useful graph — so it buckets
// by hour instead (24 points labeled "00:00".."23:00" for that day).
// Multi-day ranges keep the normal daily bucketing.
func GetPageviewsOverTime(db *gorm.DB, from, to time.Time) ([]PageviewPoint, error) {
	if isSameCalendarDay(from, to) {
		return getPageviewsByHour(db, from, to)
	}
	return getPageviewsByDay(db, from, to)
}

func isSameCalendarDay(from, to time.Time) bool {
	return from.Format("2006-01-02") == to.Format("2006-01-02")
}

func getPageviewsByDay(db *gorm.DB, from, to time.Time) ([]PageviewPoint, error) {
	// Each visitor counts once, in the bucket of their first pageview
	// within the range — not once per bucket they happened to be
	// active in. A visitor active on both Monday and Wednesday should
	// only add to Monday's bar, or the bars wouldn't sum to the
	// range's true unique-visitor total (see GetPageviewsOverTime).
	//
	// dialect.DateTrunc is the seam that keeps this portable — the
	// rest is plain GORM/SQL.
	dateExpr := dialect.DateTrunc(db.Dialector.Name(), "timestamp")
	var raw []struct {
		DistinctID string
		FirstSeen  string
	}
	err := db.Table("events").
		Select("distinct_id, MIN("+dateExpr+") as first_seen").
		Where("event_name = ? AND timestamp BETWEEN ? AND ?", "pageview", from, to).
		Group("distinct_id").
		Scan(&raw).Error
	if err != nil {
		return nil, err
	}

	byDate := make(map[string]int64, len(raw))
	for _, r := range raw {
		byDate[r.FirstSeen]++
	}

	filled := make([]PageviewPoint, 0)
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		filled = append(filled, PageviewPoint{Date: key, Count: byDate[key]})
	}

	return filled, nil
}

// getPageviewsByHour buckets a single day's unique visitors by the
// hour of their first pageview that day — same first-occurrence
// principle as getPageviewsByDay, just at hourly granularity. Used
// for a "today only" or any single-date from/to selection where a
// daily bucket would otherwise collapse to one point.
func getPageviewsByHour(db *gorm.DB, from, to time.Time) ([]PageviewPoint, error) {
	hourExpr := dialect.HourTrunc(db.Dialector.Name(), "timestamp")
	var raw []struct {
		DistinctID string
		FirstSeen  string
	}
	err := db.Table("events").
		Select("distinct_id, MIN("+hourExpr+") as first_seen").
		Where("event_name = ? AND timestamp BETWEEN ? AND ?", "pageview", from, to).
		Group("distinct_id").
		Scan(&raw).Error
	if err != nil {
		return nil, err
	}

	byHour := make(map[string]int64, len(raw))
	for _, r := range raw {
		byHour[r.FirstSeen]++
	}

	filled := make([]PageviewPoint, 0, 24)
	for h := 0; h < 24; h++ {
		key := padHour(h) + ":00"
		filled = append(filled, PageviewPoint{Date: key, Count: byHour[key]})
	}

	return filled, nil
}

func padHour(h int) string {
	if h < 10 {
		return "0" + itoa(h)
	}
	return itoa(h)
}

// parseRange reads from/to query params, defaulting to the last 7 days
// if either is missing.
func parseRange(r *http.Request) (time.Time, time.Time, error) {
	return ParseDateRange(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
}

// ParseDateRange parses optional from/to YYYY-MM-DD strings, defaulting
// to the last 7 days if either is missing. Shared by the HTTP handlers
// (via parseRange) and the MCP tools, which take the same from/to
// strings directly as tool arguments instead of query params.
func ParseDateRange(fromStr, toStr string) (time.Time, time.Time, error) {
	now := time.Now().UTC()
	from := now.AddDate(0, 0, -7)
	to := now

	if fromStr != "" {
		parsed, err := time.Parse("2006-01-02", fromStr)
		if err != nil {
			return from, to, errBadDate("from")
		}
		from = parsed
	}

	if toStr != "" {
		parsed, err := time.Parse("2006-01-02", toStr)
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

// groupByCount runs a query counting unique visitors (distinct_id) per
// value of the given column, then adds a pct of total to each row.
// Shared by countries/devices/browsers, which are otherwise identical
// queries against different columns.
//
// Counts unique visitors rather than raw pageviews — someone who
// loads five pages in one visit should show up as one visitor under
// their country/device/browser, not five, or these breakdowns would
// skew toward whoever browses the most rather than reflecting who's
// actually visiting.
func groupByCount(db *gorm.DB, column string, from, to time.Time) ([]countRow, error) {
	var rows []countRow
	err := db.Table("events").
		Select(column + " as value, count(distinct distinct_id) as count").
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

