package handlers

// custody.go — add / remove an in-custody (GPS-off) period from the console
// record (any allowed officer, like the other extension CRUD; both audited in the
// db layer). The days inside a period are excluded from the client's GPS billing
// (record GPS card, Behind-on-GPS roster, and the past-due/show-cause letters);
// the period's "back on GPS" date is the reinstall day and IS billed.

import (
	"net/http"

	"pretrial-knoxc/internal/auth"
	"pretrial-knoxc/internal/db"
)

// AddCustodyPeriod records a custody span. POST /admin/custody/add
// (idn, start_date, end_date?, note?)
func (s *Server) AddCustodyPeriod(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	idn, back := profileBack(r)
	err := db.AddCustodyPeriod(s.DB, idn, r.FormValue("start_date"),
		r.FormValue("end_date"), r.FormValue("note"), auth.User(r))
	s.afterWrite(w, r, back, err, "Custody dates added — GPS billing updated.")
}

// DeleteCustodyPeriod removes a custody span. POST /admin/custody/delete (idn, id)
func (s *Server) DeleteCustodyPeriod(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	_, back := profileBack(r)
	s.afterWrite(w, r, back, db.DeleteCustodyPeriod(s.DB, formID(r), auth.User(r)), "Custody dates removed.")
}
