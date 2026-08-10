// Package mcp exposes gnat's analytics data to AI agents over the Model
// Context Protocol, using the official github.com/modelcontextprotocol/go-sdk.
//
// Design: a handful of broad tools (not one per dashboard card, not one
// mega-tool) grouped by the kind of question a person actually asks in
// one breath — "how's traffic looking" wants the overview bundle, "how's
// the funnel doing" wants funnel data alone, etc. Each tool that isn't
// live-visitors takes an arbitrary from/to date range and hands back
// everything for that range; the agent decides how to use it, per the
// product decision to let the model reason over the full picture rather
// than pre-deciding what's relevant.
//
// Every tool is a thin wrapper over the same query.Get*/query.Compute*
// functions the dashboard's HTTP handlers call — same data, same
// semantics, no separate code path to drift out of sync.
package mcp

import (
	"context"
	"time"

	"gorm.io/gorm"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/var-raphael/gnat/internal/query"
)

// dateRangeInput is embedded by every tool that operates over a range.
// from/to are optional YYYY-MM-DD strings; omitting both defaults to the
// last 7 days, matching query.ParseDateRange's default — the same
// default the dashboard itself uses.
type dateRangeInput struct {
	From string `json:"from,omitempty" jsonschema:"start date, YYYY-MM-DD, inclusive. Omit for the last 7 days."`
	To   string `json:"to,omitempty" jsonschema:"end date, YYYY-MM-DD, inclusive. Omit to default to today."`
}

func (d dateRangeInput) parse() (time.Time, time.Time, error) {
	return query.ParseDateRange(d.From, d.To)
}

// ---- get_traffic_overview ------------------------------------------------

type trafficOverviewOutput struct {
	Summary         query.StatsSummary          `json:"summary" jsonschema:"today-vs-yesterday headline numbers: unique visitors, pageviews, bounce rate, etc. Always today vs yesterday regardless of the requested range."`
	PageviewsByDay  []query.PageviewPoint        `json:"pageviews_by_day" jsonschema:"daily pageview counts across the requested range"`
	TopPages        []query.PagePoint            `json:"top_pages" jsonschema:"most-viewed paths in the requested range"`
	TopReferrers    []query.ReferrerPoint        `json:"top_referrers" jsonschema:"referring domains, excluding direct traffic"`
	TrafficSources  []query.TrafficSourcePoint   `json:"traffic_sources" jsonschema:"traffic bucketed into direct/google/social/email/referral"`
	Countries       []query.CountryPoint         `json:"countries" jsonschema:"visitor breakdown by country code"`
	Devices         []query.DevicePoint          `json:"devices" jsonschema:"visitor breakdown by device type"`
	Browsers        []query.BrowserPoint         `json:"browsers" jsonschema:"visitor breakdown by browser"`
}

func getTrafficOverview(db *gorm.DB) sdk.ToolHandlerFor[dateRangeInput, trafficOverviewOutput] {
	return func(ctx context.Context, req *sdk.CallToolRequest, in dateRangeInput) (*sdk.CallToolResult, trafficOverviewOutput, error) {
		from, to, err := in.parse()
		if err != nil {
			return nil, trafficOverviewOutput{}, err
		}

		summary, err := query.ComputeSummary(db, time.Now().UTC())
		if err != nil {
			return nil, trafficOverviewOutput{}, err
		}
		pageviews, err := query.GetPageviewsOverTime(db, from, to)
		if err != nil {
			return nil, trafficOverviewOutput{}, err
		}
		pages, err := query.GetTopPages(db, from, to)
		if err != nil {
			return nil, trafficOverviewOutput{}, err
		}
		referrers, err := query.GetTopReferrers(db, from, to)
		if err != nil {
			return nil, trafficOverviewOutput{}, err
		}
		sources, err := query.GetTrafficSources(db, from, to)
		if err != nil {
			return nil, trafficOverviewOutput{}, err
		}
		countries, err := query.GetCountries(db, from, to)
		if err != nil {
			return nil, trafficOverviewOutput{}, err
		}
		devices, err := query.GetDevices(db, from, to)
		if err != nil {
			return nil, trafficOverviewOutput{}, err
		}
		browsers, err := query.GetBrowsers(db, from, to)
		if err != nil {
			return nil, trafficOverviewOutput{}, err
		}

		return nil, trafficOverviewOutput{
			Summary:        summary,
			PageviewsByDay: pageviews,
			TopPages:       pages,
			TopReferrers:   referrers,
			TrafficSources: sources,
			Countries:      countries,
			Devices:        devices,
			Browsers:       browsers,
		}, nil
	}
}

