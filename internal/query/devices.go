package query

import (
	"encoding/json"
	"net/http"
	"time"

	"gorm.io/gorm"
)

// DevicePoint is one device type's pageview count and share for a range.
type DevicePoint struct {
	Type  string  `json:"type"`
	Count int64   `json:"count"`
	Pct   float64 `json:"pct"`
}

// DevicesHandler returns GET /api/stats/devices?from=...&to=...
func DevicesHandler(db *gorm.DB) http.HandlerFunc {
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

		results, err := GetDevices(db, from, to)
		if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(results)
	}
}

// GetDevices returns visitor counts by device type for the given
// range, each with its percentage share of the total.
func GetDevices(db *gorm.DB, from, to time.Time) ([]DevicePoint, error) {
	rows, err := groupByCount(db, "device_type", from, to)
	if err != nil {
		return nil, err
	}

	var total int64
	for _, row := range rows {
		total += row.Count
	}

	results := make([]DevicePoint, 0, len(rows))
	for _, row := range rows {
		if row.Value == "" {
			continue
		}
		results = append(results, DevicePoint{
			Type:  row.Value,
			Count: row.Count,
			Pct:   pctOfTotal(row.Count, total),
		})
	}
	return results, nil
}
