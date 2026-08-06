package query

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"gorm.io/gorm"
)

// exportEvent is the shape of one exported event row, covering every
// enrichment field added at ingestion, not just the original raw
// payload, since export should give people everything Gnat knows.
type exportEvent struct {
	ID          uint      `json:"id"`
	EventName   string    `json:"event_name"`
	DistinctID  string    `json:"distinct_id"`
	Properties  string    `json:"properties"`
	Country     string    `json:"country"`
	Browser     string    `json:"browser"`
	OS          string    `json:"os"`
	DeviceType  string    `json:"device_type"`
	VisitorHash string    `json:"visitor_hash"`
	Timestamp   time.Time `json:"timestamp"`
}

// ExportHandler returns GET /api/export?format=csv|json|jsonl&from=...&to=...
// Defaults to json if format is omitted or unrecognized.
func ExportHandler(db *gorm.DB, apiKey string) http.HandlerFunc {
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

		var events []exportEvent
		err = db.Table("events").
			Select("id, event_name, distinct_id, properties, country, browser, os, device_type, visitor_hash, timestamp").
			Where("timestamp BETWEEN ? AND ?", from, to).
			Order("timestamp").
			Scan(&events).Error

		if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}

		format := r.URL.Query().Get("format")

		switch format {
		case "csv":
			writeCSV(w, events)
		case "jsonl":
			writeJSONL(w, events)
		default:
			writeJSON(w, events)
		}
	}
}

func writeJSON(w http.ResponseWriter, events []exportEvent) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="gnat-export.json"`)
	json.NewEncoder(w).Encode(events)
}

// writeJSONL writes one JSON object per line, no wrapping array. Suits
// streaming/incremental processing by external pipeline tools better
// than a single large JSON array, which must be fully parsed before use.
func writeJSONL(w http.ResponseWriter, events []exportEvent) {
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Content-Disposition", `attachment; filename="gnat-export.jsonl"`)

	encoder := json.NewEncoder(w)
	for _, e := range events {
		// errors here are effectively unrecoverable mid-stream (headers
		// already sent), so we just stop rather than trying to report a
		// second error status after the response has started.
		if err := encoder.Encode(e); err != nil {
			return
		}
	}
}

func writeCSV(w http.ResponseWriter, events []exportEvent) {
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", `attachment; filename="gnat-export.csv"`)

	writer := csv.NewWriter(w)
	defer writer.Flush()

	header := []string{"id", "event_name", "distinct_id", "properties", "country", "browser", "os", "device_type", "visitor_hash", "timestamp"}
	if err := writer.Write(header); err != nil {
		return
	}

	for _, e := range events {
		row := []string{
			fmt.Sprintf("%d", e.ID),
			e.EventName,
			e.DistinctID,
			e.Properties,
			e.Country,
			e.Browser,
			e.OS,
			e.DeviceType,
			e.VisitorHash,
			e.Timestamp.Format(time.RFC3339),
		}
		if err := writer.Write(row); err != nil {
			return
		}
	}
}
