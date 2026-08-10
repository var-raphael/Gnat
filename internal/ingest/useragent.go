package ingest

import (
	"strings"

	"github.com/mssola/user_agent"
)


type parsedUA struct {
	Browser    string
	OS         string
	DeviceType string
}


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

func isTablet(ua string) bool {
	lower := strings.ToLower(ua)
	return strings.Contains(lower, "ipad") ||
		(strings.Contains(lower, "android") && !strings.Contains(lower, "mobile"))
}
