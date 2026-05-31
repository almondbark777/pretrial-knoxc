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
