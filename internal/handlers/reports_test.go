package handlers

import (
	"testing"

	"pretrial-knoxc/internal/models"
)

// TestGroupBehindByOfficer pins the per-officer split of the Behind report:
// alphabetical officer sections (blank officer last as "Unassigned"), rows
// reshaped without the officer column, and a count + behind-$ subtotal that
// reconciles with the roster's (negative) surplus amounts.
func TestGroupBehindByOfficer(t *testing.T) {
	roster := []models.RosterRow{
		{Name: "ADAMS, AMY", IDN: "1", Officer: "Zoe Young", Level: 2, Detail: "behind $100 / 10 days", Amount: -100},
		{Name: "BAKER, BOB", IDN: "2", Officer: "Al Adams", Level: 3, Detail: "behind $50 / 5 days", Amount: -50},
		{Name: "CHASE, CARL", IDN: "3", Officer: "", Level: 1, Detail: "behind $25 / 2 days", Amount: -25},
		{Name: "DOYLE, DAN", IDN: "4", Officer: "Al Adams", Level: 2, Detail: "behind $30.50 / 3 days", Amount: -30.5},
	}
	groups := groupBehindByOfficer(roster)
	if len(groups) != 3 {
		t.Fatalf("want 3 groups, got %d", len(groups))
	}
	// Alphabetical, Unassigned last.
	if groups[0].Label != "Al Adams" || groups[1].Label != "Zoe Young" || groups[2].Label != "Unassigned" {
		t.Fatalf("group order = %q, %q, %q", groups[0].Label, groups[1].Label, groups[2].Label)
	}
	// Al Adams: two clients, $80.50 behind.
	if len(groups[0].Rows) != 2 {
		t.Fatalf("Al Adams rows = %d, want 2", len(groups[0].Rows))
	}
	if got := groups[0].Subtotal; got != "2 clients · $80.50 behind" {
		t.Errorf("Al Adams subtotal = %q", got)
	}
	if got := groups[1].Subtotal; got != "1 client · $100.00 behind" {
		t.Errorf("Zoe Young subtotal = %q", got)
	}
	if got := groups[2].Subtotal; got != "1 client · $25.00 behind" {
		t.Errorf("Unassigned subtotal = %q", got)
	}
	// Officer column is dropped from the per-group rows: Name, IDN, Level, Detail.
	row := groups[0].Rows[0]
	if len(row) != 4 || row[0] != "BAKER, BOB" || row[1] != "2" || row[2] != "L3" || row[3] != "behind $50 / 5 days" {
		t.Errorf("group row shape wrong: %v", row)
	}
	// Total rows across groups == roster size.
	n := 0
	for _, g := range groups {
		n += len(g.Rows)
	}
	if n != len(roster) {
		t.Errorf("rows across groups = %d, want %d", n, len(roster))
	}
	// Empty roster → no groups.
	if g := groupBehindByOfficer(nil); len(g) != 0 {
		t.Errorf("empty roster should yield no groups, got %d", len(g))
	}
}
