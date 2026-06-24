package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"pretrial-knoxc/internal/db"
)

// postAdd drives the AddDefendant handler directly (bypassing the router's CSRF
// middleware, which isn't under test here) with a urlencoded form body.
func postAdd(t *testing.T, srv *Server, form url.Values) {
	t.Helper()
	req := httptest.NewRequest("POST", "/admin/add_defendant", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.AddDefendant(rec, req)
	if rec.Code != http.StatusSeeOther && rec.Code != http.StatusOK {
		t.Fatalf("AddDefendant status = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func addedField(t *testing.T, d *sql.DB, idn, col string) string {
	t.Helper()
	var v sql.NullString
	if err := d.QueryRow("SELECT "+col+" FROM added_defendants WHERE idn = ?", idn).Scan(&v); err != nil {
		t.Fatalf("query %s for idn %s (row missing → add failed?): %v", col, idn, err)
	}
	return v.String
}

func TestAddDefendantAutoAssignsByLastName(t *testing.T) {
	d := testDB(t)
	srv := newServer(d)
	if err := db.SetCaseloadAssignments(d, map[string]string{"Z": "Marcus Olsen"}, "sup@knoxsheriff.org"); err != nil {
		t.Fatalf("SetCaseloadAssignments: %v", err)
	}

	// Officer left on "Auto" (blank) → assigned by the 'Z' of the last name.
	auto := "999777111"
	postAdd(t, srv, url.Values{
		"defendant": {"ZZAUTO, ZARA"}, "idn": {auto},
		"warrant_case_num": {"@999111"}, "pretrial_level": {"2"},
		"supervising_officer": {""}, "gps": {"false"},
	})
	if got := addedField(t, d, auto, "supervising_officer"); got != "Marcus Olsen" {
		t.Errorf("auto-assign: supervising_officer = %q, want Marcus Olsen", got)
	}

	// Manual selection is honored even though 'Z' maps to someone else.
	manual := "999777222"
	postAdd(t, srv, url.Values{
		"defendant": {"ZQMANUAL, MARV"}, "idn": {manual},
		"warrant_case_num": {"@999222"}, "pretrial_level": {"1"},
		"supervising_officer": {"Kathy Jones"}, "gps": {"false"},
	})
	if got := addedField(t, d, manual, "supervising_officer"); got != "Kathy Jones" {
		t.Errorf("manual officer not preserved: got %q, want Kathy Jones", got)
	}

	// Unmapped initial with a blank officer stays blank (no guess).
	unmapped := "999777333"
	postAdd(t, srv, url.Values{
		"defendant": {"QUNMAP, QUINN"}, "idn": {unmapped},
		"warrant_case_num": {"@999333"}, "supervising_officer": {""}, "gps": {"false"},
	})
	if got := addedField(t, d, unmapped, "supervising_officer"); got != "" {
		t.Errorf("unmapped should stay blank, got %q", got)
	}
}

// Bond conditions is a multi-select (SCRAM #4 / Drug Screens #5 / Supervision
// #7): the checked boxes are joined into one cell. Supervision type is one of the
// fixed dropdown values. Both round-trip into added_defendants.
func TestAddDefendantBondConditionsAndSupervision(t *testing.T) {
	d := testDB(t)
	srv := newServer(d)

	// Two bond-condition boxes checked → joined "A, B" in submit order.
	multi := "999779111"
	postAdd(t, srv, url.Values{
		"defendant": {"BONDER, BEV"}, "idn": {multi}, "warrant_case_num": {"@779111"},
		"pretrial_level": {"2"}, "supervision_type": {"GPS"}, "gps": {"false"},
		"bond_conditions": {"#4 SCRAM", "#7 Supervision"},
	})
	if got := addedField(t, d, multi, "bond_conditions"); got != "#4 SCRAM, #7 Supervision" {
		t.Errorf("bond_conditions = %q, want %q", got, "#4 SCRAM, #7 Supervision")
	}
	if got := addedField(t, d, multi, "supervision_type"); got != "GPS" {
		t.Errorf("supervision_type = %q, want GPS", got)
	}

	// No box checked → empty cell (not "[]" or a stray comma).
	none := "999779222"
	postAdd(t, srv, url.Values{
		"defendant": {"NOCOND, NICK"}, "idn": {none}, "warrant_case_num": {"@779222"},
		"pretrial_level": {"1"}, "gps": {"false"},
	})
	if got := addedField(t, d, none, "bond_conditions"); got != "" {
		t.Errorf("bond_conditions with nothing checked = %q, want empty", got)
	}
}

// The intake form's IDN autofill (/api/prefill) returns the identity + case
// fields we already have for an existing person, with dates normalized to ISO for
// the date inputs; an unknown IDN returns found:false.
func TestAPIPrefill(t *testing.T) {
	d := testDB(t)
	srv := newServer(d)

	idn := "999881234"
	postAdd(t, srv, url.Values{
		"defendant": {"PREFILL, PAT"}, "idn": {idn}, "warrant_case_num": {"@88123"},
		"pretrial_level": {"2"}, "charge_type": {"Felony"}, "bond_amount": {"$25,000"},
		"order_from": {"Magistrate"}, "birthdate": {"1990-05-15"}, "supervising_officer": {"Kathy Jones"},
		"gps": {"true"}, "gps_type": {"ALLIED"}, "gps_install_date": {"2026-06-01"},
	})

	get := func(idn string) map[string]any {
		req := httptest.NewRequest("GET", "/api/prefill?idn="+idn, nil)
		rec := httptest.NewRecorder()
		srv.APIPrefill(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("APIPrefill status = %d", rec.Code)
		}
		var m map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
			t.Fatalf("bad JSON: %v\n%s", err, rec.Body.String())
		}
		return m
	}

	g := get(idn)
	if g["found"] != true {
		t.Fatalf("found = %v, want true (body shape %v)", g["found"], g)
	}
	if g["name"] != "PREFILL, PAT" {
		t.Errorf("name = %v", g["name"])
	}
	if g["bondAmount"] != "$25,000" || g["chargeType"] != "Felony" || g["orderFrom"] != "Magistrate" {
		t.Errorf("case fields wrong: %+v", g)
	}
	if g["level"].(float64) != 2 {
		t.Errorf("level = %v, want 2", g["level"])
	}
	if g["gps"] != true || g["gpsType"] != "ALLIED" {
		t.Errorf("gps = %v / %v, want true/ALLIED", g["gps"], g["gpsType"])
	}
	// Dates come back as YYYY-MM-DD so the form's date inputs accept them.
	isISO := func(v string) bool { return len(v) == 10 && v[4] == '-' && v[7] == '-' }
	if bd, _ := g["birthdate"].(string); !isISO(bd) {
		t.Errorf("birthdate = %q, want ISO YYYY-MM-DD", bd)
	}
	// gpsInstall is sourced from the GPS-events dataset (absent for an app-only
	// client), so here it's empty — but when present it must be ISO.
	if gi, _ := g["gpsInstall"].(string); gi != "" && !isISO(gi) {
		t.Errorf("gpsInstall = %q, want empty or ISO", gi)
	}

	if u := get("000000099"); u["found"] != false {
		t.Errorf("unknown IDN: found = %v, want false", u["found"])
	}
}

func TestAddDefendantGPSFields(t *testing.T) {
	d := testDB(t)
	srv := newServer(d)

	// GPS = Yes: GPS + victim fields persist (dates normalized to M/D/YYYY).
	yes := "999778111"
	postAdd(t, srv, url.Values{
		"defendant": {"GPSON, GARY"}, "idn": {yes}, "warrant_case_num": {"@778111"},
		"gps": {"true"}, "gps_type": {"ALLIED"}, "gps_install_date": {"2026-06-01"},
		"victim": {"VICTIM, VERA"}, "victim_idn": {"55001"}, "victim_accept_deny_gps": {"Accept"},
	})
	if got := addedField(t, d, yes, "gps"); got != "True" {
		t.Errorf("gps = %q, want True", got)
	}
	if got := addedField(t, d, yes, "gps_type"); got != "ALLIED" {
		t.Errorf("gps_type = %q, want ALLIED", got)
	}
	if got := addedField(t, d, yes, "gps_install_date"); got != "6/1/2026" {
		t.Errorf("gps_install_date = %q, want 6/1/2026 (normalized)", got)
	}
	if got := addedField(t, d, yes, "victim"); got != "VICTIM, VERA" {
		t.Errorf("victim = %q, want VICTIM, VERA", got)
	}

	// GPS = No: any GPS/victim values posted by the (hidden) section are dropped.
	no := "999778222"
	postAdd(t, srv, url.Values{
		"defendant": {"GPSOFF, GLEN"}, "idn": {no}, "warrant_case_num": {"@778222"},
		"gps": {"false"}, "gps_type": {"SCRAM"}, "victim": {"SHOULD, NOTSTICK"},
	})
	if got := addedField(t, d, no, "gps"); got != "False" {
		t.Errorf("gps = %q, want False", got)
	}
	if got := addedField(t, d, no, "gps_type"); got != "" {
		t.Errorf("gps_type should be cleared on No, got %q", got)
	}
	if got := addedField(t, d, no, "victim"); got != "" {
		t.Errorf("victim should be cleared on No, got %q", got)
	}
}

// The record-level "Edit GPS details" endpoint (POST /admin/gps/update) lets any
// officer fill the vendor / install / switch / victim-48h fields the import left
// blank; the values flow back into the computed client via BuildClients. Bypasses
// the router's CSRF middleware (not under test); the handler itself is officer-
// accessible (no supervisor gate).
func TestUpdateGPSPersistsAndShows(t *testing.T) {
	d := testDB(t)
	srv := newServer(d)

	// A GPS-active referral with vendor + install blank (Dunaway's situation).
	idn := "999780111"
	postAdd(t, srv, url.Values{
		"defendant": {"VENDORLESS, VAL"}, "idn": {idn}, "warrant_case_num": {"@780111"},
		"gps": {"true"}, // GPS on, but no gps_type / install
	})

	req := httptest.NewRequest("POST", "/admin/gps/update", strings.NewReader(url.Values{
		"idn":                    {idn},
		"gps_type":               {"ALLIED"},
		"gps_install_date":       {"2026-06-01"},
		"switched_to":            {"SCRAM"},
		"switched_gps_date":      {"2026-07-01"},
		"victim_time_48":         {"2026-06-02T14:30"},
		"victim_accept_deny_gps": {"Accept"},
		"victim":                 {"DOE, JANE"},
		"victim_idn":             {"987654"},
	}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.UpdateGPS(rec, req)
	if rec.Code != http.StatusSeeOther && rec.Code != http.StatusOK {
		t.Fatalf("UpdateGPS status = %d, body=%s", rec.Code, rec.Body.String())
	}

	clients, err := db.BuildClients(d, time.Now())
	if err != nil {
		t.Fatalf("BuildClients: %v", err)
	}
	cases := clients[idn]
	if len(cases) == 0 {
		t.Fatalf("client %s not found after GPS update", idn)
	}
	c := cases[0]
	// SetGPSDetails stores the submitted value verbatim (overrides table), so the
	// computed client reflects exactly what the form posted.
	for label, gw := range map[string][2]string{
		"vendor":     {c.GpsType, "ALLIED"},
		"install":    {c.GpInstall, "2026-06-01"},
		"switchedTo": {c.GpSwitchedTo, "SCRAM"},
		"switchDate": {c.GpSwitchedDate, "2026-07-01"},
		"victim48":   {c.VictimNotify48, "2026-06-02T14:30"},
		"acceptDeny": {c.VictimAcceptDeny, "Accept"},
		"victim":     {c.Victim, "DOE, JANE"},
		"victimIDN":  {c.VictimIDN, "987654"},
	} {
		if gw[0] != gw[1] {
			t.Errorf("%s = %q, want %q", label, gw[0], gw[1])
		}
	}
}
