// meta.go — read-side of the tiny import_meta key/value table the daily
// importer (webapp/sharepoint_import.py) stamps on every successful run.
// The app only ever reads it; absence (pre-rollout DBs, the offline fixture)
// is normal and must never error a page.
package db

import (
	"database/sql"
	"time"
)

// LastImport returns when the daily import last committed, parsed from the
// import_meta 'last_import' row (UTC RFC3339, written by sharepoint_import.py).
// ok=false when the table or row doesn't exist yet or the value is unparseable —
// callers simply omit the freshness display.
func LastImport(d *sql.DB) (time.Time, bool) {
	var v string
	if err := d.QueryRow(`SELECT value FROM import_meta WHERE key = 'last_import'`).Scan(&v); err != nil {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// LastImportMode returns the import_meta 'last_import_mode' row — how data last
// got in ("full"/"incremental" from the daily importer, "web-upload" from the
// console CSV page). Empty when absent (same tolerance as LastImport).
func LastImportMode(d *sql.DB) string {
	var v string
	if err := d.QueryRow(`SELECT value FROM import_meta WHERE key = 'last_import_mode'`).Scan(&v); err != nil {
		return ""
	}
	return v
}
