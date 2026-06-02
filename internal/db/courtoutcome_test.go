package db

import "testing"

// TestCourtOutcomeFlow: add a court date, log an outcome + next date, and read
// it back. Also exercises the additive column migration (EnsureSchema adds
// outcome/next_date to a court_dates table that may predate them).
func TestCourtOutcomeFlow(t *testing.T) {
	d := openEnsured(t)
	idn := "555000222"
	if err := AddCourtDate(d, idn, "2026-06-09", "Courtroom 3B", "Preliminary hearing", "ofc@x"); err != nil {
		t.Fatalf("AddCourtDate: %v", err)
	}
	rows, err := ListCourtDates(d, idn)
	if err != nil || len(rows) != 1 {
		t.Fatalf("ListCourtDates: %v rows=%d", err, len(rows))
	}
	if rows[0].Outcome != "" {
		t.Errorf("new court date should have no outcome, got %q", rows[0].Outcome)
	}
	id := rows[0].ID
	if err := SetCourtOutcome(d, id, "Appeared — plea entered", "2026-07-15", "ofc@x"); err != nil {
		t.Fatalf("SetCourtOutcome: %v", err)
	}
	rows, _ = ListCourtDates(d, idn)
	if rows[0].Outcome != "Appeared — plea entered" {
		t.Errorf("outcome = %q, want logged value", rows[0].Outcome)
	}
	if rows[0].NextDate != "2026-07-15" {
		t.Errorf("next date = %q, want 2026-07-15", rows[0].NextDate)
	}
	// empty outcome is rejected
	if err := SetCourtOutcome(d, id, "  ", "", "ofc@x"); err == nil {
		t.Errorf("empty outcome should be rejected")
	}
}
