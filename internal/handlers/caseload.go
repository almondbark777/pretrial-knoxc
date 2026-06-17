package handlers

import (
	"net/http"
	"strings"

	"pretrial-knoxc/internal/db"
)

// SaveCaseload persists the A–Z caseload map from the admin grid. Supervisor-gated
// (assigning caseload is a supervisor capability); CSRF is enforced by the /admin
// route group. The grid posts one `cell` field per checked box, value
// "<LETTER>|<Officer>"; the server is authoritative and dedups to one owner per
// letter (last value wins), so it's correct even if the client-side single-select
// JS is bypassed.
// POST /admin/caseload
func (s *Server) SaveCaseload(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireSupervisor(w, r)
	if !ok {
		return
	}
	_ = r.ParseForm()
	assignments := map[string]string{}
	for _, cell := range r.Form["cell"] {
		letter, officer, found := strings.Cut(cell, "|")
		letter = strings.ToUpper(strings.TrimSpace(letter))
		officer = strings.TrimSpace(officer)
		if !found || letter == "" || officer == "" {
			continue
		}
		assignments[letter] = officer // last write wins per letter
	}
	if err := db.SetCaseloadAssignments(s.DB, assignments, user); err != nil {
		redirectMsg(w, r, "/console/admin", "Could not save caseload: "+err.Error())
		return
	}
	redirectMsg(w, r, safeNext(r, "/console/admin"),
		"Caseload assignments saved — new referrals auto-assign by last name.")
}
