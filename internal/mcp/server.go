package mcp

import (
	"net/http"

	"gorm.io/gorm"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewServer builds an mcp.Server with all 5 gnat analytics tools bound
// to db. Called once per incoming SSE connection (see Handler below) —
// cheap to construct since it holds no state beyond the db reference,
// which is itself just a pooled connection handle.
func NewServer(db *gorm.DB) *sdk.Server {
	server := sdk.NewServer(&sdk.Implementation{Name: "gnat", Version: "v1.0.0"}, nil)

	sdk.AddTool(server, &sdk.Tool{
		Name: "get_traffic_overview",
		Description: "Overall site traffic for a date range: today-vs-yesterday " +
			"summary stats, daily pageviews, top pages, top referrers, traffic " +
			"source breakdown (direct/google/social/email/referral), and visitor " +
			"breakdowns by country, device, and browser. Use for general " +
			"'how's the site doing' questions.",
	}, getTrafficOverview(db))

	sdk.AddTool(server, &sdk.Tool{
		Name: "get_custom_events",
		Description: "Custom events tracked on the site (signups, purchases, " +
			"clicks, etc — excludes pageview/heartbeat) for a date range, with " +
			"counts and property value breakdowns for each event.",
	}, getCustomEvents(db))

	sdk.AddTool(server, &sdk.Tool{
		Name: "get_funnels",
		Description: "Every saved conversion funnel, with each step's visitor " +
			"count for a date range. Use for questions about conversion, " +
			"drop-off, or a specific named funnel.",
	}, getFunnels(db))

	sdk.AddTool(server, &sdk.Tool{
		Name: "get_retention",
		Description: "Visitor retention curve for a date range: what fraction " +
			"of visitors came back on each of several day-offsets (day 0, 1, 3, " +
			"7, 14, 21, 30) after their first visit, aggregated across every " +
			"cohort in range.",
	}, getRetention(db))

	sdk.AddTool(server, &sdk.Tool{
		Name: "get_live_visitors",
		Description: "Visitors currently active on the site right now — no " +
			"date range, always the current moment. Each entry includes their " +
			"current page, country, device, browser, and how long they've been " +
			"continuously active.",
	}, getLiveVisitors(db))

	return server
}

// Handler returns the http.Handler that serves the MCP SSE endpoint,
// already wrapped with token auth. db is fixed at startup (same pattern
// as every other handler in cmd/gnat/main.go); the token store is
// checked per-request since the token itself can be regenerated at any
// time from the dashboard without a server restart.
func Handler(db *gorm.DB, tokenMiddleware func(http.HandlerFunc) http.HandlerFunc) http.Handler {
	sseHandler := sdk.NewSSEHandler(func(r *http.Request) *sdk.Server {
		return NewServer(db)
	}, nil)

	return tokenMiddleware(sseHandler.ServeHTTP)
}
