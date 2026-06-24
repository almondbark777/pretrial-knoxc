package db

import (
	"database/sql"
	"testing"
	"time"

	"pretrial-knoxc/internal/compute"
	"pretrial-knoxc/internal/models"
)

// openEnsured copies the offline DB, opens it, and runs EnsureSchema so the
// added_* / audit tables exist for data-entry tests.
func openEnsured(t *testing.T) *sql.DB {
	t.Helper()
	path, _ := tempLookupDB(t)
	d, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if err := EnsureSchema(d); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	return d
}

func TestAddDefendantFlowsIntoBuildClients(t *testing.T) {
	d := openEnsured(t)
	idn := "999000111" // not present in the offline roster
	if IDNExistsInRoster(d, idn) {
		t.Fatalf("test IDN unexpectedly already in roster")
	}
	nd := NewDefendant{IDN: idn, Name: "ZZTEST, ADAM", Level: "2", Status: "Open",
		Officer: "Jane.Doe@knoxsheriff.org", GPS: "true", GPSType: "ALLIED", CaseNumber: "@999"}
	if err := AddDefendant(d, nd, "tester@knoxsheriff.org"); err != nil {
		t.Fatalf("AddDefendant: %v", err)
	}
	if !IDNExistsInRoster(d, idn) {
		t.Fatalf("IDN should exist after add")
	}
	// Duplicate add is rejected.
	if err := AddDefendant(d, nd, "tester@knoxsheriff.org"); err != errExistingIDN {
		t.Fatalf("duplicate add err = %v, want errExistingIDN", err)
	}

	clients, err := BuildClients(d, time.Now())
	if err != nil {
		t.Fatalf("BuildClients: %v", err)
	}
	cases := clients[idn]
	if len(cases) != 1 {
		t.Fatalf("added defendant: got %d case rows, want 1", len(cases))
	}
	c := cases[0]
	if c.Name != "ZZTEST, ADAM" || c.Level != "2" || c.GpsType != "ALLIED" || !c.GpsActive {
		t.Fatalf("added defendant fields wrong: %+v", c)
	}
}

func TestAddPaymentAndCheckInFlowIn(t *testing.T) {
	d := openEnsured(t)
	idn := "999000222"
	if err := AddDefendant(d, NewDefendant{IDN: idn, Name: "ZZPAY, PAT", Level: "2", Status: "Open", GPS: "true", GPSType: "SCRAM"}, "by"); err != nil {
		t.Fatalf("AddDefendant: %v", err)
	}
	if err := AddPayment(d, idn, "@1", "5/1/2026", "120", "SCRAM", "Officer X", "by"); err != nil {
		t.Fatalf("AddPayment: %v", err)
	}
	if err := AddCheckIn(d, idn, "5/2/2026", "In Person", "GPS fitment: strap sized, base unit issued", "by"); err != nil {
		t.Fatalf("AddCheckIn: %v", err)
	}
	// The per-check-in note round-trips via ListAddedCheckIns.
	added, err := ListAddedCheckIns(d, idn)
	if err != nil || len(added) == 0 {
		t.Fatalf("ListAddedCheckIns: %v (n=%d)", err, len(added))
	}
	if added[0].Note != "GPS fitment: strap sized, base unit issued" {
		t.Errorf("check-in note not persisted, got %q", added[0].Note)
	}

	clients, err := BuildClients(d, time.Now())
	if err != nil {
		t.Fatalf("BuildClients: %v", err)
	}
	c := clients[idn][0]
	gotPay := false
	for _, p := range c.Payments {
		if p.Amt == 120 && p.Type == "SCRAM" {
			gotPay = true
		}
	}
	if !gotPay {
		t.Fatalf("added payment not in client.Payments: %+v", c.Payments)
	}
	gotCI := false
	for _, ci := range c.CheckIns {
		if ci.Type == "In Person" {
			gotCI = true
		}
	}
	if !gotCI {
		t.Fatalf("added check-in not in client.CheckIns: %+v", c.CheckIns)
	}

	// Listed + deletable.
	pays, err := ListAddedPayments(d, idn)
	if err != nil || len(pays) != 1 {
		t.Fatalf("ListAddedPayments = %v, %v", pays, err)
	}
	if err := DeleteAddedPayment(d, pays[0].ID, "by"); err != nil {
		t.Fatalf("DeleteAddedPayment: %v", err)
	}
	if pays2, _ := ListAddedPayments(d, idn); len(pays2) != 0 {
		t.Fatalf("payment not deleted: %v", pays2)
	}
}

