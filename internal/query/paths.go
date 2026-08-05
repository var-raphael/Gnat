package query

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	pathWindow       = 24 * time.Hour
	defaultPathDepth = 5
	minPathDepth     = 2
	maxPathDepth     = 10
	topPathsLimit    = 10
)

type pathResult struct {
	Path  string `json:"path"`
	Count int64  `json:"count"`
}

// anchorOccurrence is one instance of the anchor event, one visitor may
// have several within the date range.
type anchorOccurrence struct {
	DistinctID string
	Timestamp  string // raw SQLite text, parsed below
}

// PathsHandler returns GET /api/stats/paths?anchor_event=...&depth=...&from=...&to=...
//
// For each occurrence of anchor_event in the date range, walks backward
// through that visitor's events within pathWindow (24h), takes up to
// depth prior events, collapses consecutive duplicates, and groups
// identical resulting paths. Returns the top 10 paths by count, with
// everything else bucketed as "Other".
func PathsHandler(db *gorm.DB, apiKey string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if !authorized(r, apiKey) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		anchorEvent := r.URL.Query().Get("anchor_event")
		if anchorEvent == "" {
			http.Error(w, "anchor_event query param is required", http.StatusBadRequest)
			return
		}

		depth := defaultPathDepth
		if v := r.URL.Query().Get("depth"); v != "" {
			parsed, err := strconv.Atoi(v)
			if err != nil {
				http.Error(w, "depth must be an integer", http.StatusBadRequest)
				return
			}
			if parsed < minPathDepth || parsed > maxPathDepth {
				http.Error(w, "depth must be between "+strconv.Itoa(minPathDepth)+" and "+strconv.Itoa(maxPathDepth), http.StatusBadRequest)
				return
			}
			depth = parsed
		}

		from, to, err := parseRange(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		results, err := computePaths(db, anchorEvent, depth, from, to)
		if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(results)
	}
}

func computePaths(db *gorm.DB, anchorEvent string, depth int, from, to time.Time) ([]pathResult, error) {
	var occurrences []anchorOccurrence
	err := db.Table("events").
		Select("distinct_id, timestamp").
		Where("event_name = ? AND timestamp BETWEEN ? AND ?", anchorEvent, from, to).
		Scan(&occurrences).Error
	if err != nil {
		return nil, err
	}

	pathCounts := make(map[string]int64)

	for _, occ := range occurrences {
		anchorTS, err := parseSQLiteTime(occ.Timestamp)
		if err != nil {
			continue
		}
		windowStart := anchorTS.Add(-pathWindow)

		var priorRaw []stepRowRaw2
		err = db.Table("events").
			Select("event_name, timestamp").
			Where("distinct_id = ? AND timestamp >= ? AND timestamp < ?", occ.DistinctID, windowStart, anchorTS).
			Order("timestamp DESC").
			Limit(depth).
			Scan(&priorRaw).Error
		if err != nil {
			continue
		}

		if len(priorRaw) == 0 {
			continue
		}

		names := make([]string, len(priorRaw))
		for i, row := range priorRaw {
			names[len(priorRaw)-1-i] = row.EventName
		}

		normalized := collapseConsecutiveDuplicates(names)
		normalized = append(normalized, anchorEvent)

		key := strings.Join(normalized, " > ")
		pathCounts[key]++
	}

	return topPaths(pathCounts), nil
}

type stepRowRaw2 struct {
	EventName string
	Timestamp string
}

func collapseConsecutiveDuplicates(names []string) []string {
	if len(names) == 0 {
		return names
	}
	result := make([]string, 0, len(names))
	result = append(result, names[0])
	for i := 1; i < len(names); i++ {
		if names[i] != names[i-1] {
			result = append(result, names[i])
		}
	}
	return result
}

func topPaths(counts map[string]int64) []pathResult {
	all := make([]pathResult, 0, len(counts))
	for path, count := range counts {
		all = append(all, pathResult{Path: path, Count: count})
	}

	for i := 0; i < len(all); i++ {
		maxIdx := i
		for j := i + 1; j < len(all); j++ {
			if all[j].Count > all[maxIdx].Count {
				maxIdx = j
			}
		}
		all[i], all[maxIdx] = all[maxIdx], all[i]
	}

	if len(all) <= topPathsLimit {
		return all
	}

	top := all[:topPathsLimit]
	var otherCount int64
	for _, p := range all[topPathsLimit:] {
		otherCount += p.Count
	}
	return append(top, pathResult{Path: "Other", Count: otherCount})
}
