package ingest

import (
	"strings"

	"github.com/mssola/user_agent"
)

// parsedUA holds the fields we actually store, derived from a raw
// User-Agent string.
type parsedUA struct {
	Browser    string
	OS         string
	DeviceType string
}

// parseUserAgent extracts browser, OS, and a coarse device type from a
// raw User-Agent header. Returns zero-value fields (all empty strings)
// if ua is empty rather than guessing.
func parseUserAgent(ua string) parsedUA {
	if ua == "" {
		return parsedUA{}
	}

	client := user_agent.New(ua)

	browserName, _ := client.Browser()
	os := client.OS()

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

// isTablet does a simple substring check for common tablet indicators,
// since the underlying library's Mobile() does not distinguish tablets
// from phones on its own.
func isTablet(ua string) bool {
	lower := strings.ToLower(ua)
	return strings.Contains(lower, "ipad") ||
		(strings.Contains(lower, "android") && !strings.Contains(lower, "mobile"))
}
