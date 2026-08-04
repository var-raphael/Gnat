package main

import (
	"flag"
	"log"
	"net/http"
	"strconv"

	"github.com/var-raphael/gnat/internal/config"
	"github.com/var-raphael/gnat/internal/ingest"
	"github.com/var-raphael/gnat/internal/query"
	"github.com/var-raphael/gnat/internal/storage"
)

func main() {
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

	log.Printf("gnat starting on :%d (db: %s)", cfg.Server.BindPort, cfg.Database.Driver)

	mux := http.NewServeMux()
  mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
  })
  mux.HandleFunc("/api/event", ingest.Handler(db, cfg.APIKey))
  mux.HandleFunc("/api/stats/pageviews", query.PageviewsHandler(db, cfg.APIKey))
  mux.HandleFunc("/tracker.js", func(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript")
	http.ServeFile(w, r, "web/static/tracker.js")
  })

	addr := ":" + strconv.Itoa(cfg.Server.BindPort)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
