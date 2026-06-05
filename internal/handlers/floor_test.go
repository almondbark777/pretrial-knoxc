package handlers

import (
	"testing"
	"time"

	"pretrial-knoxc/internal/compute"
)

// TestReportedMissedFloor verifies the reporting-layer filter: missed windows
// before CheckInDataFloor are dropped, and a client with no check-in records is
// never flagged (even when the raw compute produced missed windows).
func TestReportedMissedFloor(t *testing.T) {
	floor := compute.CheckInDataFloor()
	preFloor := compute.Window{Deadline: floor.AddDate(0, 0, -1), Missed: true}
	postFloor := compute.Window{Deadline: floor.AddDate(0, 0, 1), Missed: true}
	ci := compute.CheckInResult{Missed: []compute.Window{preFloor, postFloor}}

	withRecord := &compute.Client{
		CheckIns: []compute.CheckIn{{D: compute.Noon(2026, time.April, 1), DOK: true, Type: "In Person"}},
	}
	got := reportedMissed(withRecord, ci)
	if len(got) != 1 {
		t.Fatalf("floor filter: want 1 post-floor window, got %d", len(got))
	}
	if got[0].Deadline.Before(floor) {
		t.Errorf("floor filter kept a pre-floor window: %v", got[0].Deadline)
	}

	noRecord := &compute.Client{} // zero check-in records
	if got := reportedMissed(noRecord, ci); got != nil {
		t.Errorf("zero-record client should report no missed windows, got %d", len(got))
	}
}

// TestMissedRosterExcludesZeroRecordClients verifies the monthly missed-check-in
// roster skips defendants with no digital check-in history (the flood guard),
// while still flagging a client who HAS records but none in the current month.
func TestMissedRosterExcludesZeroRecordClients(t *testing.T) {
	track := compute.Noon(2026, time.June, 15)
	ref := compute.Noon(2026, time.January, 1) // well past the 3-day grace
	mk := func(idn string, cis []compute.CheckIn) *compute.Client {
		return &compute.Client{
			IDN: idn, Name: "Client " + idn, Status: "Open", Level: "2",
			RefD: ref, RefOK: true, CheckIns: cis,
		}
	}
	zeroRecord := mk("Z1", nil)
	staleRecord := mk("S1", []compute.CheckIn{
		{D: compute.Noon(2026, time.May, 10), DOK: true, Type: "In Person"}, // none in June
	})
	clients := map[string][]*compute.Client{"Z1": {zeroRecord}, "S1": {staleRecord}}

	rows := missedCheckInsRoster(clients, track)
	if len(rows) != 1 {
		t.Fatalf("want exactly 1 flagged client (S1), got %d: %+v", len(rows), rows)
	}
	if rows[0].IDN != "S1" {
		t.Errorf("want S1 flagged (has records, missed June), got %s", rows[0].IDN)
	}
}
