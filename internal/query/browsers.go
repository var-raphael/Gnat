package query

import (
	"encoding/json"
	"net/http"
	"time"

	"gorm.io/gorm"
)

// BrowserPoint is one browser's pageview count and share for a range.
type BrowserPoint struct {
	Name  string  `json:"name"`
	Count int64   `json:"count"`
	Pct   float64 `json:"pct"`
}

func BrowsersHandler(db *gorm.DB) http.HandlerFunc {
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

		results, err := GetBrowsers(db, from, to)
		if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(results)
	}
}

// GetBrowsers returns visitor counts by browser for the given range,
// each with its percentage share of the total.
func GetBrowsers(db *gorm.DB, from, to time.Time) ([]BrowserPoint, error) {
	rows, err := groupByCount(db, "browser", from, to)
	if err != nil {
		return nil, err
	}

	var total int64
	for _, row := range rows {
		total += row.Count
	}

	results := make([]BrowserPoint, 0, len(rows))
	for _, row := range rows {
		if row.Value == "" {
			continue
		}
		results = append(results, BrowserPoint{
			Name:  row.Value,
			Count: row.Count,
			Pct:   pctOfTotal(row.Count, total),
		})
	}
	return results, nil
}
