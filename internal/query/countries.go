package query

import (
	"encoding/json"
	"net/http"
	"time"

	"gorm.io/gorm"
)

// CountryPoint is one country's pageview count and share for a range.
type CountryPoint struct {
	Code  string  `json:"code"`
	Count int64   `json:"count"`
	Pct   float64 `json:"pct"`
}

// CountriesHandler returns GET /api/stats/countries?from=...&to=...
// Country names and tiers are left to the frontend (country-tiers.json)
// rather than duplicated here.
func CountriesHandler(db *gorm.DB) http.HandlerFunc {
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

		results, err := GetCountries(db, from, to)
		if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(results)
	}
}

// GetCountries returns visitor counts by country code for the given
// range, each with its percentage share of the total.
func GetCountries(db *gorm.DB, from, to time.Time) ([]CountryPoint, error) {
	rows, err := groupByCount(db, "country", from, to)
	if err != nil {
		return nil, err
	}

	var total int64
	for _, row := range rows {
		total += row.Count
	}

	results := make([]CountryPoint, 0, len(rows))
	for _, row := range rows {
		if row.Value == "" {
			continue
		}
		results = append(results, CountryPoint{
			Code:  row.Value,
			Count: row.Count,
			Pct:   pctOfTotal(row.Count, total),
		})
	}
	return results, nil
}
