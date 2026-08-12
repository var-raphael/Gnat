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

// hourlyRetentionCheckpoints are the hours-since-first-seen offsets
// used when from/to collapse to a single calendar day — a "Day 7"
// scale is meaningless within one day, so retention instead reports
// whether someone returned within the same hour, an hour later, etc.
var hourlyRetentionCheckpoints = []int{0, 1, 2, 3, 6, 12}

type cohortResult struct {
	CohortDate string             `json:"cohort_date"` // YYYY-MM-DD, or YYYY-MM-DD HH for hourly cohorts
	Size       int64              `json:"size"`         // visitors in this cohort
	Retention  map[string]float64 `json:"retention"`    // "day_0"/"hour_0", etc -> percentage
	Active     map[string]int64   `json:"-"`             // same keys as Retention, but the raw active-visitor count instead of a percentage
}

// firstSeenRow is one visitor's first-ever event, raw aggregate text
// (see parseAggregateTime in funnels.go for why this can't be scanned
// straight into time.Time).
type firstSeenRow struct {
	DistinctID string
	FirstSeen  string
}

// RetentionPoint is one checkpoint (e.g. "Day 7", or "Hour 3" in
// hourly mode) in a retention curve. Active/CohortSize are the raw
// visitor counts behind Pct, so the UI can show "12 of 18 visitors"
// alongside the percentage rather than the percentage alone.
type RetentionPoint struct {
	Label      string  `json:"label"`
	Pct        float64 `json:"pct"`
	Active     int64   `json:"active"`
	CohortSize int64   `json:"cohort_size"`
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
// cohort whose first-seen falls within from/to.
//
// When from/to collapse to a single calendar day, cohorts are grouped
// by hour of first-seen and checkpoints are hour-offsets
// (hourlyRetentionCheckpoints) rather than day-offsets — a "Day 7"
// axis doesn't mean anything within one day. Multi-day ranges keep
// the normal day-based cohorts/checkpoints.
func GetRetention(db *gorm.DB, from, to time.Time) (RetentionResponse, error) {
	if isSameCalendarDay(from, to) {
		cohorts, err := computeHourlyRetention(db, from, to)
		if err != nil {
			return RetentionResponse{}, err
		}
		return aggregateHourlyCohorts(cohorts), nil
	}

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
		var activeTotal int64
		for _, c := range cohorts {
			activeTotal += c.Active[key]
		}
		pct := 0.0
		if totalSize > 0 {
			pct = roundTo2(float64(activeTotal) / float64(totalSize) * 100)
		}
		points = append(points, RetentionPoint{
			Label:      "Day " + itoa(checkpoint),
			Pct:        pct,
			Active:     activeTotal,
			CohortSize: totalSize,
		})
	}

	return RetentionResponse{CohortSize: totalSize, Unit: "day", Points: points}
}

// aggregateHourlyCohorts is aggregateCohorts' hourly-mode counterpart:
// same weighted-average logic, over hourlyRetentionCheckpoints and
// hour-keyed cohorts instead of day-keyed ones.
func aggregateHourlyCohorts(cohorts []cohortResult) RetentionResponse {
	var totalSize int64
	for _, c := range cohorts {
		totalSize += c.Size
	}

	points := make([]RetentionPoint, 0, len(hourlyRetentionCheckpoints))
	for _, checkpoint := range hourlyRetentionCheckpoints {
		key := hourCheckpointKey(checkpoint)
		var activeTotal int64
		for _, c := range cohorts {
			activeTotal += c.Active[key]
		}
		pct := 0.0
		if totalSize > 0 {
			pct = roundTo2(float64(activeTotal) / float64(totalSize) * 100)
		}
		points = append(points, RetentionPoint{
			Label:      "Hour " + itoa(checkpoint),
			Pct:        pct,
			Active:     activeTotal,
			CohortSize: totalSize,
		})
	}

	return RetentionResponse{CohortSize: totalSize, Unit: "hour", Points: points}
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
		active := make(map[string]int64, len(retentionCheckpoints))

		for _, checkpoint := range retentionCheckpoints {
			activeCount, err := countActiveAtCheckpoint(db, visitorIDs, firstSeenByVisitor, checkpoint)
			if err != nil {
				return nil, err
			}
			pct := 0.0
			if len(visitorIDs) > 0 {
				pct = float64(activeCount) / float64(len(visitorIDs)) * 100
			}
			key := checkpointKey(checkpoint)
			retention[key] = roundTo2(pct)
			active[key] = activeCount
		}

		results = append(results, cohortResult{
			CohortDate: day,
			Size:       int64(len(visitorIDs)),
			Retention:  retention,
			Active:     active,
		})
	}

	return results, nil
}

