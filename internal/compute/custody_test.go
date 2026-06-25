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

	// In custody Jan 5 → back on GPS Jan 10: BOTH endpoints billed, only the full
	// days between (6,7,8,9) are excluded = 4.
	if got := CustodyDaysInWindow([]CustodyPeriod{cp("2026-01-05", "2026-01-10")}, ws, we); got != 4 {
		t.Fatalf("basic = %d, want 4", got)
	}
	// Open-ended (still in custody) from Jan 20: take-off day Jan 20 billed, Jan
	// 21..31 inclusive excluded = 11 days.
	if got := CustodyDaysInWindow([]CustodyPeriod{cp("2026-01-20", "")}, ws, we); got != 11 {
		t.Fatalf("open-ended = %d, want 11", got)
	}
	// Reinstalled the day after release (end == start+1): both days billed, nothing
	// in between → 0 excluded.
	if got := CustodyDaysInWindow([]CustodyPeriod{cp("2026-01-05", "2026-01-06")}, ws, we); got != 0 {
		t.Fatalf("one night = %d, want 0", got)
	}
	// Clamped to the window: take-off day Dec 20 is outside the window, so exclusion
	// starts at the window edge → Jan1..5 = 5 (Jan 6 reinstall billed).
	if got := CustodyDaysInWindow([]CustodyPeriod{cp("2025-12-20", "2026-01-06")}, ws, we); got != 5 {
		t.Fatalf("clamp-start = %d, want 5 (Jan1..5)", got)
	}
	// Overlapping periods are merged, not double-counted.
	overlap := []CustodyPeriod{cp("2026-01-05", "2026-01-10"), cp("2026-01-08", "2026-01-12")}
	if got := CustodyDaysInWindow(overlap, ws, we); got != 6 { // Jan6..11 = 6
		t.Fatalf("overlap = %d, want 6", got)
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

	// In custody Jan 10 → back Jan 20: both endpoints billed, only Jan 11..19 (9
	// days) excluded → 23 billable × $8 = $184.
	c := base
	c.Custody = []CustodyPeriod{cp("2026-01-10", "2026-01-20")}
	g := ComputeGPS(c, track, nil, "")
	if g.CustodyDays == nil || *g.CustodyDays != 9 {
		t.Fatalf("custodyDays = %v, want 9", g.CustodyDays)
	}
	if g.BillableDays == nil || *g.BillableDays != 23 {
		t.Fatalf("billableDays = %v, want 23", g.BillableDays)
	}
	if g.TotalOwedDollars == nil || *g.TotalOwedDollars != 184 {
		t.Fatalf("owed with custody = %v, want 184", g.TotalOwedDollars)
	}
}

// Across a vendor switch the custody credit must be split at switchD: pre-switch
// days credited at rate1, post-switch (incl. switchD) at rate2 — mirroring the
// totalOwed switch math (14*15 + 23 + 16*8 = 361). Crediting the whole span at a
// single rate would over/under-state owed. switchD must be tiled exactly once.
func TestComputeGPSCustodyRateSplitAcrossSwitch(t *testing.T) {
	track := Noon(2026, 1, 31)
	base := Client{
		IDN: "1", GpsActive: true,
		GpsType:        "SCRAM", // rate1 = $15
		GpInstall:      "2026-01-01",
		GpSwitchedTo:   "ALLIED", // rate2 = $8
		GpSwitchedDate: "2026-01-15",
	}

	// Baseline (no custody): 14*15 + 23 + 16*8 = 361.
	g0 := ComputeGPS(base, track, nil, "")
	if !g0.HasSwitch || g0.TotalOwedDollars == nil || *g0.TotalOwedDollars != 361 {
		t.Fatalf("baseline owed = %v hasSwitch=%v want 361/true", g0.TotalOwedDollars, g0.HasSwitch)
	}

	// Custody entirely BEFORE the switch: Jan 5→10 excludes only 6,7,8,9 (4 days,
	// both endpoints billed), all pre-switch → credited at $15 → 361 − 4*15 = 301.
	cb := base
	cb.Custody = []CustodyPeriod{cp("2026-01-05", "2026-01-10")}
	gb := ComputeGPS(cb, track, nil, "")
	if gb.CustodyDays == nil || *gb.CustodyDays != 4 {
		t.Fatalf("before-switch custodyDays = %v want 4", gb.CustodyDays)
	}
	if gb.TotalOwedDollars == nil || *gb.TotalOwedDollars != 301 {
		t.Fatalf("before-switch owed = %v want 301 (361 − 4*15)", gb.TotalOwedDollars)
	}

	// Custody entirely AFTER the switch: Jan 20→25 excludes only 21..24 (4 days),
	// all post-switch → credited at $8 → 361 − 4*8 = 329.
	ca := base
	ca.Custody = []CustodyPeriod{cp("2026-01-20", "2026-01-25")}
	ga := ComputeGPS(ca, track, nil, "")
	if ga.CustodyDays == nil || *ga.CustodyDays != 4 {
		t.Fatalf("after-switch custodyDays = %v want 4", ga.CustodyDays)
	}
	if ga.TotalOwedDollars == nil || *ga.TotalOwedDollars != 329 {
		t.Fatalf("after-switch owed = %v want 329 (361 − 4*8)", ga.TotalOwedDollars)
	}

	// Custody SPANNING the switch: Jan 12→18 excludes 13..17 (5 days; take-off Jan
	// 12 is billed). The three sub-windows tile the span exactly once: pre [Jan1,
	// Jan14] = 13,14 (2 days @ $15); the switch day Jan 15 was billed the flat $23
	// premium and is itself a custody day so it's credited $23; post [Jan16, Jan31]
	// = 16,17 (2 days @ $8). Credit = 2*15 + 23 + 2*8 = 30 + 23 + 16 = 69 → 361 − 69 = 292.
	cs := base
	cs.Custody = []CustodyPeriod{cp("2026-01-12", "2026-01-18")}
	gs := ComputeGPS(cs, track, nil, "")
	if gs.CustodyDays == nil || *gs.CustodyDays != 5 {
		t.Fatalf("spanning custodyDays = %v want 5", gs.CustodyDays)
	}
	if gs.TotalOwedDollars == nil || *gs.TotalOwedDollars != 292 {
		t.Fatalf("spanning owed = %v want 292 (361 − (2*15 + 23 + 2*8))", gs.TotalOwedDollars)
	}
}
