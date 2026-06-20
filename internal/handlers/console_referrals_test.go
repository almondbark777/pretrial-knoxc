package handlers

import "testing"

// referralView must emit one label per column, link by IDN, humanize officer
// emails, normalize dates, and blank out empty cells.
func TestReferralView(t *testing.T) {
	entries := []map[string]string{{
		"idn":                 "1234567",
		"defendant":           "DOE, JANE",
		"supervising_officer": "jane.doe@knoxsheriff.org",
		"referral_date":       "2026-06-01",
		"charge_type":         "", // blank → ""
	}}
	labels, rows := referralView(entries)

	if len(labels) != len(referralColumns) {
		t.Fatalf("labels = %d, want %d", len(labels), len(referralColumns))
	}
	if labels[0] != "Defendant" || labels[1] != "IDN" {
		t.Fatalf("unexpected leading labels: %q, %q", labels[0], labels[1])
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	r := rows[0]
	if r.IDN != "1234567" {
		t.Fatalf("row IDN = %q", r.IDN)
	}
	if r.Cells[0] != "DOE, JANE" { // index 0 = Defendant
		t.Fatalf("name cell = %q", r.Cells[0])
	}
	if r.Cells[5] != "Jane Doe" { // index 5 = Supervising Officer, humanized
		t.Fatalf("officer cell = %q, want Jane Doe", r.Cells[5])
	}
	if r.Cells[6] != "Jun 1, 2026" { // index 6 = Referral Date, normalized
		t.Fatalf("referral date cell = %q, want Jun 1, 2026", r.Cells[6])
	}
	if r.Cells[7] != "" { // index 7 = Charge Type, blank stays empty
		t.Fatalf("blank charge-type cell = %q, want empty", r.Cells[7])
	}
}
