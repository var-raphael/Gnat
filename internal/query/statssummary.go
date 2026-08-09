package query

import (
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"gorm.io/gorm"
)

const (
	liveWindow = 5 * time.Minute
	sessionGap = 30 * time.Minute

	// maxVisitLookback bounds how far back LiveVisitorsHandler queries
	// when computing how long a visitor has been continuously active.
	// That walk-back has no natural upper limit on its own (a visitor
	// heartbeating every 30s could in principle stay "active" for
	// hours), so this caps query cost at a generous but finite window
	// rather than scanning unbounded history. A visit longer than this
	// simply reports maxVisitLookback as its active time instead of the
	// true (longer) duration — an acceptable floor for a "how long has
	// this person been here" live-visitor display.
	maxVisitLookback = 3 * time.Hour
)

type statValue struct {
	Value      float64  `json:"value"`
	ValueSec   float64  `json:"value_seconds"`
	ValuePct   float64  `json:"value_pct"`
	DeltaPct   *float64 `json:"delta_pct,omitempty"`
	UniqueEvts int64    `json:"unique_events,omitempty"`
}

func withDelta(d float64) *float64 {
	return &d
}

type statsSummary struct {
	UniqueVisitorsToday  statValue `json:"unique_visitors_today"`
	PageviewsToday       statValue `json:"pageviews_today"`
	LiveVisitors         statValue `json:"live_visitors"`
	AvgTimeOnPageToday   statValue `json:"avg_time_on_page_today"`
	CustomEventsToday    statValue `json:"custom_events_today"`
	AvgTimeOnSiteToday   statValue `json:"avg_time_on_site_today"`
	BounceRateToday      statValue `json:"bounce_rate_today"`
	RetentionRate7d      statValue `json:"retention_rate_7d"`
	PagesPerSessionToday statValue `json:"pages_per_session_today"`
}

type visitTimestamp struct {
	DistinctID string
	Timestamp  time.Time
}

type session struct {
	pageviews int
	start     time.Time
	end       time.Time
	gaps      []time.Duration
}

// buildSessions groups a visitor's raw timestamps into sessions,
// splitting wherever the gap between consecutive events exceeds
// sessionGap.
func buildSessions(rows []visitTimestamp) []session {
	byVisitor := make(map[string][]time.Time)
	for _, r := range rows {
		byVisitor[r.DistinctID] = append(byVisitor[r.DistinctID], r.Timestamp)
	}

	var sessions []session
	for _, times := range byVisitor {
		sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })

		cur := session{pageviews: 1, start: times[0], end: times[0]}
		for i := 1; i < len(times); i++ {
			gap := times[i].Sub(times[i-1])
			if gap > sessionGap {
				sessions = append(sessions, cur)
				cur = session{pageviews: 1, start: times[i], end: times[i]}
				continue
			}
			cur.pageviews++
			cur.end = times[i]
			cur.gaps = append(cur.gaps, gap)
		}
		sessions = append(sessions, cur)
	}
	return sessions
}

func fetchPageviewTimestamps(db *gorm.DB, from, to time.Time) ([]visitTimestamp, error) {
	var rows []visitTimestamp
	err := db.Table("events").
		Select("distinct_id, timestamp").
		Where("event_name = ? AND timestamp BETWEEN ? AND ?", "pageview", from, to).
		Order("distinct_id, timestamp").
		Scan(&rows).Error
	return rows, err
}

func pctChange(current, previous float64) float64 {
	if previous == 0 {
		if current == 0 {
			return 0
		}
		return 100
	}
	return roundTo2((current - previous) / previous * 100)
}

