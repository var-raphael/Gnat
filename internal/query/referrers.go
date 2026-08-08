package query

import (
	"encoding/json"
	"net/http"

	"gorm.io/gorm"

	"github.com/var-raphael/gnat/internal/dialect"
)

type referrerPoint struct {
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

		referrerExpr := dialect.JSONExtract(db.Dialector.Name(), "properties", "referrer")

		var raw []struct {
			Referrer string
			Count    int64
		}
		err = db.Table("events").
			Select(referrerExpr + " as referrer, count(*) as count").
			Where("event_name = ? AND timestamp BETWEEN ? AND ?", "pageview", from, to).
			Group("referrer").
			Order("count DESC").
			Scan(&raw).Error
		if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}

		results := make([]referrerPoint, 0, len(raw))
		for _, row := range raw {
			if row.Referrer == "" {
				continue
			}
			results = append(results, referrerPoint{
				Referrer: row.Referrer,
				Count:    row.Count,
				Category: "referral",
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(results)
	}
}
