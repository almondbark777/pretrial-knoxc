package handlers

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

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
