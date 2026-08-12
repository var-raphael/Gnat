package query

import (
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"gorm.io/gorm"

	"github.com/var-raphael/gnat/internal/dialect"
)

// CountryTimePoint is one visitor's first pageview from a single
// country, labeled with the exact date and time it happened (not a
// pre-bucketed hour/day slot — see countryTimeBreakdown for why).
type CountryTimePoint struct {
	Label string `json:"label"`
	Count int64  `json:"count"`
}

// CountryDimensionPoint is one value of a dimension (device/browser/os)
// and its unique-visitor count, scoped to a single country.
type CountryDimensionPoint struct {
	Value string  `json:"value"`
	Count int64   `json:"count"`
	Pct   float64 `json:"pct"`
}

// CountryPageVisit is one specific visitor's actual time spent on a
// page — the real, individual duration from that one pageview_end
// event, not blended into an average with anyone else's visit.
type CountryPageVisit struct {
	DistinctID    string  `json:"distinct_id"`
	Timestamp     string  `json:"timestamp"`
	TimespentSecs float64 `json:"timespent_seconds"`
}

// CountryPagePoint is one page visited by visitors from a single
// country: how many times it was viewed, and each individual visit's
// actual time spent (see countryPageBreakdown's doc comment for why
// this is real per-visit data rather than a blended average).
type CountryPagePoint struct {
	Path   string             `json:"path"`
	Views  int64              `json:"views"`
	Visits []CountryPageVisit `json:"visits"`
}

// CountryDetail is everything the country drill-down modal shows for
// one country: when its visitors show up, what they're on, and what
// they look at.
type CountryDetail struct {
	Country  string                  `json:"country"`
	Time     []CountryTimePoint      `json:"time"`
	Devices  []CountryDimensionPoint `json:"devices"`
	Browsers []CountryDimensionPoint `json:"browsers"`
	OS       []CountryDimensionPoint `json:"os"`
	Pages    []CountryPagePoint      `json:"pages"`
}

