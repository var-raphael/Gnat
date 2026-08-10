package query

import (
	"encoding/json"
	"net/http"
	"time"

	"gorm.io/gorm"

	"github.com/var-raphael/gnat/internal/dialect"
)

// PagePoint is one path's pageview count for a given range.
type PagePoint struct {
	Path  string `json:"path"`
	Count int64  `json:"count"`
}

func TopPagesHandler(db *gorm.DB) http.HandlerFunc {
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

		final, err := GetTopPages(db, from, to)
		if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(final)
	}
}

// GetTopPages returns paths ordered by pageview count (descending) for
// the given range, excluding rows with an empty path.
func GetTopPages(db *gorm.DB, from, to time.Time) ([]PagePoint, error) {
	pathExpr := dialect.JSONExtract(db.Dialector.Name(), "properties", "path")

	var results []PagePoint
	err := db.Table("events").
		Select(pathExpr + " as path, count(*) as count").
		Where("event_name = ? AND timestamp BETWEEN ? AND ?", "pageview", from, to).
		Group("path").
		Order("count DESC").
		Scan(&results).Error
	if err != nil {
		return nil, err
	}

	final := make([]PagePoint, 0, len(results))
	for _, row := range results {
		if row.Path == "" {
			continue
		}
		final = append(final, row)
	}
	return final, nil
}
