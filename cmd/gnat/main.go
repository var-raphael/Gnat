package main

import (
	"log"
	"net/http"
	"strconv"

	"github.com/joho/godotenv"

	"github.com/var-raphael/gnat/internal/auth"
	"github.com/var-raphael/gnat/internal/config"
	"github.com/var-raphael/gnat/internal/geo"
	"github.com/var-raphael/gnat/internal/ingest"
	"github.com/var-raphael/gnat/internal/query"
	"github.com/var-raphael/gnat/internal/storage"
)

func main() {
	// Local dev convenience only: if a .env file exists, its values are
	// loaded into the process environment. Nothing else reads from a
	// file; every actual setting comes from config.Load() below, which
	// looks only at real environment variables.
	if err := godotenv.Load(); err != nil {
		log.Println("no .env file found, using existing environment")
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	db, err := storage.Open(cfg.Database)
	if err != nil {
		log.Fatalf("storage error: %v", err)
	}

	if err := storage.AutoMigrate(db); err != nil {
		log.Fatalf("migration error: %v", err)
	}

	if err := storage.SyncSites(db, cfg.Sites); err != nil {
		log.Fatalf("site sync error: %v", err)
	}

	geoClient := geo.NewClient()

	sessionStore := auth.NewSessionStore()
	dashAuth := auth.NewDashboardAuth(cfg.DashboardPassword, sessionStore, cfg.Server.PublicURL)

	log.Printf("gnat starting on :%d (db: %s, sites: %v)", cfg.Server.BindPort, cfg.Database.Driver, cfg.Sites)

	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// Ingest: protected by the ingest API key only. Never gated by the
	// dashboard session, this is a write path meant for websites/apps
	// sending events, not for a logged-in dashboard user.
	mux.HandleFunc("/api/event", ingest.Handler(db, cfg.APIKey, geoClient))

	// Dashboard auth endpoints: unauthenticated by definition, since
	// their whole job is to establish or tear down a session.
	mux.HandleFunc("/api/dashboard/login", dashAuth.LoginHandler())
	mux.HandleFunc("/api/dashboard/logout", dashAuth.LogoutHandler())
	mux.HandleFunc("/api/dashboard/session", dashAuth.SessionHandler())

	// Stats and export: read paths, gated by the dashboard session
	// cookie rather than the ingest API key. These have no legitimate
	// caller other than a logged-in dashboard.
	mux.HandleFunc("/api/stats/pageviews", dashAuth.RequireSession(query.PageviewsHandler(db)))
	mux.HandleFunc("/api/stats/events", dashAuth.RequireSession(query.EventsHandler(db)))
	mux.HandleFunc("/api/stats/referrers", dashAuth.RequireSession(query.ReferrersHandler(db)))
	mux.HandleFunc("/api/stats/funnels", dashAuth.RequireSession(query.FunnelResultsHandler(db)))
	mux.HandleFunc("/api/stats/funnels-adhoc", dashAuth.RequireSession(query.FunnelsHandler(db)))
	mux.HandleFunc("/api/stats/paths", dashAuth.RequireSession(query.PathsHandler(db)))
	mux.HandleFunc("/api/stats/retention", dashAuth.RequireSession(query.RetentionHandler(db)))
	mux.HandleFunc("/api/stats/countries", dashAuth.RequireSession(query.CountriesHandler(db)))
	mux.HandleFunc("/api/stats/devices", dashAuth.RequireSession(query.DevicesHandler(db)))
	mux.HandleFunc("/api/stats/browsers", dashAuth.RequireSession(query.BrowsersHandler(db)))
	mux.HandleFunc("/api/stats/pages", dashAuth.RequireSession(query.TopPagesHandler(db)))
	mux.HandleFunc("/api/stats/traffic-sources", dashAuth.RequireSession(query.TrafficSourcesHandler(db)))
	mux.HandleFunc("/api/stats/custom-events", dashAuth.RequireSession(query.CustomEventsHandler(db)))
	mux.HandleFunc("/api/stats/summary", dashAuth.RequireSession(query.StatsSummaryHandler(db)))
	mux.HandleFunc("/api/stats/live", dashAuth.RequireSession(query.LiveVisitorsHandler(db)))
	mux.HandleFunc("/api/export", dashAuth.RequireSession(query.ExportHandler(db)))
	mux.HandleFunc("/api/event-names", dashAuth.RequireSession(query.EventNamesHandler(db)))
	mux.HandleFunc("/api/funnels", dashAuth.RequireSession(query.FunnelDefsHandler(db)))
	mux.HandleFunc("/api/funnels/{id}", dashAuth.RequireSession(query.FunnelDefHandler(db)))

	mux.HandleFunc("/tracker.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		http.ServeFile(w, r, "web/static/tracker.js")
	})

	// The dashboard page and its static assets are served without a
	// session check: the page itself contains the login form, so it
	// has to be reachable before anyone has a session. The data this
	// page fetches (via /api/stats/* and /api/export above) is what's
	// actually gated.
	mux.HandleFunc("/dashboard", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		http.ServeFile(w, r, "web/templates/dashboard.html")
	})
	mux.HandleFunc("/dashboard.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		http.ServeFile(w, r, "web/static/dashboard.css")
	})
	mux.HandleFunc("/dashboard.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		http.ServeFile(w, r, "web/static/dashboard.js")
	})

	mux.HandleFunc("/country-tiers.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		http.ServeFile(w, r, "web/static/country-tiers.json")
	})

	mux.HandleFunc("/alpine.min.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		http.ServeFile(w, r, "web/static/alpine.min.js")
	})

	addr := ":" + strconv.Itoa(cfg.Server.BindPort)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