// StatsSummaryHandler returns GET /api/stats/summary
// Always scoped to today vs yesterday, regardless of the dashboard's
// selected date range — these are always-current headline numbers.
func StatsSummaryHandler(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		now := time.Now().UTC()
		todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		todayEnd := todayStart.Add(24*time.Hour - time.Nanosecond)
		yesterdayStart := todayStart.AddDate(0, 0, -1)
		yesterdayEnd := todayStart.Add(-time.Nanosecond)

		result, err := computeSummary(db, now, todayStart, todayEnd, yesterdayStart, yesterdayEnd)
		if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

func computeSummary(db *gorm.DB, now, todayStart, todayEnd, yesterdayStart, yesterdayEnd time.Time) (statsSummary, error) {
	var result statsSummary

	uniqToday, err := countDistinct(db, "distinct_id", "", todayStart, todayEnd)
	if err != nil {
		return result, err
	}
	uniqYesterday, err := countDistinct(db, "distinct_id", "", yesterdayStart, yesterdayEnd)
	if err != nil {
		return result, err
	}
	result.UniqueVisitorsToday = statValue{Value: float64(uniqToday), DeltaPct: withDelta(pctChange(float64(uniqToday), float64(uniqYesterday)))}

	pvToday, err := countWhere(db, "event_name = ?", todayStart, todayEnd, "pageview")
	if err != nil {
		return result, err
	}
	pvYesterday, err := countWhere(db, "event_name = ?", yesterdayStart, yesterdayEnd, "pageview")
	if err != nil {
		return result, err
	}
	result.PageviewsToday = statValue{Value: float64(pvToday), DeltaPct: withDelta(pctChange(float64(pvToday), float64(pvYesterday)))}

	liveCount, err := countDistinct(db, "distinct_id", "", now.Add(-liveWindow), now)
	if err != nil {
		return result, err
	}
	result.LiveVisitors = statValue{Value: float64(liveCount)}

	ceToday, err := countWhere(db, "event_name NOT IN (?, ?)", todayStart, todayEnd, "pageview", "heartbeat")
	if err != nil {
		return result, err
	}
	ceYesterday, err := countWhere(db, "event_name NOT IN (?, ?)", yesterdayStart, yesterdayEnd, "pageview", "heartbeat")
	if err != nil {
		return result, err
	}
	uniqueEvtNames, err := countDistinctEventNames(db, todayStart, todayEnd)
	if err != nil {
		return result, err
	}
	result.CustomEventsToday = statValue{Value: float64(ceToday), DeltaPct: withDelta(pctChange(float64(ceToday), float64(ceYesterday))), UniqueEvts: uniqueEvtNames}

	todayRows, err := fetchPageviewTimestamps(db, todayStart, todayEnd)
	if err != nil {
		return result, err
	}
	yesterdayRows, err := fetchPageviewTimestamps(db, yesterdayStart, yesterdayEnd)
	if err != nil {
		return result, err
	}
	todaySessions := buildSessions(todayRows)
	yesterdaySessions := buildSessions(yesterdayRows)

	avgTimeOnSiteToday := avgSessionDuration(todaySessions)
	avgTimeOnSiteYesterday := avgSessionDuration(yesterdaySessions)
	result.AvgTimeOnSiteToday = statValue{ValueSec: avgTimeOnSiteToday, DeltaPct: withDelta(pctChange(avgTimeOnSiteToday, avgTimeOnSiteYesterday))}

	avgTimeOnPageToday := avgTimeOnPage(todaySessions)
	avgTimeOnPageYesterday := avgTimeOnPage(yesterdaySessions)
	result.AvgTimeOnPageToday = statValue{ValueSec: avgTimeOnPageToday, DeltaPct: withDelta(pctChange(avgTimeOnPageToday, avgTimeOnPageYesterday))}

	bounceToday := bounceRate(todaySessions)
	bounceYesterday := bounceRate(yesterdaySessions)
	result.BounceRateToday = statValue{ValuePct: bounceToday, DeltaPct: withDelta(pctChange(bounceToday, bounceYesterday))}

	ppsToday := pagesPerSession(todaySessions)
	ppsYesterday := pagesPerSession(yesterdaySessions)
	result.PagesPerSessionToday = statValue{Value: ppsToday, DeltaPct: withDelta(pctChange(ppsToday, ppsYesterday))}

	cohorts, err := computeRetention(db, todayStart.AddDate(0, 0, -30), todayEnd)
	if err != nil {
		return result, err
	}
	agg := aggregateCohorts(cohorts)
	var day7Pct float64
	for _, p := range agg.Points {
		if p.Label == "Day 7" {
			day7Pct = p.Pct
			break
		}
	}
	result.RetentionRate7d = statValue{ValuePct: day7Pct}

	return result, nil
}

func countDistinct(db *gorm.DB, column, extraWhere string, from, to time.Time) (int64, error) {
	var count int64
	q := db.Table("events").Where("timestamp BETWEEN ? AND ?", from, to)
	if extraWhere != "" {
		q = q.Where(extraWhere)
	}
	err := q.Select("COUNT(DISTINCT " + column + ")").Scan(&count).Error
	return count, err
}

func countWhere(db *gorm.DB, cond string, from, to time.Time, args ...interface{}) (int64, error) {
	var count int64
	err := db.Table("events").Where(cond, args...).Where("timestamp BETWEEN ? AND ?", from, to).Count(&count).Error
	return count, err
}

func countDistinctEventNames(db *gorm.DB, from, to time.Time) (int64, error) {
	var count int64
	err := db.Table("events").
		Where("event_name NOT IN (?, ?) AND timestamp BETWEEN ? AND ?", "pageview", "heartbeat", from, to).
		Select("COUNT(DISTINCT event_name)").Scan(&count).Error
	return count, err
}

func avgSessionDuration(sessions []session) float64 {
	if len(sessions) == 0 {
		return 0
	}
	var total float64
	for _, s := range sessions {
		total += s.end.Sub(s.start).Seconds()
	}
	return roundTo2(total / float64(len(sessions)))
}

func avgTimeOnPage(sessions []session) float64 {
	var total float64
	var count int
	for _, s := range sessions {
		for _, g := range s.gaps {
			total += g.Seconds()
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return roundTo2(total / float64(count))
}

func bounceRate(sessions []session) float64 {
	if len(sessions) == 0 {
		return 0
	}
	var bounced int
	for _, s := range sessions {
		if s.pageviews == 1 {
			bounced++
		}
	}
	return roundTo2(float64(bounced) / float64(len(sessions)) * 100)
}

func pagesPerSession(sessions []session) float64 {
	if len(sessions) == 0 {
		return 0
	}
	var total int
	for _, s := range sessions {
		total += s.pageviews
	}
	return roundTo2(float64(total) / float64(len(sessions)))
}
