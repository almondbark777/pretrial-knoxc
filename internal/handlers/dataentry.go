package handlers

import (
	"encoding/json"
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

// AddDefendant creates a new client and redirects to their record. The UI is
// the console intake wizard (/console/clients/new).
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
		redirectMsg(w, r, "/console/clients/new", "Could not add client: "+err.Error())
		return
	}
	// The console intake wizard collects richer detail than the added_defendants
	// schema has columns for (charges, bond type, conditions, schedule, …). It
	// packs those into intake_summary; keep them as an initial note rather than
	// dropping them. Best-effort: the client already exists if this fails.
	if summary := strings.TrimSpace(r.FormValue("intake_summary")); summary != "" {
		_ = db.AddNote(s.DB, strings.TrimSpace(nd.IDN), summary, auth.User(r))
	}
	s.clearCache()
	idn := strings.TrimSpace(nd.IDN)
	redirectMsg(w, r, safeNext(r, "/console/clients/"+url.PathEscape(idn)),
		"Client added: "+strings.TrimSpace(nd.Name)+" (IDN "+idn+").")
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
	err := db.AddCheckIn(s.DB, idn, r.FormValue("date"), r.FormValue("type_of_check_in"), r.FormValue("note"), auth.User(r))
	s.afterWrite(w, r, back, err, "Check-in recorded.")
}

// BulkAddCheckIn logs one check-in for many clients at once (the console's bulk
// action). Takes a comma-separated `idns` plus the usual date/type/note and
// returns JSON {ok, logged, error?} so the UI can show a real count rather than a
// demo toast. Best-effort per client: one bad IDN doesn't abort the rest.
// POST /admin/checkin/bulk
func (s *Server) BulkAddCheckIn(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	date := r.FormValue("date")
	ctype := r.FormValue("type_of_check_in")
	note := r.FormValue("note")
	by := auth.User(r)
	logged := 0
	var firstErr error
	for _, idn := range strings.Split(r.FormValue("idns"), ",") {
		if strings.TrimSpace(idn) == "" {
			continue
		}
		if err := db.AddCheckIn(s.DB, idn, date, ctype, note, by); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		logged++
	}
	s.clearCache()
	resp := map[string]any{"ok": logged > 0, "logged": logged}
	if firstErr != nil {
		resp["error"] = firstErr.Error()
	}
	w.Header().Set("Content-Type", "application/json")
	if logged == 0 {
		w.WriteHeader(http.StatusBadRequest)
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// DeleteAddedCheckIn removes an app-entered check-in. POST /admin/checkin/delete
func (s *Server) DeleteAddedCheckIn(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	_, back := profileBack(r)
	s.afterWrite(w, r, back, db.DeleteAddedCheckIn(s.DB, formID(r), auth.User(r)), "Check-in removed.")
}
