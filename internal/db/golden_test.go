package db

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pretrial-knoxc/internal/compute"
)

// rep mirrors handlers.openRep: first open-status case, else the first. Used so
// the golden assertions don't depend on blue_book row order for multi-case IDNs.
func rep(cs []*compute.Client) *compute.Client {
	for _, c := range cs {
		if strings.Contains(strings.ToLower(c.Status), "open") {
			return c
		}
	}
	if len(cs) > 0 {
		return cs[0]
	}
	return nil
}

// TestGoldenAgainstRealDB runs the full native-SQLite data layer + server-side
// math against the offline DB copy (db/kh222.db) and asserts the PHASE_2 §4
// golden values at trackDate 2026-05-30. This proves the Go pipeline
// (raw_* -> BuildClients -> compute*) reproduces the canonical JS, end to end.
//
// Skips automatically if the DB file isn't present (e.g. CI without data).
func TestGoldenAgainstRealDB(t *testing.T) {
	path := filepath.Join("..", "..", "db", "kh222.db")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("offline DB not present (%v) — skipping DB-backed golden test", err)
	}
	d, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer d.Close()

	track := compute.Noon(2026, 5, 30)
	clients, err := BuildClients(d, track)
	if err != nil {
		t.Fatalf("BuildClients: %v", err)
	}

	get := func(idn string) compute.Client {
		cs := clients[idn]
		if len(cs) == 0 {
			t.Fatalf("IDN %s not found in DB", idn)
		}
		return *rep(cs)
	}

	// JONES — L1
	if r := compute.ComputePTRFees(get("1704942"), track, ""); r.TotalOwed != 20 {
		t.Errorf("JONES L1 PTR owed=%d want 20", r.TotalOwed)
	}
	// REASONOVER — L2: 5 windows, 3 missed, $100
	{
		c := get("1426070")
		ci := compute.ComputeCheckIns(c, track)
		ptr := compute.ComputePTRFees(c, track, "")
		if len(ci.Windows) != 5 || len(ci.Missed) != 3 || ptr.TotalOwed != 100 {
			t.Errorf("REASONOVER windows=%d missed=%d owed=%d want 5/3/100",
				len(ci.Windows), len(ci.Missed), ptr.TotalOwed)
		}
	}
	// HANCOCK — L3: 5 windows, 5 missed, $40
	{
		c := get("1704989")
		ci := compute.ComputeCheckIns(c, track)
		ptr := compute.ComputePTRFees(c, track, "")
		if len(ci.Windows) != 5 || len(ci.Missed) != 5 || ptr.TotalOwed != 40 {
			t.Errorf("HANCOCK windows=%d missed=%d owed=%d want 5/5/40",
				len(ci.Windows), len(ci.Missed), ptr.TotalOwed)
		}
	}
	// COLLINS — closed L2: 1 window, 0 missed, $20
	{
		c := get("1704603")
		ci := compute.ComputeCheckIns(c, track)
		ptr := compute.ComputePTRFees(c, track, "")
		if len(ci.Windows) != 1 || len(ci.Missed) != 0 || ptr.TotalOwed != 20 {
			t.Errorf("COLLINS windows=%d missed=%d owed=%d want 1/0/20",
				len(ci.Windows), len(ci.Missed), ptr.TotalOwed)
		}
	}
	// AGUILAR — SCRAM: 41 days, $615, surplusDays -41
	{
		g := compute.ComputeGPS(get("1386687"), track, nil, "")
		if g.Vendor != "SCRAM" || g.DaysActive == nil || *g.DaysActive != 41 ||
			g.TotalOwedDollars == nil || *g.TotalOwedDollars != 615 ||
			g.SurplusDays == nil || *g.SurplusDays != -41 {
			t.Errorf("AGUILAR GPS = vendor %s days %v owed %v surplusDays %v want SCRAM/41/615/-41",
				g.Vendor, deref(g.DaysActive), derefF(g.TotalOwedDollars), deref(g.SurplusDays))
		}
	}
	// PIETY — ALLIED: 33 days, $264, surplusDays -33
	{
		g := compute.ComputeGPS(get("1340291"), track, nil, "")
		if g.Vendor != "ALLIED" || g.DaysActive == nil || *g.DaysActive != 33 ||
			g.TotalOwedDollars == nil || *g.TotalOwedDollars != 264 ||
			g.SurplusDays == nil || *g.SurplusDays != -33 {
			t.Errorf("PIETY GPS = vendor %s days %v owed %v surplusDays %v want ALLIED/33/264/-33",
				g.Vendor, deref(g.DaysActive), derefF(g.TotalOwedDollars), deref(g.SurplusDays))
		}
	}
	// LUTTRELL — the hardest real client (added in recheck): SCRAM, 418 days,
	// $6270 owed, $5810 GPS-paid, surplus -$460 -> -31 days (exercises -ceil on a
	// non-integer: 460/15 = 30.67 -> 31), and PTR 14 months = $280, $40 paid.
	{
		c := get("1374859")
		g := compute.ComputeGPS(c, track, nil, "")
		if g.Vendor != "SCRAM" || g.DaysActive == nil || *g.DaysActive != 418 ||
			g.TotalOwedDollars == nil || *g.TotalOwedDollars != 6270 ||
			g.SurplusDollars == nil || *g.SurplusDollars != -460 ||
			g.SurplusDays == nil || *g.SurplusDays != -31 {
			t.Errorf("LUTTRELL GPS = vendor %s days %v owed %v surplus$ %v surplusDays %v want SCRAM/418/6270/-460/-31",
				g.Vendor, deref(g.DaysActive), derefF(g.TotalOwedDollars), derefF(g.SurplusDollars), deref(g.SurplusDays))
		}
		ptr := compute.ComputePTRFees(c, track, "")
		if ptr.TotalOwed != 280 || ptr.TotalPaid != 40 {
			t.Errorf("LUTTRELL PTR owed=%d paid=%v want 280/40", ptr.TotalOwed, ptr.TotalPaid)
		}
	}
}

