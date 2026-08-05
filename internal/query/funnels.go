package query

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"
)

const funnelWindow = 24 * time.Hour

type funnelStepResult struct {
	Step  string `json:"step"`
	Count int64  `json:"count"`
}

// stepRowRaw matches what SQLite actually returns for a MIN(timestamp)
// aggregate: text, not a native datetime type. SQLite has no dedicated
// datetime storage class, so aggregating a datetime column always comes
// back as a string; GORM's driver can only auto-scan that into
// time.Time for direct (non-aggregated) column reads.
type stepRowRaw struct {
	DistinctID string
	StepTS     string
}

type stepRow struct {
	DistinctID string
	StepTS     time.Time
}

// sqliteTimeFormats covers the layouts SQLite/GORM commonly produce for
// stored timestamps, tried in order until one parses successfully.
var sqliteTimeFormats = []string{
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02 15:04:05.999999999Z07:00",
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02 15:04:05.999999999",
	"2006-01-02 15:04:05",
}

func parseSQLiteTime(s string) (time.Time, error) {
	var lastErr error
	for _, layout := range sqliteTimeFormats {
		t, err := time.Parse(layout, s)
		if err == nil {
			return t, nil
		}
		lastErr = err
	}
	return time.Time{}, lastErr
}

// FunnelsHandler returns GET /api/stats/funnels?steps=a,b,c&from=...&to=...
//
// steps is a comma-separated ordered list of event names, at least 2.
// Staged per-step queries: each step's MIN(timestamp) per visitor is
// fetched via SQLite aggregation, scoped to only visitors still in
// contention from the previous step, so memory tracks shrinking visitor
// counts rather than total matching events. SQLite-specific; Postgres/
// MySQL support is a known deferred gap.
func FunnelsHandler(db *gorm.DB, apiKey string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if !authorized(r, apiKey) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		stepsParam := r.URL.Query().Get("steps")
		if stepsParam == "" {
			http.Error(w, "steps query param is required, comma-separated event names", http.StatusBadRequest)
			return
		}
		steps := strings.Split(stepsParam, ",")
		for i := range steps {
			steps[i] = strings.TrimSpace(steps[i])
		}
		if len(steps) < 2 {
			http.Error(w, "steps requires at least 2 event names", http.StatusBadRequest)
			return
		}

		from, to, err := parseRange(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		results, err := computeFunnelStaged(db, steps, from, to)
		if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(results)
	}
}

func fetchStep(db *gorm.DB, query *gorm.DB) ([]stepRow, error) {
	var raw []stepRowRaw
	if err := query.Scan(&raw).Error; err != nil {
		return nil, err
	}
	rows := make([]stepRow, 0, len(raw))
	for _, r := range raw {
		ts, err := parseSQLiteTime(r.StepTS)
		if err != nil {
			continue // skip unparseable rows rather than fail the whole request
		}
		rows = append(rows, stepRow{DistinctID: r.DistinctID, StepTS: ts})
	}
	return rows, nil
}

func computeFunnelStaged(db *gorm.DB, steps []string, from, to time.Time) ([]funnelStepResult, error) {
	results := make([]funnelStepResult, len(steps))

	firstQuery := db.Table("events").
		Select("distinct_id, MIN(timestamp) as step_ts").
		Where("event_name = ? AND timestamp BETWEEN ? AND ?", steps[0], from, to).
		Group("distinct_id")

	firstRows, err := fetchStep(db, firstQuery)
	if err != nil {
		return nil, err
	}
	results[0] = funnelStepResult{Step: steps[0], Count: int64(len(firstRows))}

	if len(firstRows) == 0 {
		for i := 1; i < len(steps); i++ {
			results[i] = funnelStepResult{Step: steps[i], Count: 0}
		}
		return results, nil
	}

	anchor := make(map[string]time.Time, len(firstRows))
	prevTS := make(map[string]time.Time, len(firstRows))
	for _, row := range firstRows {
		anchor[row.DistinctID] = row.StepTS
		prevTS[row.DistinctID] = row.StepTS
	}

	for i := 1; i < len(steps); i++ {
		visitorIDs := make([]string, 0, len(prevTS))
		for id := range prevTS {
			visitorIDs = append(visitorIDs, id)
		}
		if len(visitorIDs) == 0 {
			results[i] = funnelStepResult{Step: steps[i], Count: 0}
			continue
		}

		nextQuery := db.Table("events").
			Select("distinct_id, MIN(timestamp) as step_ts").
			Where("event_name = ? AND distinct_id IN ?", steps[i], visitorIDs).
			Group("distinct_id")

		nextRows, err := fetchStep(db, nextQuery)
		if err != nil {
			return nil, err
		}

		newPrevTS := make(map[string]time.Time)
		for _, row := range nextRows {
			prior, ok := prevTS[row.DistinctID]
			if !ok || !row.StepTS.After(prior) {
				continue
			}
			if row.StepTS.Sub(anchor[row.DistinctID]) > funnelWindow {
				continue
			}
			newPrevTS[row.DistinctID] = row.StepTS
		}

		results[i] = funnelStepResult{Step: steps[i], Count: int64(len(newPrevTS))}
		prevTS = newPrevTS
	}

	return results, nil
}