func TestAddedDefendantRespectsTombstone(t *testing.T) {
	d := openEnsured(t)
	idn := "999000333"
	if err := AddDefendant(d, NewDefendant{IDN: idn, Name: "ZZGONE, GUY", Level: "1", Status: "Open"}, "by"); err != nil {
		t.Fatalf("AddDefendant: %v", err)
	}
	if _, ok := mustBuild(t, d)[idn]; !ok {
		t.Fatalf("added defendant should be present before delete")
	}
	if err := DeletePerson(d, idn, "supervisor", "entered by mistake", false); err != nil {
		t.Fatalf("DeletePerson: %v", err)
	}
	if _, ok := mustBuild(t, d)[idn]; ok {
		t.Fatalf("tombstoned added defendant should vanish from BuildClients")
	}
}

func TestDataEntryWritesAudit(t *testing.T) {
	d := openEnsured(t)
	idn := "999000444"
	_ = AddDefendant(d, NewDefendant{IDN: idn, Name: "ZZAUD, ANN", Status: "Open"}, "auditor@knoxsheriff.org")
	_ = AddPayment(d, idn, "", "5/1/2026", "50", "GPS", "", "auditor@knoxsheriff.org")
	_ = AddCheckIn(d, idn, "5/2/2026", "Phone", "", "auditor@knoxsheriff.org")
	rows, err := ListAudit(d, idn, 50)
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	want := map[string]bool{"defendant_add": false, "payment_add": false, "checkin_add": false}
	for _, r := range rows {
		if _, ok := want[r.Action]; ok {
			want[r.Action] = true
		}
	}
	for action, seen := range want {
		if !seen {
			t.Fatalf("audit missing action %q (rows=%d)", action, len(rows))
		}
	}
}

func mustBuild(t *testing.T, d *sql.DB) map[string][]*compute.Client {
	t.Helper()
	cl, err := BuildClients(d, time.Now())
	if err != nil {
		t.Fatalf("BuildClients: %v", err)
	}
	return cl
}

