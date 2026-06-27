package handlers

import (
	"testing"

	"pretrial-knoxc/internal/compute"
	"pretrial-knoxc/internal/db"
	"pretrial-knoxc/internal/models"
)

// The record's GPS card must agree with the compliance roster for multi-GPS-case
// clients. The per-case card shows only the selected case (here a $84 surplus),
// but the roster nets across both cases — owed per case ($2,432), paid counted
// ONCE ($1,300, not $2,600) — so the client is actually behind. netGPS is the
// shared math behind both; this mirrors behind_net_test.go on the record side.
func TestNetGPSPaidCountedOnceAcrossCases(t *testing.T) {
	track := compute.Noon(2026, 6, 1) // Jan1..Jun1 = 152 days × $8 = $1,216 / case
	pay := compute.Payment{D: compute.Noon(2026, 1, 1), DOK: true, Amt: 1300, Type: "ALLIED", Case: "@A, @B"}
	mk := func(caseNo string) *compute.Client {
		return &compute.Client{
			IDN: "1", Name: "DOUBLE, DEE", Status: "Open", GpsActive: true,
			GpsType: "ALLIED", GpInstall: "2026-01-01", CaseNo: caseNo,
			Payments: []compute.Payment{pay},
		}
	}
	gpsCases := []*compute.Client{mk("@A"), mk("@B")}

	n := netGPS(gpsCases, openRep(gpsCases), track)
	if n.Cases != 2 {
		t.Fatalf("Cases = %d, want 2", n.Cases)
	}
	if !n.HaveOwed {
		t.Fatal("HaveOwed = false, want true")
	}
	if n.Owed != 2432 {
		t.Fatalf("Owed = %v, want 2432 (two case windows)", n.Owed)
	}
	if n.Paid != 1300 {
		t.Fatalf("Paid = %v, want 1300 (counted once, not 2600)", n.Paid)
	}
	if n.Surplus != 1300-2432 {
		t.Fatalf("Surplus = %v, want %v", n.Surplus, 1300.0-2432.0)
	}
}

// A multi-GPS-case client's record surfaces the NET (GpsNetShow) so the card
// agrees with the roster, while the per-case GPS view (rec.GPS, default filter)
// still shows the selected-case surplus. Single-GPS-case clients are unaffected.
func TestConsoleRecordSurfacesNetForMultiGPSCase(t *testing.T) {
	track := compute.Noon(2026, 6, 1)
	pay := compute.Payment{D: compute.Noon(2026, 1, 1), DOK: true, Amt: 1300, Type: "ALLIED", Case: "@A, @B"}
	mk := func(caseNo string) *compute.Client {
		return &compute.Client{
			IDN: "1", Name: "DOUBLE, DEE", Status: "Open", GpsActive: true,
			GpsType: "ALLIED", GpInstall: "2026-01-01", CaseNo: caseNo,
			Payments: []compute.Payment{pay},
		}
	}
	allCases := []*compute.Client{mk("@A"), mk("@B")}
	rep := openRep(allCases)

	// Default record view: no ?case=, so caseFilter="" → per-case card sums ALL
	// payments ($1,300) against ONE case window ($1,216) = +$84 surplus.
	gps := compute.ComputeGPS(*rep, track, nil, "")
	ci := compute.ComputeCheckIns(*rep, track)
	ptr := compute.ComputePTRFees(*rep, track, "")
	rec := consoleRecord(rep, allCases, track, ci, ptr, gps, models.DefendantExtras{}, db.Ledger{}, nil, "")

	if !rec.GpsNetShow {
		t.Fatal("GpsNetShow = false, want true for a multi-GPS-case client")
	}
	if rec.GpsNetCases != 2 {
		t.Fatalf("GpsNetCases = %d, want 2", rec.GpsNetCases)
	}
	if rec.GpsNetOwed != 2432 || rec.GpsNetPaid != 1300 {
		t.Fatalf("net owed/paid = %v/%v, want 2432/1300", rec.GpsNetOwed, rec.GpsNetPaid)
	}
	if rec.GpsNetSurplus != 1300-2432 || rec.GpsNetCovered {
		t.Fatalf("net surplus = %v covered = %v, want -1132 / false (behind)", rec.GpsNetSurplus, rec.GpsNetCovered)
	}
	// The per-case card still shows the selected case's positive surplus — the very
	// divergence this net block explains.
	if gps.SurplusDollars == nil || *gps.SurplusDollars <= 0 {
		t.Fatalf("per-case SurplusDollars = %v, want a positive surplus (divergence)", gps.SurplusDollars)
	}
}

// A single-GPS-case client never shows the net block (nothing to reconcile).
func TestConsoleRecordNoNetForSingleGPSCase(t *testing.T) {
	track := compute.Noon(2026, 6, 1)
	c := &compute.Client{
		IDN: "2", Name: "SOLO, SAM", Status: "Open", GpsActive: true,
		GpsType: "ALLIED", GpInstall: "2026-01-01", CaseNo: "@A",
		Payments: []compute.Payment{{D: compute.Noon(2026, 1, 1), DOK: true, Amt: 500, Type: "ALLIED", Case: "@A"}},
	}
	allCases := []*compute.Client{c}
	gps := compute.ComputeGPS(*c, track, nil, "")
	ci := compute.ComputeCheckIns(*c, track)
	ptr := compute.ComputePTRFees(*c, track, "")
	rec := consoleRecord(c, allCases, track, ci, ptr, gps, models.DefendantExtras{}, db.Ledger{}, nil, "")
	if rec.GpsNetShow {
		t.Fatal("GpsNetShow = true, want false for a single-GPS-case client")
	}
}
