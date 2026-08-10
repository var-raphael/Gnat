package query

import (
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"gorm.io/gorm"
)

// PropertyValueCount is one distinct value of a custom event property
// and how many times it occurred.
type PropertyValueCount struct {
	Value string `json:"value"`
	Count int64  `json:"count"`
}

// PropertyBreakdown is one custom event property's value distribution.
type PropertyBreakdown struct {
	Name      string               `json:"name"`
	Breakdown []PropertyValueCount `json:"breakdown"`
}

// CustomEventPoint is one custom event name's count and property
// breakdowns for a range.
type CustomEventPoint struct {
	EventName  string              `json:"event_name"`
	Count      int64               `json:"count"`
	Properties []PropertyBreakdown `json:"properties"`
}

// CustomEventsHandler returns GET /api/stats/custom-events?from=...&to=...
// Excludes pageview, which has its own dedicated cards. Property
// breakdowns are computed in Go since the set of property keys isn't
// known in advance and can't be expressed as one portable SQL query.
func CustomEventsHandler(db *gorm.DB) http.HandlerFunc {
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

		results, err := GetCustomEvents(db, from, to)
		if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(results)
	}
}

// GetCustomEvents returns custom events (everything except
// pageview/heartbeat) in the given range, with counts and per-property
// value breakdowns, sorted by count descending.
func GetCustomEvents(db *gorm.DB, from, to time.Time) ([]CustomEventPoint, error) {
	var rows []struct {
		EventName  string
		Properties string
	}
	err := db.Table("events").
		Select("event_name, properties").
		Where("event_name NOT IN (?, ?) AND timestamp BETWEEN ? AND ?", "pageview", "heartbeat", from, to).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	type propAgg struct {
		counts map[string]int64
		order  []string
	}
	type eventAgg struct {
		count int64
		props map[string]*propAgg
		order []string
	}
	events := make(map[string]*eventAgg)
	eventOrder := make([]string, 0)

	for _, row := range rows {
		ev, ok := events[row.EventName]
		if !ok {
			ev = &eventAgg{props: make(map[string]*propAgg)}
			events[row.EventName] = ev
			eventOrder = append(eventOrder, row.EventName)
		}
		ev.count++

		var props map[string]interface{}
		if err := json.Unmarshal([]byte(row.Properties), &props); err != nil {
			continue
		}
		for key, val := range props {
			strVal, ok := val.(string)
			if !ok || strVal == "" {
				continue
			}
			pa, ok := ev.props[key]
			if !ok {
				pa = &propAgg{counts: make(map[string]int64)}
				ev.props[key] = pa
				ev.order = append(ev.order, key)
			}
			if _, seen := pa.counts[strVal]; !seen {
				pa.order = append(pa.order, strVal)
			}
			pa.counts[strVal]++
		}
	}

	results := make([]CustomEventPoint, 0, len(eventOrder))
	for _, name := range eventOrder {
		ev := events[name]
		properties := make([]PropertyBreakdown, 0, len(ev.order))
		for _, key := range ev.order {
			pa := ev.props[key]
			breakdown := make([]PropertyValueCount, 0, len(pa.order))
			for _, val := range pa.order {
				breakdown = append(breakdown, PropertyValueCount{Value: val, Count: pa.counts[val]})
			}
			sort.Slice(breakdown, func(i, j int) bool { return breakdown[i].Count > breakdown[j].Count })
			properties = append(properties, PropertyBreakdown{Name: key, Breakdown: breakdown})
		}
		results = append(results, CustomEventPoint{EventName: name, Count: ev.count, Properties: properties})
	}

	sort.Slice(results, func(i, j int) bool { return results[i].Count > results[j].Count })

	return results, nil
}
