package db

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

// freshGPSDB opens a new SQLite DB with EnsureSchema (overrides/audit) and the
// four raw_* tables BuildClients reads, so the GPS-details overlay can be tested
// end-to-end without the gitignored offline snapshot.
func freshGPSDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := Open(filepath.Join(t.TempDir(), "gps_details.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if err := EnsureSchema(d); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	stmts := []string{
		`CREATE TABLE raw_blue_book (
			idn TEXT, defendant TEXT, warrant_case_num TEXT, case_number TEXT,
			case_status TEXT, pretrial_level TEXT, gps_type TEXT, gps TEXT,
			referral_date TEXT, supervising_officer TEXT
		)`,
		`CREATE TABLE raw_check_ins (idn TEXT, date TEXT, type_of_check_in TEXT)`,
		`CREATE TABLE raw_payments (idn TEXT, case_number TEXT, payment_type TEXT, payment_amount TEXT, payment_date TEXT)`,
		`CREATE TABLE raw_gps_48_hours (
			idn TEXT, defendant TEXT, case_number TEXT, gps_type TEXT,
			gps_install_date TEXT, switched_to TEXT, switched_gps_date TEXT
		)`,
	}
	for _, s := range stmts {
		if _, err := d.Exec(s); err != nil {
			t.Fatalf("create raw table: %v", err)
		}
	}
	return d
}

// TestSetGPSDetailsOverlay proves the record-level "Edit GPS details" form (stored
// as overrides) flows back into the computed client: vendor/install/switch/victim
// values that the import left blank are filled, and blanking a field reverts it.
func TestSetGPSDetailsOverlay(t *testing.T) {
	d := freshGPSDB(t)
	// GPS-active client (gps=True) with vendor + install blank — Dunaway's situation.
	if _, err := d.Exec(`INSERT INTO raw_blue_book
		(idn, defendant, warrant_case_num, case_status, gps, referral_date)
		VALUES ('999','TEST, GPS','@999','OPEN','True','1/1/2026')`); err != nil {
		t.Fatalf("seed bb: %v", err)
	}
	track := time.Now()

	clients, err := BuildClients(d, track)
	if err != nil {
		t.Fatalf("build (before): %v", err)
	}
	c := clients["999"][0]
	if !c.GpsActive {
		t.Fatal("client should be GPS-active before edit")
	}
	if c.GpsType != "" {
		t.Fatalf("vendor should start blank, got %q", c.GpsType)
	}

	vals := map[string]string{
		"gps_type":               "ALLIED",
		"gps_install_date":       "2026-01-05",
		"switched_to":            "SCRAM",
		"switched_gps_date":      "2026-02-01",
		"victim_time_48":         "2026-01-02T09:30",
		"victim_accept_deny_gps": "Accept",
		"victim":                 "DOE, JANE",
		"victim_idn":             "555",
	}
	if err := SetGPSDetails(d, "999", vals, "tester"); err != nil {
		t.Fatalf("set gps details: %v", err)
	}

	clients, err = BuildClients(d, track)
	if err != nil {
		t.Fatalf("build (after): %v", err)
	}
	c = clients["999"][0]
	checks := map[string]struct{ got, want string }{
		"vendor":     {c.GpsType, "ALLIED"},
		"install":    {c.GpInstall, "2026-01-05"},
		"switchedTo": {c.GpSwitchedTo, "SCRAM"},
		"switchDate": {c.GpSwitchedDate, "2026-02-01"},
		"victim48":   {c.VictimNotify48, "2026-01-02T09:30"},
		"acceptDeny": {c.VictimAcceptDeny, "Accept"},
		"victimName": {c.Victim, "DOE, JANE"},
		"victimIDN":  {c.VictimIDN, "555"},
	}
	for label, ck := range checks {
		if ck.got != ck.want {
			t.Errorf("%s: got %q, want %q", label, ck.got, ck.want)
		}
	}

	// Blanking a field clears the override (reverts to the imported blank value).
	if err := SetGPSDetails(d, "999", map[string]string{"gps_type": ""}, "tester"); err != nil {
		t.Fatalf("clear vendor: %v", err)
	}
	clients, _ = BuildClients(d, track)
	if c = clients["999"][0]; c.GpsType != "" {
		t.Errorf("vendor should revert to blank after clearing, got %q", c.GpsType)
	}
	// A field not present in the second submit must be untouched.
	if c.GpInstall != "2026-01-05" {
		t.Errorf("install should be unchanged by the vendor-only clear, got %q", c.GpInstall)
	}
}

// TestSetGPSDetailsRejectsUnknownField confirms the GPS editor only writes its own
// allow-listed fields (an unknown key is ignored, not injected).
func TestSetGPSDetailsRejectsUnknownField(t *testing.T) {
	d := freshGPSDB(t)
	if _, err := d.Exec(`INSERT INTO raw_blue_book (idn, defendant, gps, case_status)
		VALUES ('111','X','True','OPEN')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := SetGPSDetails(d, "111", map[string]string{"case_status": "CLOSED"}, "tester"); err != nil {
		t.Fatalf("set: %v", err)
	}
	clients, _ := BuildClients(d, time.Now())
	if got := clients["111"][0].Status; got != "OPEN" {
		t.Errorf("case_status must not be writable via the GPS editor, got %q", got)
	}
}
