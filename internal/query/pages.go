package query

import (
	"encoding/json"
	"net/http"

	"gorm.io/gorm"

	"github.com/var-raphael/gnat/internal/dialect"
)

type pagePoint struct {
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

		pathExpr := dialect.JSONExtract(db.Dialector.Name(), "properties", "path")

		var results []pagePoint
		err = db.Table("events").
			Select(pathExpr + " as path, count(*) as count").
			Where("event_name = ? AND timestamp BETWEEN ? AND ?", "pageview", from, to).
			Group("path").
			Order("count DESC").
			Scan(&results).Error
		if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}

		final := make([]pagePoint, 0, len(results))
		for _, row := range results {
			if row.Path == "" {
				continue
			}
			final = append(final, row)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(final)
	}
}
