package db

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"pretrial-knoxc/internal/emfees"
)

// freshEMFeesDB opens a brand-new SQLite DB, runs EnsureSchema (for overrides /
// deleted_idns / added_* / audit_log), and creates the three raw_* tables the
// EM-fee engine reads. Using a synthetic DB keeps this test deterministic and
// independent of the (stale, gitignored) offline snapshot.
func freshEMFeesDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := Open(filepath.Join(t.TempDir(), "emfees_ov.db"))
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
			case_status TEXT, gps_type TEXT, gps TEXT, referral_date TEXT,
			closed_date TEXT, released_to_hilltop_date TEXT, court TEXT
		)`,
		`CREATE TABLE raw_payments (
			idn TEXT, case_number TEXT, payment_type TEXT, payment_amount TEXT, payment_date TEXT
		)`,
		`CREATE TABLE raw_gps_48_hours (
			idn TEXT, defendant TEXT, case_number TEXT, case_status TEXT, gps_type TEXT,
			gps_install_date TEXT, closed_date TEXT, switched_to TEXT, switched_gps_date TEXT
		)`,
	}
	for _, s := range stmts {
		if _, err := d.Exec(s); err != nil {
			t.Fatalf("create raw table: %v", err)
		}
	}
	return d
}

// addBBClosed inserts a closed GPS blue-book row that — left alone — produces one
// Closed EM-fee record: start = referral_date, end = closed_date (31 days apart =
// 32 inclusive), so at ALLIED ($8) it is $256 owed / 32 days behind (>= 5).
func addBBClosed(t *testing.T, d *sql.DB, idn, name, caseNo, gpsType string) {
	t.Helper()
	_, err := d.Exec(`INSERT INTO raw_blue_book
		(idn, defendant, warrant_case_num, case_status, gps_type, gps, referral_date, closed_date)
		VALUES (?, ?, ?, 'CLOSED', ?, 'True', '1/1/2026', '2/1/2026')`,
		idn, name, caseNo, gpsType)
	if err != nil {
		t.Fatalf("insert bb row: %v", err)
	}
}

// TestEMFeesAppliesOverrides proves a supervisor's field corrections reach the
// Past-Due EM Fees report + its show-cause letters — the same way they reach every
// other view — covering the three material override effects:
//   - gps_type   -> changes the daily rate (ALLIED $8 -> SCRAM $15) and the arrears
//   - case_status -> the Open/Closed gate (a reopened case drops off the Closed list)
//   - defendant   -> the junk-name filter (a corrected "TEST" name starts billing)
func TestEMFeesAppliesOverrides(t *testing.T) {
	d := freshEMFeesDB(t)
	addBBClosed(t, d, "900000001", "ALPHA, AMY", "@A1", "ALLIED")  // rate flip
	addBBClosed(t, d, "900000002", "BETA, BOB", "@B1", "ALLIED")   // status flip
	addBBClosed(t, d, "900000003", "TEST, TERRY", "@C1", "ALLIED") // junk -> real
	asOf := time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC)

	byIDN := func(res emfees.Result) map[string]emfees.Rec {
		m := map[string]emfees.Rec{}
		for _, r := range res.Closed {
			m[r.IDN] = r
		}
		return m
	}

	// --- baseline: A & B bill at ALLIED $256; the junk-named C is skipped ---
	base, err := EMFees(d, asOf)
	if err != nil {
		t.Fatalf("EMFees baseline: %v", err)
	}
	bm := byIDN(base)
	if len(base.Closed) != 2 {
		t.Fatalf("baseline Closed = %d, want 2 (A,B); recs=%+v", len(base.Closed), base.Closed)
	}
	if a := bm["900000001"]; a.Type != "ALLIED" || a.Rate != 8 || a.Owed != 256 {
		t.Fatalf("baseline A: type=%q rate=%d owed=%v, want ALLIED/8/256", a.Type, a.Rate, a.Owed)
	}
	if _, ok := bm["900000003"]; ok {
		t.Fatalf("baseline: junk-named C should be skipped, but appeared")
	}

	// --- apply three overrides (each audited) ---
	if err := SetOverride(d, "900000001", "gps_type", "SCRAM", "sup@knoxsheriff.org"); err != nil {
		t.Fatalf("override gps_type: %v", err)
	}
	if err := SetOverride(d, "900000002", "case_status", "OPEN", "sup@knoxsheriff.org"); err != nil {
		t.Fatalf("override case_status: %v", err)
	}
	if err := SetOverride(d, "900000003", "defendant", "GAMMA, GREG", "sup@knoxsheriff.org"); err != nil {
		t.Fatalf("override defendant: %v", err)
	}

	// --- after: A -> SCRAM $480, B reopened (off the Closed list), C now billed ---
	after, err := EMFees(d, asOf)
	if err != nil {
		t.Fatalf("EMFees after override: %v", err)
	}
	am := byIDN(after)
	if len(after.Closed) != 2 {
		t.Fatalf("after Closed = %d, want 2 (A,C); recs=%+v", len(after.Closed), after.Closed)
	}
	if a := am["900000001"]; a.Type != "SCRAM" || a.Rate != 15 || a.Owed != 480 {
		t.Fatalf("after A: type=%q rate=%d owed=%v, want SCRAM/15/480 (gps_type override)", a.Type, a.Rate, a.Owed)
	}
	if _, ok := am["900000002"]; ok {
		t.Fatalf("after: B reopened via case_status override should be off the Closed list")
	}
	c, ok := am["900000003"]
	if !ok {
		t.Fatalf("after: C un-junked via defendant override should now be billed")
	}
	if c.Name != "GAMMA, GREG" || c.Type != "ALLIED" || c.Owed != 256 {
		t.Fatalf("after C: name=%q type=%q owed=%v, want GAMMA, GREG/ALLIED/256", c.Name, c.Type, c.Owed)
	}
}

// addDefendantGPS inserts an app-entered (added_defendants) person with a GPS install
// date — the intake path that has no 48-hour row. EnsureSchema creates the table.
func addDefendantGPS(t *testing.T, d *sql.DB, idn, name, caseNo, gpsType, install string) {
	t.Helper()
	if _, err := d.Exec(`INSERT INTO added_defendants
		(idn, defendant, warrant_case_num, case_status, gps_type, gps_install_date, author, created_at)
		VALUES (?, ?, ?, 'OPEN', ?, ?, 'officer@knoxsheriff.org', '2026-04-01')`,
		idn, name, caseNo, gpsType, install); err != nil {
		t.Fatalf("insert added_defendant: %v", err)
	}
}

// TestEMFeesAppEnteredOpenGPSClient proves an app-entered OPEN GPS client (no 48-hour
// row) reaches the arrears / show-cause list (item #2). Without the synthesize step
// such a person silently misses a letter.
func TestEMFeesAppEnteredOpenGPSClient(t *testing.T) {
	d := freshEMFeesDB(t)
	// ALLIED, installed 4/1, asOf 5/1 → 31 days × $8 = $248 on the OPEN list.
	addDefendantGPS(t, d, "920000001", "NEWBY, NICK", "@N1", "ALLIED", "4/1/2026")
	asOf := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	res, err := EMFees(d, asOf)
	if err != nil {
		t.Fatalf("EMFees: %v", err)
	}
	var got []emfees.Rec
	for _, r := range res.Open {
		if r.IDN == "920000001" {
			got = append(got, r)
		}
	}
	if len(got) != 1 {
		t.Fatalf("app-entered OPEN GPS client should appear exactly once on Open; got %d: %+v", len(got), res.Open)
	}
	if r := got[0]; r.Name != "NEWBY, NICK" || r.Case != "@N1" || r.Type != "ALLIED" || r.Owed != 248 {
		t.Fatalf("app-entered rec wrong: %+v, want NEWBY/@N1/ALLIED/248", r)
	}
}

// TestEMFeesAppEnteredNoDoubleBill proves the once-only guard: when the importer later
// ships a real 48-hour row for the same IDN, the synthetic added row is suppressed so
// the person is billed exactly once (no double-billing on the legal letter).
func TestEMFeesAppEnteredNoDoubleBill(t *testing.T) {
	d := freshEMFeesDB(t)
	// Same IDN in BOTH the 48-hour file and added_defendants.
	addGPS48Open(t, d, "930000001", "DUP, DON", "@D1", "ALLIED", "4/1/2026")
	addDefendantGPS(t, d, "930000001", "DUP, DON", "@D1", "ALLIED", "4/1/2026")
	asOf := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	res, err := EMFees(d, asOf)
	if err != nil {
		t.Fatalf("EMFees: %v", err)
	}
	n := 0
	for _, r := range res.Open {
		if r.IDN == "930000001" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("IDN present in both 48-hour and added rows must be billed once; got %d Open recs", n)
	}
}

// addGPS48Open inserts an OPEN GPS 48-hour row plus a minimal blue-book row (for the
// name fallback). Left alone it bills from gps_install_date through asOf.
func addGPS48Open(t *testing.T, d *sql.DB, idn, name, caseNo, gpsType, install string) {
	t.Helper()
	if _, err := d.Exec(`INSERT INTO raw_gps_48_hours
		(idn, defendant, case_number, case_status, gps_type, gps_install_date)
		VALUES (?, ?, ?, 'OPEN', ?, ?)`, idn, name, caseNo, gpsType, install); err != nil {
		t.Fatalf("insert gps48 row: %v", err)
	}
	if _, err := d.Exec(`INSERT INTO raw_blue_book
		(idn, defendant, warrant_case_num, case_status, gps_type, gps)
		VALUES (?, ?, ?, 'OPEN', ?, 'True')`, idn, name, caseNo, gpsType); err != nil {
		t.Fatalf("insert bb row: %v", err)
	}
}

// TestEMFeesAppliesOverridesPass1 proves a supervisor override reaches the OPEN list,
// which Pass 1 builds straight from the GPS 48-hour rows (item #1). Before the fix the
// override only spliced into blue-book rows, so a SCRAM→ALLIED rate correction (and a
// case_status / closed_date fix) was silently discarded for the entire Open list.
func TestEMFeesAppliesOverridesPass1(t *testing.T) {
	d := freshEMFeesDB(t)
	// SCRAM, installed 4/1, asOf 5/1 → 31 days × $15 = $465 owed, $0 paid.
	addGPS48Open(t, d, "910000001", "OPEN, OLLY", "@O1", "SCRAM", "4/1/2026")
	asOf := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	openByIDN := func(res emfees.Result) map[string]emfees.Rec {
		m := map[string]emfees.Rec{}
		for _, r := range res.Open {
			m[r.IDN] = r
		}
		return m
	}

	// --- baseline: bills at SCRAM $15/day = $465 on the OPEN list ---
	base, err := EMFees(d, asOf)
	if err != nil {
		t.Fatalf("EMFees baseline: %v", err)
	}
	if r, ok := openByIDN(base)["910000001"]; !ok || r.Type != "SCRAM" || r.Rate != 15 || r.Owed != 465 {
		t.Fatalf("baseline Pass-1: %+v (want SCRAM/15/465 on Open)", r)
	}

	// --- override gps_type SCRAM→ALLIED: the $8 rate must reach the Open record ---
	if err := SetOverride(d, "910000001", "gps_type", "ALLIED", "sup@knoxsheriff.org"); err != nil {
		t.Fatalf("override gps_type: %v", err)
	}
	after, err := EMFees(d, asOf)
	if err != nil {
		t.Fatalf("EMFees after override: %v", err)
	}
	r, ok := openByIDN(after)["910000001"]
	if !ok || r.Type != "ALLIED" || r.Rate != 8 || r.Owed != 248 {
		t.Fatalf("after Pass-1 gps_type override: %+v, want ALLIED/8/248 (31×$8); override ignored on the 48-hour rows", r)
	}

	// --- override case_status OPEN→CLOSED + closed_date: moves to the Closed list and
	// shortens the billing window (end = closed_date), proving both fields splice. ---
	if err := SetOverride(d, "910000001", "case_status", "CLOSED", "sup@knoxsheriff.org"); err != nil {
		t.Fatalf("override case_status: %v", err)
	}
	if err := SetOverride(d, "910000001", "closed_date", "4/16/2026", "sup@knoxsheriff.org"); err != nil {
		t.Fatalf("override closed_date: %v", err)
	}
	after2, err := EMFees(d, asOf)
	if err != nil {
		t.Fatalf("EMFees after status/date override: %v", err)
	}
	if _, ok := openByIDN(after2)["910000001"]; ok {
		t.Fatalf("after status override: client should be off the Open list")
	}
	var closed *emfees.Rec
	for i := range after2.Closed {
		if after2.Closed[i].IDN == "910000001" {
			closed = &after2.Closed[i]
		}
	}
	if closed == nil {
		t.Fatalf("after status override: client should be on the Closed list")
	}
	// 4/1 → 4/16 inclusive = 16 days × $8 = $128 (closed_date override shortened it).
	if closed.Days != 16 || closed.Owed != 128 {
		t.Fatalf("after closed_date override: days=%d owed=%v, want 16/128", closed.Days, closed.Owed)
	}
}
