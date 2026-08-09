package query

import (
	"encoding/json"
	"net/http"

	"gorm.io/gorm"
)

// EventNamesHandler returns GET /api/event-names
// All distinct event names ever seen, including pageview, for use in
// the funnel builder's step dropdown.
func EventNamesHandler(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var names []string
		err := db.Table("events").
			Where("event_name != ?", "heartbeat").
			Distinct("event_name").
			Order("event_name").
			Pluck("event_name", &names).Error
		if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(names)
	}
}
