package ingest

import (
	"net/http"
	"net/url"
	"strings"

	"gorm.io/gorm"

	"github.com/var-raphael/gnat/internal/storage"
)


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


func originHost(r *http.Request) string {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return ""
	}

	parsed, err := url.Parse(origin)
	if err != nil || parsed.Hostname() == "" {

		host := origin
		if i := strings.Index(host, ":"); i != -1 {
			host = host[:i]
		}
		return strings.ToLower(host)
	}

	return strings.ToLower(parsed.Hostname())
}
