package query

import (
	"encoding/json"
	"net/http"
	"strings"

	"gorm.io/gorm"

	"github.com/var-raphael/gnat/internal/dialect"
)

type trafficSourcePoint struct {
	Label    string `json:"label"`
	Category string `json:"category"`
	Count    int64  `json:"count"`
	Color    string `json:"color"`
}

var socialDomains = map[string]bool{
	"facebook.com": true, "twitter.com": true, "x.com": true,
	"instagram.com": true, "linkedin.com": true, "tiktok.com": true,
	"pinterest.com": true, "reddit.com": true, "youtube.com": true,
	"threads.net": true, "snapchat.com": true,
}

var emailDomains = map[string]bool{
	"mail.google.com": true, "outlook.com": true,
	"outlook.live.com": true, "mail.yahoo.com": true,
}

func categorize(referrer, siteName string) string {
	if referrer == "" || referrer == siteName {
		return "direct"
	}
	if referrer == "google.com" || strings.HasPrefix(referrer, "google.") || strings.Contains(referrer, ".google.") {
		return "google"
	}
	if socialDomains[referrer] {
		return "social"
	}
	if emailDomains[referrer] {
		return "email"
	}
	return "referral"
}

// TrafficSourcesHandler returns GET /api/stats/traffic-sources?from=...&to=...
// Categorization happens in Go since it isn't expressible as portable SQL.
func TrafficSourcesHandler(db *gorm.DB) http.HandlerFunc {
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

		referrerExpr := dialect.JSONExtract(db.Dialector.Name(), "properties", "referrer")

		var rows []struct {
			Referrer string
			SiteName string
			Count    int64
		}
		err = db.Table("events").
			Select(referrerExpr+" as referrer, sites.name as site_name, count(*) as count").
			Joins("JOIN sites ON sites.id = events.site_id").
			Where("event_name = ? AND timestamp BETWEEN ? AND ?", "pageview", from, to).
			Group("referrer, sites.name").
			Scan(&rows).Error
		if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}

		buckets := map[string]int64{"direct": 0, "google": 0, "social": 0, "email": 0, "referral": 0}
		for _, row := range rows {
			cat := categorize(row.Referrer, row.SiteName)
			buckets[cat] += row.Count
		}

		results := []trafficSourcePoint{
			{Label: "Referral", Category: "referral", Count: buckets["referral"], Color: "#2dd4bf"},
			{Label: "Direct", Category: "direct", Count: buckets["direct"], Color: "#7c8f92"},
			{Label: "Google", Category: "google", Count: buckets["google"], Color: "#c9776a"},
			{Label: "Social", Category: "social", Count: buckets["social"], Color: "#5aa9a3"},
			{Label: "Email", Category: "email", Count: buckets["email"], Color: "#e8c07d"},
		}

		final := make([]trafficSourcePoint, 0, len(results))
		for _, r := range results {
			if r.Count > 0 {
				final = append(final, r)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(final)
	}
}
