// freshness.go — the "Data updated …" stamp shown at the top of every page
// (Alex: "for my own sanity, the last time the website was updated"). One
// source of truth over import_meta, exposed to ALL templates as the
// `dataFreshness` template func (bound in cmd/server/main.go after the Server
// exists), so each page chrome — console topbar, tracker shell, printable
// reports, admin pages — renders it without per-handler plumbing.
package handlers

import (
	"fmt"
	"time"

	"pretrial-knoxc/internal/compute"
	"pretrial-knoxc/internal/db"
)

// Freshness describes when data last entered the database.
type Freshness struct {
	OK    bool   // false = no import_meta yet (pre-rollout DB) — show nothing
	Label string // "Jun 10, 9:01 PM ET"
	Ago   string // "2h ago"
	Mode  string // "daily import" | "web CSV upload"
	Stale bool   // > 26h — the daily pipeline likely broke; show a warning
}

// DataFreshness reads the import_meta stamp (written by sharepoint_import.py
// and by web uploads via reconcile_import.py --stamp-meta). Two indexed
// single-row lookups; cheap enough to run per render.
func (s *Server) DataFreshness() Freshness {
	t, ok := db.LastImport(s.DB)
	if !ok {
		return Freshness{}
	}
	f := Freshness{
		OK:    true,
		Label: compute.InET(t).Format("Jan 2, 3:04 PM") + " ET",
		Ago:   agoStr(time.Since(t)),
		Stale: time.Since(t) > 26*time.Hour,
		Mode:  "daily import",
	}
	if db.LastImportMode(s.DB) == "web-upload" {
		f.Mode = "web CSV upload"
	}
	return f
}

// agoStr renders a duration as a compact "how long ago" suffix.
func agoStr(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
