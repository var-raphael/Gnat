package query

import (
	"encoding/json"
	"net/http"
	"time"

	"gorm.io/gorm"
)

// retentionCheckpoints are the standard days-since-first-seen offsets
// most retention tools report. Day 0 is always 100% by definition
// (the day someone first appears, they were obviously active), included
// mainly for completeness/consistency in the response shape.
var retentionCheckpoints = []int{0, 1, 3, 7, 14, 21, 30}

type cohortResult struct {
	CohortDate string             `json:"cohort_date"` // YYYY-MM-DD
	Size       int64              `json:"size"`         // visitors in this cohort
	Retention  map[string]float64 `json:"retention"`    // "day_0", "day_1", etc -> percentage
}

// firstSeenRow is one visitor's first-ever event, raw aggregate text
// (see parseAggregateTime in funnels.go for why this can't be scanned
// straight into time.Time).
type firstSeenRow struct {
	DistinctID string
	FirstSeen  string
}

// RetentionPoint is one checkpoint (e.g. "Day 7") in a retention curve.
type RetentionPoint struct {
	Label string  `json:"label"`
	Pct   float64 `json:"pct"`
}

// RetentionResponse is a single retention curve aggregated across every
// cohort in a range.
type RetentionResponse struct {
	CohortSize int64            `json:"cohort_size"`
	Unit       string           `json:"unit"`
	Points     []RetentionPoint `json:"points"`
}

// RetentionHandler returns GET /api/stats/retention?from=...&to=...
// Aggregates every cohort in range into one curve, weighted by cohort
// size, since the dashboard shows a single retention line rather than
// per-cohort breakdowns.
func RetentionHandler(db *gorm.DB) http.HandlerFunc {
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

		result, err := GetRetention(db, from, to)
		if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

// GetRetention computes the retention curve aggregated across every
// cohort whose first-seen day falls within from/to.
func GetRetention(db *gorm.DB, from, to time.Time) (RetentionResponse, error) {
	cohorts, err := computeRetention(db, from, to)
	if err != nil {
		return RetentionResponse{}, err
	}
	return aggregateCohorts(cohorts), nil
}

// aggregateCohorts combines multiple cohorts into a single weighted
// retention curve: each checkpoint's pct is the total active count
// across all cohorts at that checkpoint, divided by total cohort size.
func aggregateCohorts(cohorts []cohortResult) RetentionResponse {
	var totalSize int64
	for _, c := range cohorts {
		totalSize += c.Size
	}

	points := make([]RetentionPoint, 0, len(retentionCheckpoints))
	for _, checkpoint := range retentionCheckpoints {
		key := checkpointKey(checkpoint)
		var activeTotal float64
		for _, c := range cohorts {
			activeTotal += c.Retention[key] / 100 * float64(c.Size)
		}
		pct := 0.0
		if totalSize > 0 {
			pct = roundTo2(activeTotal / float64(totalSize) * 100)
		}
		points = append(points, RetentionPoint{Label: "Day " + itoa(checkpoint), Pct: pct})
	}

	return RetentionResponse{CohortSize: totalSize, Unit: "day", Points: points}
}

func computeRetention(db *gorm.DB, from, to time.Time) ([]cohortResult, error) {
	// Step 1: every visitor's first-ever event, across all time, not just
	// the query range, since someone's first event could predate "from"
	// while they still had activity inside the range.
	var firstSeenRaw []firstSeenRow
	err := db.Table("events").
		Select("distinct_id, MIN(timestamp) as first_seen").
		Group("distinct_id").
		Scan(&firstSeenRaw).Error
	if err != nil {
		return nil, err
	}

	// Group visitors by cohort day (their first-seen calendar day),
	// keeping only cohorts whose first-seen day falls within from/to.
	cohorts := make(map[string][]string) // cohort day -> []distinct_id
	firstSeenByVisitor := make(map[string]time.Time)

	for _, row := range firstSeenRaw {
		ts, err := parseAggregateTime(row.FirstSeen)
		if err != nil {
			continue
		}
		if ts.Before(from) || ts.After(to) {
			continue
		}
		day := ts.Format("2006-01-02")
		cohorts[day] = append(cohorts[day], row.DistinctID)
		firstSeenByVisitor[row.DistinctID] = ts
	}

	results := make([]cohortResult, 0, len(cohorts))

	for day, visitorIDs := range cohorts {
		retention := make(map[string]float64, len(retentionCheckpoints))

		for _, checkpoint := range retentionCheckpoints {
			activeCount, err := countActiveAtCheckpoint(db, visitorIDs, firstSeenByVisitor, checkpoint)
			if err != nil {
				return nil, err
			}
			pct := 0.0
			if len(visitorIDs) > 0 {
				pct = float64(activeCount) / float64(len(visitorIDs)) * 100
			}
			retention[checkpointKey(checkpoint)] = roundTo2(pct)
		}

		results = append(results, cohortResult{
			CohortDate: day,
			Size:       int64(len(visitorIDs)),
			Retention:  retention,
		})
	}

	return results, nil
}

// countActiveAtCheckpoint counts how many of the given visitors have any
// event on the calendar day that is checkpoint days after their own
// first-seen day. Each visitor has a different target day since cohorts
// share a first-seen day but "day N" is relative to that shared day, so
// within one cohort this simplifies to a single shared target day.
func countActiveAtCheckpoint(db *gorm.DB, visitorIDs []string, firstSeenByVisitor map[string]time.Time, checkpoint int) (int64, error) {
	if len(visitorIDs) == 0 {
		return 0, nil
	}

	// All visitors in this call share the same cohort day (this is called
	// once per cohort), so the target day is the same for all of them.
	anyVisitor := visitorIDs[0]
	targetDayStart := firstSeenByVisitor[anyVisitor].AddDate(0, 0, checkpoint).Truncate(24 * time.Hour)
	targetDayEnd := targetDayStart.Add(24 * time.Hour)

	var count int64
	err := db.Table("events").
		Select("COUNT(DISTINCT distinct_id)").
		Where("distinct_id IN ? AND timestamp >= ? AND timestamp < ?", visitorIDs, targetDayStart, targetDayEnd).
		Scan(&count).Error

	return count, err
}

func checkpointKey(day int) string {
	if day == 0 {
		return "day_0"
	}
	return "day_" + itoa(day)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func roundTo2(f float64) float64 {
	return float64(int(f*100+0.5)) / 100
}
