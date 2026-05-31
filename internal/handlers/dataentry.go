package handlers

import (
	"net/http"
	"net/url"
	"strings"

	"pretrial-knoxc/internal/auth"
	"pretrial-knoxc/internal/db"
)

// Phase 10 — data entry. Add a defendant (a new client), and add payments /
// check-ins to an existing one. Open to any allowed officer (audited); a
// supervisor delete (tombstone) is the backstop for a wrong entry. All writes go
// to app-owned tables only and are merged into every view (see db/dataentry.go).

// AddDefendantForm renders the new-client form. GET /admin/add_defendant
func (s *Server) AddDefendantForm(w http.ResponseWriter, r *http.Request) {
	user := auth.User(r)
	s.render(w, "add_defendant.html", map[string]any{
		"User": user, "IsSupervisor": s.Auth.IsSupervisor(user), "ActiveNav": "",
		"CSRF": s.Auth.CSRF(w, r), "Msg": r.URL.Query().Get("msg"),
	})
}

// AddDefendant creates a new client and redirects to their profile.
// POST /admin/add_defendant
func (s *Server) AddDefendant(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	nd := db.NewDefendant{
		IDN:             r.FormValue("idn"),
		Name:            r.FormValue("defendant"),
		CaseNumber:      r.FormValue("warrant_case_num"),
		Level:           r.FormValue("pretrial_level"),
		Status:          r.FormValue("case_status"),
		Officer:         r.FormValue("supervising_officer"),
		ReferralDate:    r.FormValue("referral_date"),
		GPS:             r.FormValue("gps"),
		GPSType:         r.FormValue("gps_type"),
		ChargeType:      r.FormValue("charge_type"),
		BondAmount:      r.FormValue("bond_amount"),
		SupervisionType: r.FormValue("supervision_type"),
		OrderFrom:       r.FormValue("order_from"),
		DMA:             r.FormValue("dma"),
		Birthdate:       r.FormValue("birthdate"),
	}
	if err := db.AddDefendant(s.DB, nd, auth.User(r)); err != nil {
		redirectMsg(w, r, "/admin/add_defendant", "Could not add client: "+err.Error())
		return
	}
	s.clearCache()
	redirectMsg(w, r, "/client_profile.html?idn="+url.QueryEscape(strings.TrimSpace(nd.IDN)),
		"Client added: "+strings.TrimSpace(nd.Name)+" (IDN "+strings.TrimSpace(nd.IDN)+").")
}

// AddPayment records a payment on an existing client's profile.
// POST /admin/payment/add
func (s *Server) AddPayment(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	idn, back := profileBack(r)
	officer := strings.TrimSpace(r.FormValue("officer"))
	if officer == "" {
		officer = auth.User(r)
	}
	err := db.AddPayment(s.DB, idn, r.FormValue("case_number"), r.FormValue("payment_date"),
		r.FormValue("payment_amount"), r.FormValue("payment_type"), officer, auth.User(r))
	s.afterWrite(w, r, back, err, "Payment recorded.")
}

// DeleteAddedPayment removes an app-entered payment. POST /admin/payment/delete
func (s *Server) DeleteAddedPayment(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	_, back := profileBack(r)
	s.afterWrite(w, r, back, db.DeleteAddedPayment(s.DB, formID(r), auth.User(r)), "Payment removed.")
}

// AddCheckIn records a check-in on an existing client's profile.
// POST /admin/checkin/add
func (s *Server) AddCheckIn(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	idn, back := profileBack(r)
	err := db.AddCheckIn(s.DB, idn, r.FormValue("date"), r.FormValue("type_of_check_in"), auth.User(r))
	s.afterWrite(w, r, back, err, "Check-in recorded.")
}

// DeleteAddedCheckIn removes an app-entered check-in. POST /admin/checkin/delete
func (s *Server) DeleteAddedCheckIn(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	_, back := profileBack(r)
	s.afterWrite(w, r, back, db.DeleteAddedCheckIn(s.DB, formID(r), auth.User(r)), "Check-in removed.")
}
