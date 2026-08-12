package ingest

import (
	"regexp"
	"strings"

	"github.com/mssola/user_agent"
)

type parsedUA struct {
	Browser    string
	OS         string
	DeviceType string
}

// chromeVersionRe pulls the Chrome/X.X.X.X token out of a raw UA
// string — present in WebView UAs even though mssola/user_agent's
// own Browser() parser mislabels them (see webViewBrowser below).
var chromeVersionRe = regexp.MustCompile(`Chrome/[\d.]+`)

func parseUserAgent(ua string) parsedUA {
	if ua == "" {
		return parsedUA{}
	}

	client := user_agent.New(ua)

	browserName, _ := client.Browser()
	os := client.OS()

	// mssola/user_agent has a known bug (github.com/mssola/user_agent
	// issue #80): any UA containing the "; wv" WebView marker gets
	// Browser() = "Android" instead of the real browser engine. This
	// hits every Android WebView-based context — installed PWAs, and
	// in-app browsers like Facebook/Instagram's — not just one case.
	// Recover the real engine from the raw UA instead of trusting the
	// library's parse when this marker is present.
	if isWebView(ua) {
		browserName = webViewBrowser(ua)
	}

	deviceType := "desktop"
	switch {
	case client.Bot():
		deviceType = "bot"
	case isTablet(ua):
		deviceType = "tablet"
	case client.Mobile():
		deviceType = "mobile"
	}

	return parsedUA{
		Browser:    browserName,
		OS:         os,
		DeviceType: deviceType,
	}
}

// isWebView reports whether a UA string carries Android's WebView
// marker. Per Android's own documentation, this token ships in
// WebView UAs (installed PWAs, TWAs, and in-app browsers like
// Facebook/Instagram's) unless the host app has explicitly
// overridden its user agent string.
func isWebView(ua string) bool {
	return strings.Contains(ua, "; wv)") || strings.Contains(ua, "; wv;")
}

// webViewBrowser recovers a real browser label from a WebView UA
// string, labeled distinctly from a standalone browser since a
// WebView is a more restricted embedded context (no address bar,
// often no persistent login state) even when it's built on the same
// rendering engine — see isWebView's doc comment for why this
// matters more broadly than just PWAs.
func webViewBrowser(ua string) string {
	if match := chromeVersionRe.FindString(ua); match != "" {
		return "Chrome (WebView)"
	}
	return "Android WebView"
}

func isTablet(ua string) bool {
	lower := strings.ToLower(ua)
	return strings.Contains(lower, "ipad") ||
		(strings.Contains(lower, "android") && !strings.Contains(lower, "mobile"))
}
