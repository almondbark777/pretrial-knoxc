package db

import (
	"database/sql"
	"testing"
	"time"

	"pretrial-knoxc/internal/compute"
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
