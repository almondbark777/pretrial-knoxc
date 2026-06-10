// drugscreens.go — HTTP handlers for the drug-screen log (officer CRUD,
// CSRF-guarded under /admin, audited in the db layer). Same Post/Redirect/Get
// pattern as the other extension CRUD in admin.go.
package handlers

import (
	"net/http"

	"pretrial-knoxc/internal/auth"
	"pretrial-knoxc/internal/db"
)

// AddDrugScreen records a screen. POST /admin/drugscreen/add
// (idn, screen_date, test_type, result, substances, notes)
func (s *Server) AddDrugScreen(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	idn, back := profileBack(r)
	err := db.AddDrugScreen(s.DB, idn, r.FormValue("screen_date"), r.FormValue("test_type"),
		r.FormValue("result"), r.FormValue("substances"), r.FormValue("notes"), auth.User(r))
	s.afterWrite(w, r, back, err, "Drug screen recorded.")
}

// DeleteDrugScreen removes a screen row. POST /admin/drugscreen/delete (idn, id)
func (s *Server) DeleteDrugScreen(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	_, back := profileBack(r)
	s.afterWrite(w, r, back, db.DeleteDrugScreen(s.DB, formID(r), auth.User(r)), "Drug screen deleted.")
}
