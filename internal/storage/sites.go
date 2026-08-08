package storage

import "gorm.io/gorm"

// SyncSites ensures every domain in sites has a corresponding Site row,
// creating any that don't exist yet. It never deletes a Site: removing
// a domain from GNAT_SITES stops new events for it from being accepted
// (see ingest site resolution), but its historical data is left alone
// rather than silently destroyed by an env var edit.
//
// Safe to call on every startup — existing sites are left untouched.
func SyncSites(db *gorm.DB, sites []string) error {
	for _, domain := range sites {
		site := Site{Name: domain}
		if err := db.Where(Site{Name: domain}).FirstOrCreate(&site).Error; err != nil {
			return err
		}
	}
	return nil
}