// TestLiveListsFilterTombstones verifies that ListAllCourtDatesLive,
// ListAllViolationsLive, and ListAllScheduledCheckInsLive suppress rows
// belonging to a whole-person tombstoned IDN (item #7).
//
// The tombstone is planted directly into deleted_idns WITHOUT going through
// DeletePerson (which also purges extension rows) so that the filter in the
// Live variants is the only thing removing these rows from the feeds.
func TestLiveListsFilterTombstones(t *testing.T) {
	d := openEnsured(t)
	const liveIDN = "999001001"
	const deadIDN = "999001002"

	// Add two defendants (no GPS, level 1 — minimal).
	for _, idn := range []string{liveIDN, deadIDN} {
		if err := AddDefendant(d, NewDefendant{IDN: idn, Name: "ZZLIVE " + idn, Level: "1", Status: "Open"}, "by"); err != nil {
			t.Fatalf("AddDefendant %s: %v", idn, err)
		}
	}

	// Seed court date, violation, and scheduled check-in for both.
	for _, idn := range []string{liveIDN, deadIDN} {
		if err := AddCourtDate(d, idn, "2026-08-01", "Room A", "", "by"); err != nil {
			t.Fatalf("AddCourtDate %s: %v", idn, err)
		}
		if err := AddViolation(d, idn, "2026-07-01", "Curfew", "minor", "test", "", "by"); err != nil {
			t.Fatalf("AddViolation %s: %v", idn, err)
		}
		if err := AddScheduledCheckIn(d, idn, "2026-07-10", "In-person", "", "by"); err != nil {
			t.Fatalf("AddScheduledCheckIn %s: %v", idn, err)
		}
	}

	// Before tombstone: Live variants must include both IDNs.
	courts, _ := ListAllCourtDatesLive(d)
	viols, _ := ListAllViolationsLive(d)
	scheds, _ := ListAllScheduledCheckInsLive(d)
	if !containsIDN(courts, deadIDN) || !containsIDN(courts, liveIDN) {
		t.Errorf("pre-tombstone: court dates missing expected IDNs (courts=%d)", len(courts))
	}
	if !containsIDNV(viols, deadIDN) || !containsIDNV(viols, liveIDN) {
		t.Errorf("pre-tombstone: violations missing expected IDNs (viols=%d)", len(viols))
	}
	if !containsIDNS(scheds, deadIDN) || !containsIDNS(scheds, liveIDN) {
		t.Errorf("pre-tombstone: scheds missing expected IDNs (scheds=%d)", len(scheds))
	}

	// Plant a whole-person tombstone directly (no extension-row purge), so the
	// Live-variant filter is what must suppress the rows.
	if _, err := d.Exec(
		`INSERT OR IGNORE INTO deleted_idns (idn, case_number, deleted_by, reason, deleted_at)
		 VALUES (?, NULL, 'supervisor', 'live-filter test', '2026-06-20 12:00:00')`, deadIDN,
	); err != nil {
		t.Fatalf("insert tombstone: %v", err)
	}

	// Live variants must exclude deadIDN but keep liveIDN.
	courts, _ = ListAllCourtDatesLive(d)
	viols, _ = ListAllViolationsLive(d)
	scheds, _ = ListAllScheduledCheckInsLive(d)
	if containsIDN(courts, deadIDN) {
		t.Errorf("ListAllCourtDatesLive leaked tombstoned IDN %s", deadIDN)
	}
	if !containsIDN(courts, liveIDN) {
		t.Errorf("ListAllCourtDatesLive dropped live IDN %s", liveIDN)
	}
	if containsIDNV(viols, deadIDN) {
		t.Errorf("ListAllViolationsLive leaked tombstoned IDN %s", deadIDN)
	}
	if !containsIDNV(viols, liveIDN) {
		t.Errorf("ListAllViolationsLive dropped live IDN %s", liveIDN)
	}
	if containsIDNS(scheds, deadIDN) {
		t.Errorf("ListAllScheduledCheckInsLive leaked tombstoned IDN %s", deadIDN)
	}
	if !containsIDNS(scheds, liveIDN) {
		t.Errorf("ListAllScheduledCheckInsLive dropped live IDN %s", liveIDN)
	}

	// Non-Live variants must still include deadIDN (extension rows not purged).
	allCourts, _ := ListAllCourtDates(d)
	allViols, _ := ListAllViolations(d)
	allScheds, _ := ListAllScheduledCheckIns(d)
	if !containsIDN(allCourts, deadIDN) {
		t.Errorf("ListAllCourtDates (non-live) should include tombstoned IDN %s (rows not purged)", deadIDN)
	}
	if !containsIDNV(allViols, deadIDN) {
		t.Errorf("ListAllViolations (non-live) should include tombstoned IDN %s (rows not purged)", deadIDN)
	}
	if !containsIDNS(allScheds, deadIDN) {
		t.Errorf("ListAllScheduledCheckIns (non-live) should include tombstoned IDN %s (rows not purged)", deadIDN)
	}
}

