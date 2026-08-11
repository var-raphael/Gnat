package query

import (
	"net/url"
	"strings"
)

// referrerHost normalizes a raw referrer value (as captured by
// document.referrer on the client, e.g. "https://www.facebook.com/"
// or "http://localhost:8080/some/path") down to a bare hostname
// (e.g. "facebook.com", "localhost:8080") suitable for display,
// grouping, and category matching (see socialDomains/emailDomains in
// trafficsources.go).
//
// "www." is stripped since it's not meaningful for grouping or
// category matching — facebook.com and www.facebook.com should be
// treated as the same source. Anything that isn't a valid absolute
// URL (blank, or already a bare host with no scheme) is returned as
// given, trimmed of surrounding whitespace.
func referrerHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		// Not a parseable absolute URL — return as-is rather than
		// dropping the value, so unexpected formats stay visible
		// instead of silently disappearing from results.
		return raw
	}

	host := parsed.Host
	host = strings.TrimPrefix(host, "www.")
	return host
}
