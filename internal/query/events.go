package query

import (
	"encoding/json"
	"net/http"

	"gorm.io/gorm"

	"github.com/var-raphael/gnat/internal/dialect"
)

// eventBreakdownPoint is one row when no ?name= filter is given: total
// count per distinct event name over the range.
type eventBreakdownPoint struct {
	EventName string `json:"event_name"`
	Count     int64  `json:"count"`
}

// eventSeriesPoint is one row when ?name= is given: a daily time series
// for that one event, same shape as the pageviews endpoint.
type eventSeriesPoint struct {
	Date  string `json:"date"`
	Count int64  `json:"count"`
}

// EventsHandler returns GET /api/stats/events?name=...&from=...&to=...
//
// If name is omitted: returns a breakdown of counts per distinct event
// name across the range, for an overview of "what custom events fire and
// how often."
//
// If name is given: returns a daily time series for that one event, for
// drilling into a specific event's trend.
func EventsHandler(db *gorm.DB) http.HandlerFunc {
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

		name := r.URL.Query().Get("name")

		w.Header().Set("Content-Type", "application/json")

		if name == "" {
			var results []eventBreakdownPoint
			err = db.Table("events").
				Select("event_name, count(*) as count").
				Where("timestamp BETWEEN ? AND ?", from, to).
				Group("event_name").
				Order("count DESC").
				Scan(&results).Error

			if err != nil {
				http.Error(w, "query failed", http.StatusInternalServerError)
				return
			}
			json.NewEncoder(w).Encode(results)
			return
		}

		var results []eventSeriesPoint
		// See internal/dialect's package doc for why this is the one
		// engine-specific seam here, same as the pageviews endpoint.
		dateExpr := dialect.DateTrunc(db.Dialector.Name(), "timestamp")
		err = db.Table("events").
			Select(dateExpr + " as date, count(*) as count").
			Where("event_name = ? AND timestamp BETWEEN ? AND ?", name, from, to).
			Group("date").
			Order("date").
			Scan(&results).Error

		if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(results)
	}
}
