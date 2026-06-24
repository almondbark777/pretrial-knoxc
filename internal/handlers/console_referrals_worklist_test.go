package handlers

import (
	"testing"
	"time"

	"pretrial-knoxc/internal/compute"
)

// mkRefClient builds a minimal client for the referrals-worklist tests. When
// hasDT is true the referral date+time is set (both RefDT and RefD); otherwise the
// client is "undated" and must sort to the bottom.
func mkRefClient(idn, name, status string, ref time.Time, hasDT bool) *compute.Client {
	c := &compute.Client{IDN: idn, Name: name, Level: "1", Status: status, Officer: "x"}
	if hasDT {
		c.RefDT, c.RefDTOK = ref, true
		c.RefD, c.RefOK = ref, true
	}
	return c
}

// referralWorklist must list EVERY client (including closed-only and undated) and
// order them most-recently-referred first, with undated referrals last.
func TestReferralWorklistNewestFirst(t *testing.T) {
	apr30am := time.Date(2026, 4, 30, 7, 30, 0, 0, time.UTC)
	apr30pm := time.Date(2026, 4, 30, 16, 0, 0, 0, time.UTC)
	mar1 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	clients := map[string][]*compute.Client{
		"A": {mkRefClient("A", "Alpha", "Open", mar1, true)},
		"B": {mkRefClient("B", "Bravo", "Open", apr30am, true)},
		"C": {mkRefClient("C", "Charlie", "Open", apr30pm, true)},
		"D": {mkRefClient("D", "Delta", "Closed", time.Time{}, false)}, // closed + undated
	}
	rows := referralWorklist(clients)
	if len(rows) != 4 {
		t.Fatalf("rows = %d, want 4 (all clients incl. closed-only/undated)", len(rows))
	}
	got := []string{rows[0].IDN, rows[1].IDN, rows[2].IDN, rows[3].IDN}
	want := []string{"C", "B", "A", "D"} // Apr30 16:00 > Apr30 07:30 > Mar1 > undated
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
	if rows[3].Referred != "—" || rows[3].ReferredSort != "" {
		t.Fatalf("undated row: display %q sort %q, want %q and empty", rows[3].Referred, rows[3].ReferredSort, "—")
	}
}

// referralExportRows must emit one row per client, 20 columns aligned with the
// ExportReferrals header, in the same newest-referral-first order.
func TestReferralExportRowsAlignedAndSorted(t *testing.T) {
	clients := map[string][]*compute.Client{
		"B": {mkRefClient("B", "Bravo", "Open", time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC), true)},
		"A": {mkRefClient("A", "Alpha", "Open", time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), true)},
	}
	rows := referralExportRows(clients)
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if len(rows[0]) != 20 {
		t.Fatalf("columns = %d, want 20 (must match the ExportReferrals header)", len(rows[0]))
	}
	if rows[0][0] != "Bravo" || rows[1][0] != "Alpha" {
		t.Fatalf("export order = %q then %q, want Bravo then Alpha (newest first)", rows[0][0], rows[1][0])
	}
	if rows[0][1] != "B" {
		t.Fatalf("IDN column = %q, want B", rows[0][1])
	}
}
