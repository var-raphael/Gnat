package ingest

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/var-raphael/gnat/internal/geo"
	"github.com/var-raphael/gnat/internal/storage"
)

type eventPayload struct {
	EventName  string          `json:"event_name"`
	DistinctID string          `json:"distinct_id"`
	Properties json.RawMessage `json:"properties"`
	Timestamp  *time.Time      `json:"timestamp"`
	APIKey     string          `json:"api_key,omitempty"`
}

// Handler returns the http.HandlerFunc for /api/event.
func Handler(db *gorm.DB, apiKey string, geoClient *geo.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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

		var payload eventPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}

		if !authorized(r, payload.APIKey, apiKey) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
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

		ip := clientIP(r)
		ua := parseUserAgent(r.UserAgent())

		event := storage.Event{
			SiteID:      1,
			EventName:   payload.EventName,
			DistinctID:  payload.DistinctID,
			Properties:  propsStr,
			VisitorHash: hashVisitorIP(ip),
			Browser:     ua.Browser,
			OS:          ua.OS,
			DeviceType:  ua.DeviceType,
			Timestamp:   ts,
		}

		if err := db.Create(&event).Error; err != nil {
			http.Error(w, "failed to store event", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusAccepted)

		if geoClient != nil {
			eventID := event.ID
			go enrichCountry(db, geoClient, eventID, ip)
		}
	}
}

func enrichCountry(db *gorm.DB, geoClient *geo.Client, eventID uint, ip string) {
	country := geoClient.Lookup(ip)
	if country == "" {
		return
	}
	db.Model(&storage.Event{}).Where("id = ?", eventID).Update("country", country)
}

const devFallbackIP = "8.8.8.8"

// clientIP extracts the real visitor IP, preferring X-Forwarded-For (set
// by a reverse proxy like Nginx/Caddy in front of Gnat) over RemoteAddr.
// Falls back to devFallbackIP only for loopback/private addresses.
func clientIP(r *http.Request) string {
	var ip string

	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		parts := strings.Split(fwd, ",")
		ip = strings.TrimSpace(parts[0])
	} else {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		} else {
			ip = host
		}
	}

	if isLoopbackOrPrivate(ip) {
		return devFallbackIP
	}
	return ip
}

func isLoopbackOrPrivate(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return true
	}
	return parsed.IsLoopback() || parsed.IsPrivate() || parsed.IsUnspecified()
}

func authorized(r *http.Request, bodyKey string, apiKey string) bool {
	if key := r.Header.Get("X-API-Key"); key != "" {
		return key == apiKey
	}
	auth := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(auth) > len(prefix) && auth[:len(prefix)] == prefix {
		return auth[len(prefix):] == apiKey
	}
	if bodyKey != "" {
		return bodyKey == apiKey
	}
	return false
}
