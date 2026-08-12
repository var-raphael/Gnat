package ingest

import (
	"bytes"
	"encoding/json"
	"io"
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
	// APIKey is only ever populated for navigator.sendBeacon requests
	// (see tracker.js's send()) — sendBeacon can't set custom headers,
	// so the beacon payload carries the key in the JSON body instead
	// of X-API-Key. Every other request path (fetch) sends the key as
	// a header and leaves this empty.
	APIKey string `json:"api_key"`
}

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

		// Read the body once up front — authorized() needs to inspect it
		// for beacon requests (see eventPayload.APIKey's doc comment),
		// and the body can only be read once. bytes.NewReader below lets
		// the later json.Decode read the same bytes again.
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed to read body", http.StatusBadRequest)
			return
		}
		r.Body.Close()

		if !authorized(r, bodyBytes, apiKey) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		siteID, ok := resolveSite(db, r)
		if !ok {
			w.WriteHeader(http.StatusAccepted)
			return
		}

		var payload eventPayload
		if err := json.NewDecoder(bytes.NewReader(bodyBytes)).Decode(&payload); err != nil {
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

		ip := clientIP(r)
		ua := parseUserAgent(r.UserAgent())

		event := storage.Event{
			SiteID:      siteID,
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


// authorized checks the request's API key against three places, in
// order: the X-API-Key header, an Authorization: Bearer header, or —
// for navigator.sendBeacon requests, which cannot set custom headers
// — an api_key field in the JSON body itself (see eventPayload.APIKey).
// Without this third check, every beacon-sent event (pageview_end, in
// particular — see tracker.js's trackPageviewEnd) is silently rejected
// with 401 and never stored, since sendBeacon has no way to attach the
// header the other two checks look for.
func authorized(r *http.Request, bodyBytes []byte, apiKey string) bool {
	if key := r.Header.Get("X-API-Key"); key != "" {
		return key == apiKey
	}
	auth := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(auth) > len(prefix) && auth[:len(prefix)] == prefix {
		return auth[len(prefix):] == apiKey
	}

	var beacon struct {
		APIKey string `json:"api_key"`
	}
	if err := json.Unmarshal(bodyBytes, &beacon); err == nil && beacon.APIKey != "" {
		return beacon.APIKey == apiKey
	}

	return false
}
