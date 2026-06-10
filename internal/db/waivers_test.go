package db

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"pretrial-knoxc/internal/compute"
)

// TestFeeWaiverLifecycle covers grant → BuildClients gp_notes splice (so
// compute.IsFeesWaived lights up) → re-grant (upsert) → clear → audit trail.
func TestFeeWaiverLifecycle(t *testing.T) {
	d := openEnsured(t)
	const idn = "999000333"
	const sup = "alexander.bentley@knoxsheriff.org"

	if err := SetFeeWaiver(d, "", "reason", sup); err != errEmptyField {
		t.Fatalf("missing idn: err = %v, want errEmptyField", err)
	}
	if err := SetFeeWaiver(d, idn, "reason", ""); err != errEmptyField {
		t.Fatalf("missing user: err = %v, want errEmptyField", err)
	}

	// A GPS client with no vendor waiver text.
	if err := AddDefendant(d, NewDefendant{
		IDN: idn, Name: "ZZWAIVE, WANDA", Level: "3", Status: "Open",
		GPS: "true", GPSType: "ALLIED",
	}, sup); err != nil {
		t.Fatalf("AddDefendant: %v", err)
	}
	if HasFeeWaiver(d, idn) {
		t.Fatal("HasFeeWaiver true before any waiver")
	}

	if err := SetFeeWaiver(d, idn, "indigency — approved by Judge Roe", sup); err != nil {
		t.Fatalf("SetFeeWaiver: %v", err)
	}
	if !HasFeeWaiver(d, idn) {
		t.Fatal("HasFeeWaiver false after grant")
	}
	c := waiverClient(t, d, idn)
	if !compute.IsFeesWaived(c.GpNotes) {
		t.Fatalf("IsFeesWaived false after grant; GpNotes = %q", c.GpNotes)
	}
	if !strings.Contains(c.GpNotes, "GPS fees waived (app — Alexander Bentley") ||
		!strings.Contains(c.GpNotes, "indigency — approved by Judge Roe") {
		t.Errorf("marker missing officer/reason: %q", c.GpNotes)
	}

	// Re-grant replaces the reason — still one row, new text.
	if err := SetFeeWaiver(d, idn, "updated reason", sup); err != nil {
		t.Fatalf("SetFeeWaiver upsert: %v", err)
	}
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM fee_waivers WHERE idn = ?`, idn).Scan(&n); err != nil || n != 1 {
		t.Fatalf("rows after upsert = %d (err %v), want 1", n, err)
	}
	if c := waiverClient(t, d, idn); !strings.Contains(c.GpNotes, "updated reason") {
		t.Errorf("upsert did not replace reason: %q", c.GpNotes)
	}

	// Clear → flag and splice both gone; clearing again is a quiet no-op.
	if err := ClearFeeWaiver(d, idn, sup); err != nil {
		t.Fatalf("ClearFeeWaiver: %v", err)
	}
	if HasFeeWaiver(d, idn) {
		t.Fatal("HasFeeWaiver true after clear")
	}
	if c := waiverClient(t, d, idn); compute.IsFeesWaived(c.GpNotes) {
		t.Errorf("IsFeesWaived still true after clear: %q", c.GpNotes)
	}
	if err := ClearFeeWaiver(d, idn, sup); err != nil {
		t.Fatalf("double clear errored: %v", err)
	}

	// Audit breadcrumbs: 2 grants, 1 remove (the no-op clear writes nothing).
	audit, _ := ListAudit(d, "", 100)
	var adds, removes int
	for _, a := range audit {
		if a.Table != "fee_waivers" {
			continue
		}
		switch a.Action {
		case "waiver_add":
			adds++
		case "waiver_remove":
			removes++
		}
	}
	if adds != 2 || removes != 1 {
		t.Errorf("audit: waiver_add=%d waiver_remove=%d, want 2/1", adds, removes)
	}
}

// waiverClient rebuilds and returns the test client's first case row.
func waiverClient(t *testing.T, d *sql.DB, idn string) *compute.Client {
	t.Helper()
	clients, err := BuildClients(d, time.Now())
	if err != nil {
		t.Fatalf("BuildClients: %v", err)
	}
	cases := clients[idn]
	if len(cases) == 0 {
		t.Fatalf("client %s missing from BuildClients", idn)
	}
	return cases[0]
}

// TestFeeWaiverInLookupFeed: the tracker feed's gp dataset carries the marker
// in its Notes column, so the bundled tracker's own isFeesWaived matches.
func TestFeeWaiverInLookupFeed(t *testing.T) {
	d := openEnsured(t)
	const sup = "alexander.bentley@knoxsheriff.org"

	// Any fixture client with a GPS row.
	var idn string
	if err := d.QueryRow(
		`SELECT TRIM(idn) FROM raw_gps_48_hours WHERE TRIM(COALESCE(idn,'')) <> '' LIMIT 1`).Scan(&idn); err != nil {
		t.Skipf("no GPS rows in offline DB: %v", err)
	}
	if err := SetFeeWaiver(d, idn, "feed test", sup); err != nil {
		t.Fatalf("SetFeeWaiver: %v", err)
	}

	sets, err := LookupDatasets(d)
	if err != nil {
		t.Fatalf("LookupDatasets: %v", err)
	}
	found := false
	for _, row := range sets["gp"].([]map[string]string) {
		if strings.TrimSpace(row["IDN"]) != idn {
			continue
		}
		found = true
		if !strings.Contains(row["Notes"], "GPS fees waived (app") {
			t.Errorf("gp Notes missing waiver marker: %q", row["Notes"])
		}
	}
	if !found {
		t.Fatalf("idn %s not found in gp feed", idn)
	}
}

// TestFeeWaiversPurgedOnPersonDelete: a whole-person delete removes the waiver
// row (fee_waivers is in extensionTablesByIDN).
func TestFeeWaiversPurgedOnPersonDelete(t *testing.T) {
	d := openEnsured(t)
	const idn = "999000444"
	const sup = "alexander.bentley@knoxsheriff.org"
	if err := AddDefendant(d, NewDefendant{IDN: idn, Name: "ZZPURGE, PETE", Level: "2", Status: "Open"}, sup); err != nil {
		t.Fatalf("AddDefendant: %v", err)
	}
	if err := SetFeeWaiver(d, idn, "", sup); err != nil {
		t.Fatalf("SetFeeWaiver: %v", err)
	}
	if err := DeletePerson(d, idn, sup, "purge test", false); err != nil {
		t.Fatalf("DeletePerson: %v", err)
	}
	if HasFeeWaiver(d, idn) {
		t.Error("waiver survived whole-person delete")
	}
}
