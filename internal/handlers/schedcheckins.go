package handlers

// schedcheckins.go — book / cancel a future check-in appointment from the
// console record (any allowed officer, like the other extension CRUD; both
// audited in the db layer). The booking surfaces on the record's Check-ins
// tab and on the dashboard's Today's Schedule the day it falls due.

import (
	"net/http"

	"pretrial-knoxc/internal/auth"
	"pretrial-knoxc/internal/db"
)

// AddScheduledCheckIn books a check-in. POST /admin/schedule/add
// (idn, scheduled_for, check_in_type?)
func (s *Server) AddScheduledCheckIn(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	idn, back := profileBack(r)
	err := db.AddScheduledCheckIn(s.DB, idn, r.FormValue("scheduled_for"),
		r.FormValue("check_in_type"), r.FormValue("officer"), auth.User(r))
	s.afterWrite(w, r, back, err, "Check-in scheduled.")
}

// DeleteScheduledCheckIn cancels a booking. POST /admin/schedule/delete (idn, id)
func (s *Server) DeleteScheduledCheckIn(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	_, back := profileBack(r)
	s.afterWrite(w, r, back, db.DeleteScheduledCheckIn(s.DB, formID(r), auth.User(r)), "Scheduled check-in canceled.")
}