// TestMultiCaseRetained verifies the PHASE_4 recheck G1 fix: BuildClients keeps
// EVERY blue_book row per IDN instead of collapsing to "last row wins". The
// concrete offline-data impact is multi-case defendants whose cases carry
// DIFFERENT pretrial levels — under the old collapse, only one level survived.
// (The offline copy has no closed rows, so the open/closed roster-membership
// effect can't be exercised here; the open-preferred rep is still asserted
// wherever a status mix exists, which it will on ptr1's live data.)
func TestMultiCaseRetained(t *testing.T) {
	path := filepath.Join("..", "..", "db", "kh222.db")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("offline DB not present (%v) — skipping", err)
	}
	d, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer d.Close()
	clients, err := BuildClients(d, compute.Noon(2026, 5, 30))
	if err != nil {
		t.Fatalf("BuildClients: %v", err)
	}
	isOpen := func(s string) bool { return strings.Contains(strings.ToLower(s), "open") }

	multi, diffLevel, statusMix := 0, 0, 0
	for _, cs := range clients {
		if len(cs) < 2 {
			continue
		}
		multi++
		levels := map[int]bool{}
		hasOpen, hasOther := false, false
		for _, c := range cs {
			lvl, _ := compute.ParseLevel(c.Level)
			levels[lvl] = true
			if isOpen(c.Status) {
				hasOpen = true
			} else {
				hasOther = true
			}
		}
		if len(levels) > 1 {
			diffLevel++ // proves no single-row collapse — both levels retained
		}
		if hasOpen && hasOther {
			statusMix++
			if r := rep(cs); !isOpen(r.Status) {
				t.Errorf("rep status %q not open despite an open case present", r.Status)
			}
		}
	}
	if multi == 0 {
		t.Skip("no multi-case IDN in offline DB")
	}
	if diffLevel == 0 {
		t.Errorf("expected multi-case IDNs with differing levels to be retained, found 0 (collapse may have regressed)")
	}
	t.Logf("multi-case IDNs=%d, differing-level (retained)=%d, open/closed-mix=%d", multi, diffLevel, statusMix)
}

func deref(p *int) int {
	if p == nil {
		return -999
	}
	return *p
}
func derefF(p *float64) float64 {
	if p == nil {
		return -999
	}
	return *p
}
