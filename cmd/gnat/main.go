package main

import (
	"flag"
	"log"
	"net/http"
	"strconv"

	"github.com/joho/godotenv"

	"github.com/var-raphael/gnat/internal/config"
	"github.com/var-raphael/gnat/internal/geo"
	"github.com/var-raphael/gnat/internal/ingest"
	"github.com/var-raphael/gnat/internal/query"
	"github.com/var-raphael/gnat/internal/storage"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("no .env file found, using existing environment")
	}

	configPath := flag.String("config", "./gnat.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
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

	geoClient := geo.NewClient()

	log.Printf("gnat starting on :%d (db: %s)", cfg.Server.BindPort, cfg.Database.Driver)

	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	mux.HandleFunc("/api/event", ingest.Handler(db, cfg.APIKey, geoClient))
	mux.HandleFunc("/api/stats/pageviews", query.PageviewsHandler(db, cfg.APIKey))
	mux.HandleFunc("/api/stats/events", query.EventsHandler(db, cfg.APIKey))
	mux.HandleFunc("/api/stats/referrers", query.ReferrersHandler(db, cfg.APIKey))
	mux.HandleFunc("/api/stats/funnels", query.FunnelsHandler(db, cfg.APIKey))
	mux.HandleFunc("/api/stats/paths", query.PathsHandler(db, cfg.APIKey))
	mux.HandleFunc("/api/stats/retention", query.RetentionHandler(db, cfg.APIKey))
	mux.HandleFunc("/api/export", query.ExportHandler(db, cfg.APIKey))
	
	mux.HandleFunc("/tracker.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		http.ServeFile(w, r, "web/static/tracker.js")
	})
	
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

mux.HandleFunc("/mock-data.json", func(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	http.ServeFile(w, r, "web/static/mock-data.json")
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