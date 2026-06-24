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

// TestClientLedgerTombstones covers tombstone suppression on the record-page
// ledger (item #8):
//
//	(a) a whole-person tombstone yields an empty ledger; and
//	(b) a per-case tombstone drops ONLY that case's payments (raw + app), while
//	    the other case's payments AND all check-ins (person-scoped, never
//	    case-filtered) remain.
func TestClientLedgerTombstones(t *testing.T) {
	// ── (a) whole-person suppression ──
	t.Run("whole_person", func(t *testing.T) {
		d := openEnsured(t)
		idn := "999777010"
		if err := AddDefendant(d, NewDefendant{IDN: idn, Name: "ZZWHOLE, WANDA", Level: "2", Status: "Open", GPS: "true", GPSType: "SCRAM"}, "by"); err != nil {
			t.Fatalf("AddDefendant: %v", err)
		}
		if err := AddPayment(d, idn, "@1", "5/1/2026", "120", "SCRAM", "Officer X", "by"); err != nil {
			t.Fatalf("AddPayment: %v", err)
		}
		if err := AddCheckIn(d, idn, "5/2/2026", "In Person", "note", "jane.doe@knoxsheriff.org"); err != nil {
			t.Fatalf("AddCheckIn: %v", err)
		}
		// Plant a whole-person tombstone directly (NULL case_number).
		if _, err := d.Exec(`INSERT INTO deleted_idns (idn, case_number) VALUES (?, NULL)`, idn); err != nil {
			t.Fatalf("plant whole tombstone: %v", err)
		}
		lg, err := ClientLedger(d, idn)
		if err != nil {
			t.Fatalf("ClientLedger: %v", err)
		}
		if len(lg.CheckIns) != 0 || len(lg.Payments) != 0 {
			t.Fatalf("whole-person tombstone should empty the ledger, got %d check-ins / %d payments", len(lg.CheckIns), len(lg.Payments))
		}
	})

	// ── (b) per-case suppression ──
	t.Run("per_case", func(t *testing.T) {
		d := openEnsured(t)
		idn := "999777011"
		if err := AddDefendant(d, NewDefendant{IDN: idn, Name: "ZZCASE, CARL", Level: "2", Status: "Open", GPS: "true", GPSType: "SCRAM"}, "by"); err != nil {
			t.Fatalf("AddDefendant: %v", err)
		}
		// App-entered payments on two different cases.
		if err := AddPayment(d, idn, "@AAA", "5/1/2026", "100", "SCRAM", "Officer X", "by"); err != nil {
			t.Fatalf("AddPayment @AAA: %v", err)
		}
		if err := AddPayment(d, idn, "@BBB", "5/3/2026", "200", "SCRAM", "Officer X", "by"); err != nil {
			t.Fatalf("AddPayment @BBB: %v", err)
		}
		// Imported payments on the same two cases (exercise the raw loop too).
		if _, err := d.Exec(
			`INSERT INTO raw_payments (idn, case_number, payment_date, payment_amount, payment_type) VALUES (?, ?, ?, ?, ?)`,
			idn, "@AAA", "4/1/2026", "50", "SCRAM"); err != nil {
			t.Fatalf("insert raw payment @AAA: %v", err)
		}
		if _, err := d.Exec(
			`INSERT INTO raw_payments (idn, case_number, payment_date, payment_amount, payment_type) VALUES (?, ?, ?, ?, ?)`,
			idn, "@BBB", "4/3/2026", "60", "SCRAM"); err != nil {
			t.Fatalf("insert raw payment @BBB: %v", err)
		}
		// Two check-ins (person-scoped — must survive a per-case delete).
		if err := AddCheckIn(d, idn, "5/2/2026", "In Person", "n1", "jane.doe@knoxsheriff.org"); err != nil {
			t.Fatalf("AddCheckIn 1: %v", err)
		}
		if err := AddCheckIn(d, idn, "5/4/2026", "Phone", "n2", "jane.doe@knoxsheriff.org"); err != nil {
			t.Fatalf("AddCheckIn 2: %v", err)
		}

		// Tombstone ONLY case @AAA (per-case).
		if _, err := d.Exec(`INSERT INTO deleted_idns (idn, case_number) VALUES (?, ?)`, idn, "@AAA"); err != nil {
			t.Fatalf("plant per-case tombstone: %v", err)
		}

		lg, err := ClientLedger(d, idn)
		if err != nil {
			t.Fatalf("ClientLedger: %v", err)
		}

		// Check-ins: both remain (never case-filtered).
		if len(lg.CheckIns) != 2 {
			t.Fatalf("check-ins = %d, want 2 (per-case delete must not drop check-ins): %+v", len(lg.CheckIns), lg.CheckIns)
		}

		// Payments: only the two @BBB payments (1 raw + 1 app) remain; both @AAA dropped.
		if len(lg.Payments) != 2 {
			t.Fatalf("payments = %d, want 2 (only @BBB): %+v", len(lg.Payments), lg.Payments)
		}
		for _, p := range lg.Payments {
			if p.Case == "@AAA" {
				t.Fatalf("payment for tombstoned case @AAA leaked: %+v", p)
			}
			if p.Case != "@BBB" {
				t.Fatalf("unexpected payment case %q (want @BBB): %+v", p.Case, p)
			}
		}
	})
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
