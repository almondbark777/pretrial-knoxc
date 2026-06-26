// flags_bulletins.go wires the two officer features that ride alongside QR
// check-in: manual client flags (a prominent alert on a client — banner on the
// record, chip on the roster) and the office-wide notice board shown on the
// check-in page. Both are app-owned, CSRF-guarded, audited writes; reads live on
// the record page (flags) and the check-in queue (bulletins).
package handlers

import (
	"net/http"
	"strings"

	"pretrial-knoxc/internal/auth"
	"pretrial-knoxc/internal/db"
)

// ── client flags ─────────────────────────────────────────────────────────────

// AddClientFlag raises a manual alert on a client.
func (s *Server) AddClientFlag(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	idn := strings.TrimSpace(r.FormValue("idn"))
	severity := strings.TrimSpace(r.FormValue("severity"))
	reason := strings.TrimSpace(r.FormValue("reason"))
	err := db.AddClientFlag(s.DB, idn, severity, reason, auth.User(r))
	s.afterWrite(w, r, flagBack(r, idn), err, "Client flagged.")
}

// ClearClientFlag clears one active flag.
func (s *Server) ClearClientFlag(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	idn := strings.TrimSpace(r.FormValue("idn"))
	err := db.ClearClientFlag(s.DB, formID(r), auth.User(r))
	s.afterWrite(w, r, flagBack(r, idn), err, "Flag cleared.")
}

// flagBack redirects to the next= target (if a safe /console/ path) else the
// client's record.
func flagBack(r *http.Request, idn string) string {
	if n := strings.TrimSpace(r.FormValue("next")); strings.HasPrefix(n, "/console/") {
		return n
	}
	if idn != "" {
		return "/console/clients/" + idn
	}
	return "/console/clients"
}

// ── bulletin board ───────────────────────────────────────────────────────────

// AddBulletin posts a notice to the office board.
func (s *Server) AddBulletin(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	title := strings.TrimSpace(r.FormValue("title"))
	body := strings.TrimSpace(r.FormValue("body"))
	priority := strings.TrimSpace(r.FormValue("priority"))
	pinned := r.FormValue("pinned") != ""
	err := db.AddBulletin(s.DB, title, body, priority, pinned, auth.User(r))
	s.afterWrite(w, r, bulletinBack(r), err, "Notice posted.")
}

// RemoveBulletin takes a notice off the board.
func (s *Server) RemoveBulletin(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	err := db.RemoveBulletin(s.DB, formID(r), auth.User(r))
	s.afterWrite(w, r, bulletinBack(r), err, "Notice removed.")
}

// bulletinBack defaults to the check-in page (where the board lives).
func bulletinBack(r *http.Request) string {
	if n := strings.TrimSpace(r.FormValue("next")); strings.HasPrefix(n, "/console/") {
		return n
	}
	return "/console/checkins"
}
