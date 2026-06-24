package handlers

import (
	"strings"
	"testing"
	"time"

	"pretrial-knoxc/internal/compute"
	"pretrial-knoxc/internal/db"
)

// TestDashboardShowsReopenedCases: a case with an OLD referral date that was
// recently reopened appears in the new-referrals feed (labeled "Reopened"); an
// equally-old case that wasn't reopened stays out.
func TestDashboardShowsReopenedCases(t *testing.T) {
	track := time.Date(2026, 6, 24, 0, 0, 0, 0, time.UTC)
	oldRef := time.Date(2025, 1, 10, 9, 0, 0, 0, time.UTC) // far outside the 48h window
	clients := map[string][]*compute.Client{
		"555": {{IDN: "555", Name: "REOPEN, RON", Status: "Open", Level: "2", RefD: oldRef, RefOK: true}},
		"556": {{IDN: "556", Name: "OLD, OLLIE", Status: "Open", Level: "1", RefD: oldRef, RefOK: true}},
	}
	reopened := map[string]time.Time{"555": time.Date(2026, 6, 24, 8, 0, 0, 0, time.UTC)}

	d := consoleDashboard(clients, track, nil, nil, nil, "", reopened)

	var found *ConsoleReferral
	for i := range d.Referrals {
		switch d.Referrals[i].IDN {
		case "555":
			found = &d.Referrals[i]
		case "556":
			t.Errorf("old, un-reopened case 556 leaked into the new-referrals feed")
		}
	}
	if found == nil {
		t.Fatalf("reopened case 555 not in the new-referrals feed")
	}
	if !strings.Contains(found.Context, "Reopened") {
		t.Errorf("reopened context = %q, want it to mention Reopened", found.Context)
	}
}

// TestReopenedSinceTracksOpenOverrides: only case_status overrides set to an
// open value, within the cutoff, count as reopens.
func TestReopenedSinceTracksOpenOverrides(t *testing.T) {
	d := testDB(t)
	if err := db.SetOverride(d, "770001", "case_status", "Open", "tester"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetOverride(d, "770002", "case_status", "Closed", "tester"); err != nil {
		t.Fatal(err)
	}
	m, err := db.ReopenedSince(d, time.Now().AddDate(0, 0, -2))
	if err != nil {
		t.Fatalf("ReopenedSince: %v", err)
	}
	if _, ok := m["770001"]; !ok {
		t.Errorf("reopened (Open) 770001 missing from ReopenedSince")
	}
	if _, ok := m["770002"]; ok {
		t.Errorf("closed 770002 should not count as reopened")
	}
	// A future cutoff excludes everything (the override was written just now).
	if m2, _ := db.ReopenedSince(d, time.Now().AddDate(0, 0, 2)); len(m2) != 0 {
		t.Errorf("future cutoff should exclude all reopens, got %d", len(m2))
	}
}