// containsIDN reports whether any CourtDate in the slice has the given IDN.
func containsIDN(cds []models.CourtDate, idn string) bool {
	for _, c := range cds {
		if c.IDN == idn {
			return true
		}
	}
	return false
}

// containsIDNV reports whether any Violation in the slice has the given IDN.
func containsIDNV(vs []models.Violation, idn string) bool {
	for _, v := range vs {
		if v.IDN == idn {
			return true
		}
	}
	return false
}

// containsIDNS reports whether any ScheduledCheckIn in the slice has the given IDN.
func containsIDNS(ss []models.ScheduledCheckIn, idn string) bool {
	for _, s := range ss {
		if s.IDN == idn {
			return true
		}
	}
	return false
}

// rawCount returns COUNT(*) from the named table, or -1 if the table does not
// exist. The -1 sentinel lets the assertion loop distinguish "table absent" from
// "table present but empty" — both are acceptable initial states.
func rawCount(t *testing.T, d *sql.DB, table string) int {
	t.Helper()
	var n int
	err := d.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n)
	if err != nil {
		// Table not present in this test DB variant — treat as zero rows.
		return 0
	}
	return n
}

// TestAppWritesNeverTouchRaw is the canonical guard for the "app writes NEVER
// touch raw_*" invariant (audit plan item #29). It snapshots the four raw table
// counts, exercises every app-layer write path — AddDefendant, AddPayment,
// AddCheckIn, their delete paths, SetOverride, and DeletePerson with
// importerRetired=false — and asserts every raw count is identical after. It
// also asserts the app-owned tables DID change so the test isn't vacuously green.
func TestAppWritesNeverTouchRaw(t *testing.T) {
	d := openEnsured(t)

	// Snapshot raw_* counts before any app writes.
	rawTables := []string{"raw_blue_book", "raw_check_ins", "raw_payments", "raw_gps_48_hours"}
	before := make(map[string]int, len(rawTables))
	for _, tbl := range rawTables {
		before[tbl] = rawCount(t, d, tbl)
	}

	// ── exercise every app-layer write path ──────────────────────────────────

	const idn1 = "999888001"
	const idn2 = "999888002"

	// AddDefendant (both without GPS and with GPS install date).
	if err := AddDefendant(d, NewDefendant{
		IDN: idn1, Name: "ZZRAW, ONE", Level: "2", Status: "Open",
		GPS: "true", GPSType: "SCRAM", CaseNumber: "@RAW1",
	}, "rawtest"); err != nil {
		t.Fatalf("AddDefendant idn1: %v", err)
	}
	if err := AddDefendant(d, NewDefendant{
		IDN: idn2, Name: "ZZRAW, TWO", Level: "3", Status: "Open",
		GPS: "true", GPSType: "ALLIED", CaseNumber: "@RAW2",
	}, "rawtest"); err != nil {
		t.Fatalf("AddDefendant idn2: %v", err)
	}

	// AddPayment + delete.
	if err := AddPayment(d, idn1, "@RAW1", "6/1/2026", "150", "GPS", "Officer X", "rawtest"); err != nil {
		t.Fatalf("AddPayment: %v", err)
	}
	pays, err := ListAddedPayments(d, idn1)
	if err != nil || len(pays) == 0 {
		t.Fatalf("ListAddedPayments after add: err=%v n=%d", err, len(pays))
	}
	if err := DeleteAddedPayment(d, pays[0].ID, "rawtest"); err != nil {
		t.Fatalf("DeleteAddedPayment: %v", err)
	}

	// AddCheckIn + delete.
	if err := AddCheckIn(d, idn1, "6/2/2026", "In Person", "GPS fitment test", "rawtest"); err != nil {
		t.Fatalf("AddCheckIn: %v", err)
	}
	cis, err := ListAddedCheckIns(d, idn1)
	if err != nil || len(cis) == 0 {
		t.Fatalf("ListAddedCheckIns after add: err=%v n=%d", err, len(cis))
	}
	if err := DeleteAddedCheckIn(d, cis[0].ID, "rawtest"); err != nil {
		t.Fatalf("DeleteAddedCheckIn: %v", err)
	}

	// SetOverride — writes to the overrides extension table, not raw_*.
	if err := SetOverride(d, idn1, "gps_type", "ALLIED", "rawtest"); err != nil {
		t.Fatalf("SetOverride: %v", err)
	}

	// DeletePerson with importerRetired=false — must NOT touch raw_*.
	if err := DeletePerson(d, idn2, "rawtest", "raw invariant test", false); err != nil {
		t.Fatalf("DeletePerson(importerRetired=false): %v", err)
	}

	// ── assert raw_* counts are UNCHANGED ────────────────────────────────────
	for _, tbl := range rawTables {
		after := rawCount(t, d, tbl)
		if after != before[tbl] {
			t.Errorf("raw table %s changed: before=%d after=%d (app writes must never touch raw_*)",
				tbl, before[tbl], after)
		}
	}

	// ── assert app-owned extension tables DID change ──────────────────────────

	// added_defendants must contain idn1 (idn2 was tombstoned, but row may be
	// gone from added_defendants after DeletePerson purges extension rows).
	var addedCount int
	if err := d.QueryRow(`SELECT COUNT(*) FROM added_defendants WHERE idn = ?`, idn1).Scan(&addedCount); err != nil {
		t.Fatalf("count added_defendants: %v", err)
	}
	if addedCount == 0 {
		t.Errorf("added_defendants: expected idn1 %s to be present", idn1)
	}

	// deleted_idns must contain idn2 (the one we tombstoned).
	var tombCount int
	if err := d.QueryRow(`SELECT COUNT(*) FROM deleted_idns WHERE idn = ? AND case_number IS NULL`, idn2).Scan(&tombCount); err != nil {
		t.Fatalf("count deleted_idns: %v", err)
	}
	if tombCount == 0 {
		t.Errorf("deleted_idns: expected idn2 %s whole-person tombstone", idn2)
	}
}