// computeHourlyRetention is computeRetention's hourly-mode
// counterpart: cohorts are grouped by hour of first-seen within the
// single selected day (rather than by calendar day across the whole
// range), and each checkpoint asks "were they active again N hours
// after their first pageview that day" instead of N days after.
func computeHourlyRetention(db *gorm.DB, from, to time.Time) ([]cohortResult, error) {
	var firstSeenRaw []firstSeenRow
	err := db.Table("events").
		Select("distinct_id, MIN(timestamp) as first_seen").
		Group("distinct_id").
		Scan(&firstSeenRaw).Error
	if err != nil {
		return nil, err
	}

	cohorts := make(map[string][]string) // cohort hour ("HH") -> []distinct_id
	firstSeenByVisitor := make(map[string]time.Time)

	for _, row := range firstSeenRaw {
		ts, err := parseAggregateTime(row.FirstSeen)
		if err != nil {
			continue
		}
		if ts.Before(from) || ts.After(to) {
			continue
		}
		hour := ts.Format("2006-01-02 15")
		cohorts[hour] = append(cohorts[hour], row.DistinctID)
		firstSeenByVisitor[row.DistinctID] = ts
	}

	results := make([]cohortResult, 0, len(cohorts))

	for hour, visitorIDs := range cohorts {
		retention := make(map[string]float64, len(hourlyRetentionCheckpoints))
		active := make(map[string]int64, len(hourlyRetentionCheckpoints))

		for _, checkpoint := range hourlyRetentionCheckpoints {
			activeCount, err := countActiveAtHourCheckpoint(db, visitorIDs, firstSeenByVisitor, checkpoint)
			if err != nil {
				return nil, err
			}
			pct := 0.0
			if len(visitorIDs) > 0 {
				pct = float64(activeCount) / float64(len(visitorIDs)) * 100
			}
			key := hourCheckpointKey(checkpoint)
			retention[key] = roundTo2(pct)
			active[key] = activeCount
		}

		results = append(results, cohortResult{
			CohortDate: hour,
			Size:       int64(len(visitorIDs)),
			Retention:  retention,
			Active:     active,
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

// countActiveAtHourCheckpoint is countActiveAtCheckpoint's hourly
// counterpart: counts visitors active within the hour that is
// checkpoint hours after their own first-seen hour (rather than the
// calendar day that is checkpoint days later).
func countActiveAtHourCheckpoint(db *gorm.DB, visitorIDs []string, firstSeenByVisitor map[string]time.Time, checkpoint int) (int64, error) {
	if len(visitorIDs) == 0 {
		return 0, nil
	}

	anyVisitor := visitorIDs[0]
	targetHourStart := firstSeenByVisitor[anyVisitor].Add(time.Duration(checkpoint) * time.Hour).Truncate(time.Hour)
	targetHourEnd := targetHourStart.Add(time.Hour)

	var count int64
	err := db.Table("events").
		Select("COUNT(DISTINCT distinct_id)").
		Where("distinct_id IN ? AND timestamp >= ? AND timestamp < ?", visitorIDs, targetHourStart, targetHourEnd).
		Scan(&count).Error

	return count, err
}

func checkpointKey(day int) string {
	if day == 0 {
		return "day_0"
	}
	return "day_" + itoa(day)
}

func hourCheckpointKey(hour int) string {
	if hour == 0 {
		return "hour_0"
	}
	return "hour_" + itoa(hour)
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
