package db

import (
	"path/filepath"
	"testing"
)

// TestLetterLogLifecycle pins the log round trip: a batch write lands one row
// per memo plus ONE audit row for the event; LastLetters returns the newest
// stamp per client; a missing table reads as empty (pre-migration tolerance).
func TestLetterLogLifecycle(t *testing.T) {
	// Pre-migration DB (bare file, no schema): tolerant empty read.
	bare, err := Open(filepath.Join(t.TempDir(), "letterlog_bare.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if got := LastLetters(bare, "em_fees"); len(got) != 0 {
		t.Errorf("missing table should read empty, got %v", got)
	}
	bare.Close()

	// The real flow runs on the provisioned fixture (DeletePerson purges every
	// extension table, including migration-001 ones EnsureSchema doesn't create).
	d := openEnsured(t)

	refs := []LetterRef{
		{IDN: "111", Case: "@1", Detail: "behind $40.00 · open"},
		{IDN: "222", Case: "@2", Detail: "behind $75.00 · closed"},
	}
	if err := LogLetters(d, "officer@knoxsheriff.org", "em_fees", refs); err != nil {
		t.Fatalf("LogLetters: %v", err)
	}

	last := LastLetters(d, "em_fees")
	if len(last) != 2 {
		t.Fatalf("LastLetters = %d clients, want 2", len(last))
	}
	if last["111"].By != "officer@knoxsheriff.org" || last["111"].At == "" {
		t.Errorf("stamp for 111 = %+v, want officer + timestamp", last["111"])
	}

	// One audit row per generation EVENT, not per memo.
	var audits int
	if err := d.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action='letters_generated'`).Scan(&audits); err != nil {
		t.Fatalf("audit count: %v", err)
	}
	if audits != 1 {
		t.Errorf("audit rows = %d, want 1 for one batch", audits)
	}

	// A later letter for 111 wins as the newest stamp.
	if err := LogLetters(d, "second@knoxsheriff.org", "em_fees", []LetterRef{{IDN: "111", Case: "@1"}}); err != nil {
		t.Fatalf("second LogLetters: %v", err)
	}
	if got := LastLetters(d, "em_fees")["111"].By; got != "second@knoxsheriff.org" {
		t.Errorf("newest stamp by = %q, want the second generator", got)
	}

	// Other letter types don't bleed in.
	if got := LastLetters(d, "court_reminder"); len(got) != 0 {
		t.Errorf("letter types must not bleed: %v", got)
	}

	// Whole-person delete purges the history like every extension table.
	if err := DeletePerson(d, "111", "sup@knoxsheriff.org", "test", false); err != nil {
		t.Fatalf("DeletePerson: %v", err)
	}
	if _, ok := LastLetters(d, "em_fees")["111"]; ok {
		t.Error("letter_log rows should purge on whole-person delete")
	}
	if _, ok := LastLetters(d, "em_fees")["222"]; !ok {
		t.Error("other clients' letter history must survive")
	}
}
