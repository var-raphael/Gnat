package ingest

import (
	"net/http"
	"net/url"
	"strings"

	"gorm.io/gorm"

	"github.com/var-raphael/gnat/internal/storage"
)

// resolveSite maps a request's Origin header to a registered Site's ID.
// Matching is on hostname only — scheme and port are ignored, so
// "http://example.com:3000" and "https://example.com" both resolve to
// the same site, since the same property commonly runs on different
// ports across dev and production.
//
// Returns (0, false) if the Origin header is missing, unparseable, or
// simply doesn't match any configured site. Callers must silently drop
// the event in that case rather than error, so an attacker probing for
// valid origins gets no signal either way, and the same behavior covers
// the mundane case of a stray/misconfigured sender.
func resolveSite(db *gorm.DB, r *http.Request) (uint, bool) {
	host := originHost(r)
	if host == "" {
		return 0, false
	}

	var site storage.Site
	err := db.Where(storage.Site{Name: host}).First(&site).Error
	if err != nil {
		return 0, false
	}

	return site.ID, true
}

// originHost extracts just the hostname from the request's Origin
// header, e.g. "https://example.com:3000" -> "example.com". Falls back
// to treating the raw header value as a bare hostname if it doesn't
// parse as a URL, so a Origin sent without a scheme still has a chance
// to match.
func originHost(r *http.Request) string {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return ""
	}

	parsed, err := url.Parse(origin)
	if err != nil || parsed.Hostname() == "" {
		// Not a valid absolute URL; treat the whole value as a bare
		// hostname, trimming any stray port suffix by hand.
		host := origin
		if i := strings.Index(host, ":"); i != -1 {
			host = host[:i]
		}
		return strings.ToLower(host)
	}

	return strings.ToLower(parsed.Hostname())
}