// CountryDetailHandler returns GET /api/stats/country-detail?country=NG&from=...&to=...
func CountryDetailHandler(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		country := r.URL.Query().Get("country")
		if country == "" {
			http.Error(w, "country is required", http.StatusBadRequest)
			return
		}

		from, to, err := parseRange(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		result, err := GetCountryDetail(db, from, to, country)
		if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

// GetCountryDetail aggregates everything the country drill-down modal
// needs for one country: exact visit timestamps (see
// countryTimeBreakdown), device/browser/OS splits, and pages visited
// with their real average engaged time from pageview_end events.
func GetCountryDetail(db *gorm.DB, from, to time.Time, country string) (CountryDetail, error) {
	detail := CountryDetail{Country: country}

	timePoints, err := countryTimeBreakdown(db, from, to, country)
	if err != nil {
		return detail, err
	}
	detail.Time = timePoints

	devices, err := countryDimensionBreakdown(db, from, to, country, "device_type")
	if err != nil {
		return detail, err
	}
	detail.Devices = devices

	browsers, err := countryDimensionBreakdown(db, from, to, country, "browser")
	if err != nil {
		return detail, err
	}
	detail.Browsers = browsers

	os, err := countryDimensionBreakdown(db, from, to, country, "os")
	if err != nil {
		return detail, err
	}
	detail.OS = os

	pages, err := countryPageBreakdown(db, from, to, country)
	if err != nil {
		return detail, err
	}
	detail.Pages = pages

	return detail, nil
}

// countryTimeBreakdown lists when this country's visitors first
// showed up in the range — one row per distinct visitor's earliest
// pageview, not an hour/day skeleton. A chart needs continuous zero-
// filled buckets to stay readable, but a modal list doesn't: showing
// 24 mostly-empty hour rows (or one row per calendar day across a
// multi-week range) is just noise when a handful of exact visit
// timestamps says more. Always includes both date and time, even for
// multi-day ranges, so a moment is identifiable on its own without
// relying on which section of the list it's in.
func countryTimeBreakdown(db *gorm.DB, from, to time.Time, country string) ([]CountryTimePoint, error) {
	var raw []struct {
		DistinctID string
		FirstSeen  string
	}
	err := db.Table("events").
		Select("distinct_id, MIN(timestamp) as first_seen").
		Where("event_name = ? AND country = ? AND timestamp BETWEEN ? AND ?", "pageview", country, from, to).
		Group("distinct_id").
		Scan(&raw).Error
	if err != nil {
		return nil, err
	}

	points := make([]CountryTimePoint, 0, len(raw))
	for _, r := range raw {
		ts, err := parseAggregateTime(r.FirstSeen)
		if err != nil {
			continue
		}
		points = append(points, CountryTimePoint{
			Label: ts.Format("2006-01-02 15:04"),
			Count: 1,
		})
	}

	sort.Slice(points, func(i, j int) bool { return points[i].Label < points[j].Label })
	return points, nil
}

// countryDimensionBreakdown is groupByCount's per-country counterpart
// (see query.go) — same unique-visitor basis, same column-agnostic
// shape, just scoped to one country and returning the modal's own
// point type instead of the shared countRow.
func countryDimensionBreakdown(db *gorm.DB, from, to time.Time, country, column string) ([]CountryDimensionPoint, error) {
	var rows []countRow
	err := db.Table("events").
		Select(column+" as value, count(distinct distinct_id) as count").
		Where("event_name = ? AND country = ? AND timestamp BETWEEN ? AND ?", "pageview", country, from, to).
		Group(column).
		Order("count DESC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	var total int64
	for _, row := range rows {
		total += row.Count
	}

	points := make([]CountryDimensionPoint, 0, len(rows))
	for _, row := range rows {
		if row.Value == "" {
			continue
		}
		points = append(points, CountryDimensionPoint{
			Value: row.Value,
			Count: row.Count,
			Pct:   pctOfTotal(row.Count, total),
		})
	}
	return points, nil
}

// countryPageBreakdown pairs each page's view count with the actual
// list of individual visits and their real engaged time, scoped to
// one country.
//
// View counts come from pageview events. Per-visit time comes from a
// separate event, pageview_end, which the tracker fires on navigation
// or tab close with a `timespent` property — actual measured engaged
// time (mouse/scroll/keypress activity, excluding idle time and
// background tabs), not a gap-between-events estimate. Each visit is
// listed individually rather than blended into a page-wide average —
// one visitor spending 90s and another spending 10s on the same page
// is two different, real numbers, not one number that represents
// neither of them. This is the only place in the codebase that reads
// pageview_end/timespent; every other "time on page" figure elsewhere
// (see avgTimeOnPage in statssummary.go) still uses the older
// gap-based estimate, which remains unchanged.
func countryPageBreakdown(db *gorm.DB, from, to time.Time, country string) ([]CountryPagePoint, error) {
	pathExpr := dialect.JSONExtract(db.Dialector.Name(), "properties", "path")

	var viewRows []struct {
		Path  string
		Views int64
	}
	err := db.Table("events").
		Select("COALESCE("+pathExpr+", '') as path, count(*) as views").
		Where("event_name = ? AND country = ? AND timestamp BETWEEN ? AND ?", "pageview", country, from, to).
		Group("path").
		Scan(&viewRows).Error
	if err != nil {
		return nil, err
	}

	timeExpr := dialect.JSONExtract(db.Dialector.Name(), "properties", "timespent")
	var visitRows []struct {
		Path       string
		DistinctID string
		Timestamp  time.Time
		Timespent  *float64
	}
	err = db.Table("events").
		Select("COALESCE("+pathExpr+", '') as path, distinct_id, timestamp, "+timeExpr+" as timespent").
		Where("event_name = ? AND country = ? AND timestamp BETWEEN ? AND ?", "pageview_end", country, from, to).
		Order("timestamp").
		Scan(&visitRows).Error
	if err != nil {
		return nil, err
	}

	visitsByPath := make(map[string][]CountryPageVisit)
	for _, r := range visitRows {
		if r.Path == "" || r.Timespent == nil {
			continue
		}
		visitsByPath[r.Path] = append(visitsByPath[r.Path], CountryPageVisit{
			DistinctID:    r.DistinctID,
			Timestamp:     r.Timestamp.Format("2006-01-02 15:04"),
			TimespentSecs: *r.Timespent,
		})
	}

	pages := make([]CountryPagePoint, 0, len(viewRows))
	for _, row := range viewRows {
		if row.Path == "" {
			continue
		}
		pages = append(pages, CountryPagePoint{
			Path:   row.Path,
			Views:  row.Views,
			Visits: visitsByPath[row.Path],
		})
	}

	sort.Slice(pages, func(i, j int) bool { return pages[i].Views > pages[j].Views })
	return pages, nil
}
