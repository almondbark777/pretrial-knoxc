package handlers

// waivers.go — the console record's "Waive GPS fees" action. Gated like field
// overrides (a money decision → requireSupervisor; CSRF via the /admin/* POST
// guard), audited inside db.SetFeeWaiver/ClearFeeWaiver. The cache is cleared
// because a waiver changes computed view state (the gp_notes splice in
// BuildClients / the tracker feed).

import (
	"net/http"
	"net/url"
	"strings"

	"pretrial-knoxc/internal/db"
)

// SetFeeWaiver grants a GPS fee waiver. POST /admin/waiver (idn, reason?)
func (s *Server) SetFeeWaiver(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireSupervisor(w, r)
	if !ok {
		return
	}
	_ = r.ParseForm()
	idn := strings.TrimSpace(r.FormValue("idn"))
	back := safeNext(r, "/console/clients/"+url.PathEscape(idn))
	if err := db.SetFeeWaiver(s.DB, idn, r.FormValue("reason"), user); err != nil {
		redirectMsg(w, r, back, "Waive fees failed: "+err.Error())
		return
	}
	s.clearCache()
	redirectMsg(w, r, back, "GPS fees marked waived.")
}

// ClearFeeWaiver removes an app fee waiver. POST /admin/waiver/clear (idn)
func (s *Server) ClearFeeWaiver(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireSupervisor(w, r)
	if !ok {
		return
	}
	_ = r.ParseForm()
	idn := strings.TrimSpace(r.FormValue("idn"))
	back := safeNext(r, "/console/clients/"+url.PathEscape(idn))
	if err := db.ClearFeeWaiver(s.DB, idn, user); err != nil {
		redirectMsg(w, r, back, "Remove waiver failed: "+err.Error())
		return
	}
	s.clearCache()
	redirectMsg(w, r, back, "Fee waiver removed.")
}
