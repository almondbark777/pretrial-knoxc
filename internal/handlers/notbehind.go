package handlers

// notbehind.go — the compliance roster's "Reviewed — not behind" action
// (problem report #12). Officer-level (a review judgment, not a money decision
// like a waiver), CSRF via the /admin/* POST guard, audited inside
// db.SetNotBehind/ClearNotBehind. The cache is cleared because the flag changes
// computed roster membership (BuildClients sets Client.NotBehind; behindRoster
// holds those people off).

import (
	"net/http"

	"pretrial-knoxc/internal/auth"
	"pretrial-knoxc/internal/db"
)

// SetNotBehind marks a client "reviewed — not behind". POST /admin/not-behind
// (idn, reason?)
func (s *Server) SetNotBehind(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	idn, back := profileBack(r)
	err := db.SetNotBehind(s.DB, idn, r.FormValue("reason"), auth.User(r))
	s.afterWrite(w, r, back, err, "Marked reviewed — not behind.")
}

// ClearNotBehind removes a not-behind hold. POST /admin/not-behind/clear (idn)
func (s *Server) ClearNotBehind(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	idn, back := profileBack(r)
	err := db.ClearNotBehind(s.DB, idn, auth.User(r))
	s.afterWrite(w, r, back, err, "Not-behind review cleared.")
}
