// pins.go — pin/unpin a client (per-officer starred list, CSRF-guarded under
// /admin, audited in the db layer). The record's ⋯ menu posts here; pinned
// clients surface as a quick list on the console dashboard.
package handlers

import (
	"net/http"

	"pretrial-knoxc/internal/auth"
	"pretrial-knoxc/internal/db"
)

// TogglePin pins an unpinned client and unpins a pinned one.
// POST /admin/pin/toggle (idn, next)
func (s *Server) TogglePin(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	idn, back := profileBack(r)
	pinned, err := db.TogglePin(s.DB, auth.User(r), idn)
	if err != nil {
		redirectMsg(w, r, back, "Failed: "+err.Error())
		return
	}
	msg := "Client pinned — they now show on your dashboard."
	if !pinned {
		msg = "Client unpinned."
	}
	redirectMsg(w, r, back, msg) // pins don't feed BuildClients — no cache clear needed
}
