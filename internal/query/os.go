package query

import (
	"encoding/json"
	"net/http"
	"time"

	"gorm.io/gorm"
)

// OSPoint is one operating system's unique-visitor count and share for
// a range.
type OSPoint struct {
	Name  string  `json:"name"`
	Count int64   `json:"count"`
	Pct   float64 `json:"pct"`
}

// OSHandler returns GET /api/stats/os?from=...&to=...
func OSHandler(db *gorm.DB) http.HandlerFunc {
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

		results, err := GetOS(db, from, to)
		if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(results)
	}
}

// GetOS returns unique-visitor counts by operating system for the
// given range, each with its percentage share of the total. Same
// unique-visitor basis as Countries/Devices/Browsers (see groupByCount)
// — someone loading five pages on the same OS counts once, not five
// times.
func GetOS(db *gorm.DB, from, to time.Time) ([]OSPoint, error) {
	rows, err := groupByCount(db, "os", from, to)
	if err != nil {
		return nil, err
	}

	var total int64
	for _, row := range rows {
		total += row.Count
	}

	results := make([]OSPoint, 0, len(rows))
	for _, row := range rows {
		if row.Value == "" {
			continue
		}
		results = append(results, OSPoint{
			Name:  row.Value,
			Count: row.Count,
			Pct:   pctOfTotal(row.Count, total),
		})
	}
	return results, nil
}