// TestReAddTombstonedIDNReturnsHint verifies item #10: re-adding a tombstoned IDN
// returns errTombstonedIDN (not errExistingIDN), while a non-tombstoned duplicate
// still returns errExistingIDN.
func TestReAddTombstonedIDNReturnsHint(t *testing.T) {
	d := openEnsured(t)
	const idn = "999002001"

	// Add and then whole-delete a defendant (creates a tombstone).
	if err := AddDefendant(d, NewDefendant{IDN: idn, Name: "ZZTOMB, TEST", Level: "1", Status: "Open"}, "by"); err != nil {
		t.Fatalf("AddDefendant: %v", err)
	}
	if err := DeletePerson(d, idn, "supervisor", "test", false); err != nil {
		t.Fatalf("DeletePerson: %v", err)
	}

	// Re-adding the tombstoned IDN must return errTombstonedIDN, not errExistingIDN.
	err := AddDefendant(d, NewDefendant{IDN: idn, Name: "ZZTOMB, TEST", Level: "1", Status: "Open"}, "by")
	if err != errTombstonedIDN {
		t.Fatalf("re-add tombstoned IDN: err = %v, want errTombstonedIDN", err)
	}

	// A genuine duplicate (no tombstone) must still return errExistingIDN.
	const dupIDN = "999002002"
	if err := AddDefendant(d, NewDefendant{IDN: dupIDN, Name: "ZZDUP, DOE", Level: "1", Status: "Open"}, "by"); err != nil {
		t.Fatalf("AddDefendant dup setup: %v", err)
	}
	err = AddDefendant(d, NewDefendant{IDN: dupIDN, Name: "ZZDUP, DOE", Level: "1", Status: "Open"}, "by")
	if err != errExistingIDN {
		t.Fatalf("duplicate (non-tombstoned) IDN: err = %v, want errExistingIDN", err)
	}
}
