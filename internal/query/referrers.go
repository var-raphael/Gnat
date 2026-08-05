package query

import (
	"encoding/json"
	"net/http"

	"gorm.io/gorm"
)

type referrerPoint struct {
	Referrer string `json:"referrer"`
	Count    int64  `json:"count"`
}

// ReferrersHandler returns GET /api/stats/referrers?from=...&to=...
// Breaks down pageview counts by referrer, extracted from the JSON
// properties column. "direct" groups pageviews with no referrer (empty
// or null), matching how every analytics tool treats a bare visit.
//
// Known gap: json_extract is SQLite syntax. Postgres uses ->> and MySQL
// uses JSON_EXTRACT with different path syntax, this needs a dialect
// switch before it's tested against those backends, same open item as
// the strftime date grouping in pageviews/events.
func ReferrersHandler(db *gorm.DB, apiKey string) http.HandlerFunc {
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

		var results []referrerPoint

		err = db.Table("events").
			Select(`
				CASE
					WHEN json_extract(properties, '$.referrer') IS NULL
					     OR json_extract(properties, '$.referrer') = ''
					THEN 'direct'
					ELSE json_extract(properties, '$.referrer')
				END AS referrer,
				count(*) as count
			`).
			Where("event_name = ? AND timestamp BETWEEN ? AND ?", "pageview", from, to).
			Group("referrer").
			Order("count DESC").
			Scan(&results).Error

		if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(results)
	}
}
