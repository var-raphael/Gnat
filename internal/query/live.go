package query

import (
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"gorm.io/gorm"
)

type liveVisitorPoint struct {
	ID            string `json:"id"`
	Page          string `json:"page"`
	CountryCode   string `json:"country_code"`
	CountryName   string `json:"country_name"`
	Device        string `json:"device"`
	Browser       string `json:"browser"`
	ActiveSeconds int64  `json:"active_seconds"`
}

type liveEventRow struct {
	DistinctID string
	Country    string
	DeviceType string
	Browser    string
	Timestamp  time.Time
}

// LiveVisitorsHandler returns GET /api/stats/live
// A visitor is "live" if they have any event within liveWindow. Their
// country/device/browser come from their most recent event, page from
// their most recent pageview. active_seconds is time since their current
// visit started — i.e. since the last gap between events exceeding
// liveWindow, so it resets once they've actually stepped away rather
// than accumulating across a longer 30-min analytics session.
func LiveVisitorsHandler(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		now := time.Now().UTC()
		// activeStart (below) walks back through a visitor's events as
		// long as consecutive gaps stay under liveWindow — that chain can
		// extend arbitrarily far if someone's been continuously active
		// (a heartbeat every 30s) for a long time, e.g. an hour-long
		// visit. lookback has to cover that, not just liveWindow itself,
		// or a long-active visitor's time would be silently truncated to
		// whatever the lookback happened to cover. maxVisitLookback caps
		// it at a generous bound rather than querying unbounded history.
		lookback := now.Add(-maxVisitLookback)

		var rows []liveEventRow
		err := db.Table("events").
			Select("distinct_id, country, device_type, browser, timestamp").
			Where("timestamp BETWEEN ? AND ?", lookback, now).
			Order("distinct_id, timestamp").
			Scan(&rows).Error
		if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}

		lastPathByVisitor, err := extractLastPaths(db, lookback, now)
		if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}

		byVisitor := make(map[string][]liveEventRow)
		for _, row := range rows {
			byVisitor[row.DistinctID] = append(byVisitor[row.DistinctID], row)
		}

		results := make([]liveVisitorPoint, 0)
		for distinctID, events := range byVisitor {
			sort.Slice(events, func(i, j int) bool { return events[i].Timestamp.Before(events[j].Timestamp) })

			latest := events[len(events)-1]
			if now.Sub(latest.Timestamp) > liveWindow {
				continue
			}

			// activeStart walks back through consecutive events as long as
			// each gap is within liveWindow (5 min) — i.e. no missed
			// heartbeat cycle (heartbeats fire every 30s). The moment a
			// gap exceeds that, the visitor is treated as having left and
			// come back, so active time restarts from the more recent
			// event rather than continuing to add up from their original
			// arrival. This deliberately does NOT use the 30-minute
			// sessionGap used elsewhere (statssummary.go, retention) —
			// that longer window is right for counting a return within
			// 30 min as "the same session" for analytics purposes, but
			// wrong for "how long have they currently been active", which
			// should reset the moment they've actually stepped away.
			activeStart := latest.Timestamp
			for i := len(events) - 1; i > 0; i-- {
				gap := events[i].Timestamp.Sub(events[i-1].Timestamp)
				if gap > liveWindow {
					break
				}
				activeStart = events[i-1].Timestamp
			}

			results = append(results, liveVisitorPoint{
				ID:            shortID(distinctID),
				Page:          lastPathByVisitor[distinctID],
				CountryCode:   latest.Country,
				CountryName:   latest.Country,
				Device:        latest.DeviceType,
				Browser:       latest.Browser,
				ActiveSeconds: int64(now.Sub(activeStart).Seconds()),
			})
		}

		sort.Slice(results, func(i, j int) bool { return results[i].ActiveSeconds > results[j].ActiveSeconds })

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(results)
	}
}

func extractLastPaths(db *gorm.DB, from, to time.Time) (map[string]string, error) {
	var rows []struct {
		DistinctID string
		Properties string
		Timestamp  time.Time
	}
	err := db.Table("events").
		Select("distinct_id, properties, timestamp").
		Where("event_name = ? AND timestamp BETWEEN ? AND ?", "pageview", from, to).
		Order("distinct_id, timestamp").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	last := make(map[string]string)
	for _, row := range rows {
		var props struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal([]byte(row.Properties), &props); err == nil && props.Path != "" {
			last[row.DistinctID] = props.Path
		}
	}
	return last, nil
}

func shortID(distinctID string) string {
	if len(distinctID) <= 8 {
		return "v_" + distinctID
	}
	return "v_" + distinctID[:8]
}
