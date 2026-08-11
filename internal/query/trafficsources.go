package query

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/var-raphael/gnat/internal/dialect"
)

// TrafficSourcePoint is one traffic-source bucket's pageview count for
// a range (direct/google/social/email/referral).
type TrafficSourcePoint struct {
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

// categorize buckets a referrer host (already normalized via
// referrerHost — see GetTrafficSources) against the site's own host.
// Internal navigation (referrer host == site's own host) counts as
// direct, same as a blank referrer.
func categorize(referrerHostVal, siteHost string) string {
	if referrerHostVal == "" || referrerHostVal == siteHost {
		return "direct"
	}
	if referrerHostVal == "google.com" || strings.HasPrefix(referrerHostVal, "google.") || strings.Contains(referrerHostVal, ".google.") {
		return "google"
	}
	if socialDomains[referrerHostVal] {
		return "social"
	}
	if emailDomains[referrerHostVal] {
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

		final, err := GetTrafficSources(db, from, to)
		if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(final)
	}
}

// GetTrafficSources buckets pageviews in the given range into
// direct/google/social/email/referral categories. Buckets with zero
// count are omitted.
func GetTrafficSources(db *gorm.DB, from, to time.Time) ([]TrafficSourcePoint, error) {
	referrerExpr := dialect.JSONExtract(db.Dialector.Name(), "properties", "referrer")

	var rows []struct {
		Referrer string
		SiteName string
		Count    int64
	}
	// COALESCE(..., '') for the same reason as GetTopReferrers: direct
	// traffic stores JSON null for referrer, which would otherwise
	// scan as SQL NULL into the non-pointer Referrer field and error
	// out — categorize() already expects "" to mean direct, it just
	// needs the row to actually reach it.
	err := db.Table("events").
		Select("COALESCE("+referrerExpr+", '')"+" as referrer, sites.name as site_name, count(*) as count").
		Joins("JOIN sites ON sites.id = events.site_id").
		Where("event_name = ? AND timestamp BETWEEN ? AND ?", "pageview", from, to).
		Group("referrer, sites.name").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	buckets := map[string]int64{"direct": 0, "google": 0, "social": 0, "email": 0, "referral": 0}
	for _, row := range rows {
		// Normalize the raw referrer URL to a bare host (same helper
		// GetTopReferrers uses) so it can actually match against
		// socialDomains/emailDomains and the site's own host — those
		// were previously compared against full raw URLs, which
		// could never match, letting internal pages and social/email
		// referrers alike fall through to "referral".
		host := referrerHost(row.Referrer)
		cat := categorize(host, row.SiteName)
		buckets[cat] += row.Count
	}

	results := []TrafficSourcePoint{
		{Label: "Referral", Category: "referral", Count: buckets["referral"], Color: "#2dd4bf"},
		{Label: "Direct", Category: "direct", Count: buckets["direct"], Color: "#7c8f92"},
		{Label: "Google", Category: "google", Count: buckets["google"], Color: "#c9776a"},
		{Label: "Social", Category: "social", Count: buckets["social"], Color: "#5aa9a3"},
		{Label: "Email", Category: "email", Count: buckets["email"], Color: "#e8c07d"},
	}

	final := make([]TrafficSourcePoint, 0, len(results))
	for _, r := range results {
		if r.Count > 0 {
			final = append(final, r)
		}
	}
	return final, nil
}
