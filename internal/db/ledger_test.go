package db

import "testing"

// ClientLedger must surface app-entered check-ins/payments (with Source "App" and
// the per-check-in note) for a brand-new client that has no imported history.
func TestClientLedgerAppRows(t *testing.T) {
	d := openEnsured(t)
	idn := "999777001"
	if err := AddDefendant(d, NewDefendant{IDN: idn, Name: "ZZLEDGER, LEE", Level: "2", Status: "Open", GPS: "true", GPSType: "SCRAM"}, "by"); err != nil {
		t.Fatalf("AddDefendant: %v", err)
	}
	if err := AddPayment(d, idn, "@1", "5/1/2026", "120", "SCRAM", "Officer X", "by"); err != nil {
		t.Fatalf("AddPayment: %v", err)
	}
	if err := AddPayment(d, idn, "@1", "5/8/2026", "60", "SCRAM", "Officer X", "by"); err != nil {
		t.Fatalf("AddPayment: %v", err)
	}
	if err := AddCheckIn(d, idn, "5/2/2026", "In Person", "fitment note", "jane.doe@knoxsheriff.org"); err != nil {
		t.Fatalf("AddCheckIn: %v", err)
	}

	lg, err := ClientLedger(d, idn)
	if err != nil {
		t.Fatalf("ClientLedger: %v", err)
	}
	if len(lg.CheckIns) != 1 {
		t.Fatalf("check-ins = %d, want 1: %+v", len(lg.CheckIns), lg.CheckIns)
	}
	if lg.CheckIns[0].Source != "App" || lg.CheckIns[0].Note != "fitment note" || lg.CheckIns[0].Type != "In Person" {
		t.Fatalf("app check-in wrong: %+v", lg.CheckIns[0])
	}
	if len(lg.Payments) != 2 {
		t.Fatalf("payments = %d, want 2", len(lg.Payments))
	}
	// Newest first: 5/8 before 5/1.
	if lg.Payments[0].Date != "May 8, 2026" || lg.Payments[1].Date != "May 1, 2026" {
		t.Fatalf("payments not newest-first: %+v", lg.Payments)
	}
	for _, p := range lg.Payments {
		if p.Source != "App" {
			t.Fatalf("payment Source = %q, want App", p.Source)
		}
	}
}

// ClientLedger must also surface IMPORTED history (raw_check_ins) for a client that
// already has check-ins in the offline DB, tagged Source "Imported".
func TestClientLedgerImportedRows(t *testing.T) {
	d := openEnsured(t)
	var idn string
	if err := d.QueryRow("SELECT idn FROM raw_check_ins WHERE TRIM(COALESCE(idn,'')) <> '' LIMIT 1").Scan(&idn); err != nil {
		t.Skipf("offline DB has no raw_check_ins rows: %v", err)
	}
	lg, err := ClientLedger(d, idn)
	if err != nil {
		t.Fatalf("ClientLedger: %v", err)
	}
	if len(lg.CheckIns) == 0 {
		t.Fatalf("expected imported check-ins for idn %s", idn)
	}
	for _, c := range lg.CheckIns {
		if c.Source != "Imported" {
			t.Fatalf("imported check-in Source = %q, want Imported", c.Source)
		}
	}
}
