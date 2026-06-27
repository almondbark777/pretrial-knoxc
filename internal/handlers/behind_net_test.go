package handlers

import (
	"testing"

	"pretrial-knoxc/internal/compute"
)

// A GPS payment tagged with several case #s must be credited ONCE across an
// IDN's GPS cases, not once per case (the per-case sum double-counted it — audit
// 2026-06-27). Here owed across two cases is $2,432 and only $1,300 was paid, so
// the person IS behind; the old double-count ($2,600 paid) would have flipped
// them to a surplus and wrongly dropped them off the roster.
func TestBehindRosterPaidCountedOnceAcrossCases(t *testing.T) {
	track := compute.Noon(2026, 6, 1) // Jan1..Jun1 = 152 days × $8 = $1,216 / case
	pay := compute.Payment{D: compute.Noon(2026, 1, 1), DOK: true, Amt: 1300, Type: "ALLIED", Case: "@A, @B"}
	mk := func(caseNo string) *compute.Client {
		return &compute.Client{
			IDN: "1", Name: "DOUBLE, DEE", Status: "Open", GpsActive: true,
			GpsType: "ALLIED", GpInstall: "2026-01-01", CaseNo: caseNo,
			Payments: []compute.Payment{pay},
		}
	}
	clients := map[string][]*compute.Client{"1": {mk("@A"), mk("@B")}}

	rows := behindRoster(clients, track)
	if len(rows) != 1 {
		t.Fatalf("want 1 behind row (paid once = $1,300 < owed $2,432), got %d", len(rows))
	}
	if rows[0].Paid != 1300 {
		t.Fatalf("Paid = %v, want 1300 (counted once, not 2600)", rows[0].Paid)
	}
	if rows[0].Owed != 2432 {
		t.Fatalf("Owed = %v, want 2432 (two case windows)", rows[0].Owed)
	}
	if rows[0].Amount != 1300-2432 {
		t.Fatalf("Amount(surplus) = %v, want %v", rows[0].Amount, 1300.0-2432.0)
	}
}

// Two cases, fully paid once across both → NOT behind (and the old double-count
// must not be what saves them; here a single count already clears it).
func TestBehindRosterTwoCasesPaidUp(t *testing.T) {
	track := compute.Noon(2026, 6, 1)
	pay := compute.Payment{D: compute.Noon(2026, 1, 1), DOK: true, Amt: 2432, Type: "ALLIED", Case: "@A, @B"}
	mk := func(caseNo string) *compute.Client {
		return &compute.Client{
			IDN: "2", Name: "PAID, PAT", Status: "Open", GpsActive: true,
			GpsType: "ALLIED", GpInstall: "2026-01-01", CaseNo: caseNo,
			Payments: []compute.Payment{pay},
		}
	}
	clients := map[string][]*compute.Client{"2": {mk("@A"), mk("@B")}}
	if rows := behindRoster(clients, track); len(rows) != 0 {
		t.Fatalf("want 0 behind rows (paid $2,432 == owed), got %d", len(rows))
	}
}
