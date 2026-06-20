package compute

import (
	"testing"
	"time"
)

func cp(start, end string) CustodyPeriod {
	s, sok := ParseDay(start)
	e, eok := ParseDay(end)
	return CustodyPeriod{Start: s, End: e, StartOK: sok, EndOK: eok}
}

func TestCustodyDaysInWindow(t *testing.T) {
	win := func() (time.Time, time.Time) { return Noon(2026, 1, 1), Noon(2026, 1, 31) }
	ws, we := win()

	// In custody Jan 5 → back on GPS Jan 10: days 5,6,7,8,9 excluded (10 is billed) = 5.
	if got := CustodyDaysInWindow([]CustodyPeriod{cp("2026-01-05", "2026-01-10")}, ws, we); got != 5 {
		t.Fatalf("basic = %d, want 5", got)
	}
	// Open-ended (still in custody) from Jan 20: Jan 20..31 inclusive = 12 days.
	if got := CustodyDaysInWindow([]CustodyPeriod{cp("2026-01-20", "")}, ws, we); got != 12 {
		t.Fatalf("open-ended = %d, want 12", got)
	}
	// Reinstalled the same day as release: end == start+1 → 1 day excluded, release billed.
	if got := CustodyDaysInWindow([]CustodyPeriod{cp("2026-01-05", "2026-01-06")}, ws, we); got != 1 {
		t.Fatalf("one night = %d, want 1", got)
	}
	// Clamped to the window: starts before Jan 1.
	if got := CustodyDaysInWindow([]CustodyPeriod{cp("2025-12-20", "2026-01-06")}, ws, we); got != 5 {
		t.Fatalf("clamp-start = %d, want 5 (Jan1..5)", got)
	}
	// Overlapping periods are merged, not double-counted.
	overlap := []CustodyPeriod{cp("2026-01-05", "2026-01-10"), cp("2026-01-08", "2026-01-12")}
	if got := CustodyDaysInWindow(overlap, ws, we); got != 7 { // Jan5..11 = 7
		t.Fatalf("overlap = %d, want 7", got)
	}
	// None.
	if got := CustodyDaysInWindow(nil, ws, we); got != 0 {
		t.Fatalf("nil = %d, want 0", got)
	}
}

// ComputeGPS must subtract custody days from the billed/owed side and expose the
// billable-day count, while the reinstall day stays billable.
func TestComputeGPSCustodyReducesOwed(t *testing.T) {
	track := Noon(2026, 2, 1)
	base := Client{
		IDN: "1", GpsActive: true, GpsType: "ALLIED", GpInstall: "2026-01-01", // $8/day
	}
	// No custody: Jan 1..Feb 1 inclusive = 32 days × $8 = $256.
	g0 := ComputeGPS(base, track, nil, "")
	if g0.TotalOwedDollars == nil || *g0.TotalOwedDollars != 256 {
		t.Fatalf("baseline owed = %v, want 256", g0.TotalOwedDollars)
	}

	// In custody Jan 10 → back Jan 20 = 10 days excluded → 22 billable × $8 = $176.
	c := base
	c.Custody = []CustodyPeriod{cp("2026-01-10", "2026-01-20")}
	g := ComputeGPS(c, track, nil, "")
	if g.CustodyDays == nil || *g.CustodyDays != 10 {
		t.Fatalf("custodyDays = %v, want 10", g.CustodyDays)
	}
	if g.BillableDays == nil || *g.BillableDays != 22 {
		t.Fatalf("billableDays = %v, want 22", g.BillableDays)
	}
	if g.TotalOwedDollars == nil || *g.TotalOwedDollars != 176 {
		t.Fatalf("owed with custody = %v, want 176", g.TotalOwedDollars)
	}
}
