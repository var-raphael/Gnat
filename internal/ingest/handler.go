package ingest

import (
	"encoding/json"
	"net/http"
	"time"

	"gorm.io/gorm"

	"github.com/var-raphael/gnat/internal/storage"
)

// eventPayload is the shape POSTed to /api/event. Properties is left as
// json.RawMessage so arbitrary caller-defined fields pass through without
// needing a fixed schema, then get stored as a JSON string.
type eventPayload struct {
	EventName  string          `json:"event_name"`
	DistinctID string          `json:"distinct_id"`
	Properties json.RawMessage `json:"properties"`
	Timestamp  *time.Time      `json:"timestamp"`
}

// Handler returns the http.HandlerFunc for /api/event. It needs the db
// connection and the configured API key to authenticate requests.
func Handler(db *gorm.DB, apiKey string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// CORS: wide open on purpose. This is a write-only endpoint, the
		// API key is what controls data ownership, not request origin.
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key")
	
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if !authorized(r, apiKey) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		var payload eventPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}

		if payload.EventName == "" {
			http.Error(w, "event_name is required", http.StatusBadRequest)
			return
		}
		if payload.DistinctID == "" {
			http.Error(w, "distinct_id is required", http.StatusBadRequest)
			return
		}

		ts := time.Now().UTC()
		if payload.Timestamp != nil {
			ts = *payload.Timestamp
		}

		propsStr := "{}"
		if len(payload.Properties) > 0 {
			propsStr = string(payload.Properties)
		}

		event := storage.Event{
			// SiteID is hardcoded to 1 for now, single-site mode. Multi-site
			// resolution (via api key -> site lookup) comes with the paid tier.
			SiteID:     1,
			EventName:  payload.EventName,
			DistinctID: payload.DistinctID,
			Properties: propsStr,
			Timestamp:  ts,
		}

		if err := db.Create(&event).Error; err != nil {
			http.Error(w, "failed to store event", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusAccepted)
	}
}

// authorized checks the API key via the Authorization header
// ("Bearer <key>") or an X-API-Key header, either is accepted.
func authorized(r *http.Request, apiKey string) bool {
	if key := r.Header.Get("X-API-Key"); key != "" {
		return key == apiKey
	}
	auth := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(auth) > len(prefix) && auth[:len(prefix)] == prefix {
		return auth[len(prefix):] == apiKey
	}
	return false
}