// ---- get_custom_events ---------------------------------------------------

type customEventsOutput struct {
	Events []query.CustomEventPoint `json:"events" jsonschema:"custom events (excludes pageview/heartbeat) with counts and property breakdowns, in the requested range"`
}

func getCustomEvents(db *gorm.DB) sdk.ToolHandlerFor[dateRangeInput, customEventsOutput] {
	return func(ctx context.Context, req *sdk.CallToolRequest, in dateRangeInput) (*sdk.CallToolResult, customEventsOutput, error) {
		from, to, err := in.parse()
		if err != nil {
			return nil, customEventsOutput{}, err
		}
		events, err := query.GetCustomEvents(db, from, to)
		if err != nil {
			return nil, customEventsOutput{}, err
		}
		return nil, customEventsOutput{Events: events}, nil
	}
}

// ---- get_funnels ----------------------------------------------------------

type funnelsOutput struct {
	Funnels []query.ComputedFunnel `json:"funnels" jsonschema:"every saved funnel, with each step's count for the requested range"`
}

func getFunnels(db *gorm.DB) sdk.ToolHandlerFor[dateRangeInput, funnelsOutput] {
	return func(ctx context.Context, req *sdk.CallToolRequest, in dateRangeInput) (*sdk.CallToolResult, funnelsOutput, error) {
		from, to, err := in.parse()
		if err != nil {
			return nil, funnelsOutput{}, err
		}
		funnels, err := query.GetFunnelResults(db, from, to)
		if err != nil {
			return nil, funnelsOutput{}, err
		}
		return nil, funnelsOutput{Funnels: funnels}, nil
	}
}

// ---- get_retention ---------------------------------------------------------

func getRetention(db *gorm.DB) sdk.ToolHandlerFor[dateRangeInput, query.RetentionResponse] {
	return func(ctx context.Context, req *sdk.CallToolRequest, in dateRangeInput) (*sdk.CallToolResult, query.RetentionResponse, error) {
		from, to, err := in.parse()
		if err != nil {
			return nil, query.RetentionResponse{}, err
		}
		result, err := query.GetRetention(db, from, to)
		if err != nil {
			return nil, query.RetentionResponse{}, err
		}
		return nil, result, nil
	}
}

// ---- get_live_visitors ------------------------------------------------------

// liveVisitorsInput is intentionally empty: "live" always means right
// now, same as the dashboard's live-visitors card, so there's no date
// range to request.
type liveVisitorsInput struct{}

type liveVisitorsOutput struct {
	Visitors []query.LiveVisitorPoint `json:"visitors" jsonschema:"visitors currently active on the site, right now"`
}

func getLiveVisitors(db *gorm.DB) sdk.ToolHandlerFor[liveVisitorsInput, liveVisitorsOutput] {
	return func(ctx context.Context, req *sdk.CallToolRequest, in liveVisitorsInput) (*sdk.CallToolResult, liveVisitorsOutput, error) {
		visitors, err := query.GetLiveVisitors(db)
		if err != nil {
			return nil, liveVisitorsOutput{}, err
		}
		return nil, liveVisitorsOutput{Visitors: visitors}, nil
	}
}
