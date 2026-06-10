package db

import "testing"

// TestScheduledCheckInLifecycle covers book → list (chronological) → LoadExtras
// carry → cancel → audit trail → purge on whole-person delete.
func TestScheduledCheckInLifecycle(t *testing.T) {
	d := openEnsured(t)
	const idn = "999000666"
	const by = "officer@knoxsheriff.org"

	if err := AddScheduledCheckIn(d, "", "2026-07-01", "", "", by); err != errEmptyField {
		t.Fatalf("missing idn: err = %v, want errEmptyField", err)
	}
	if err := AddScheduledCheckIn(d, idn, "", "", "", by); err != errEmptyField {
		t.Fatalf("missing date: err = %v, want errEmptyField", err)
	}

	if err := AddDefendant(d, NewDefendant{IDN: idn, Name: "ZZSCHED, SAM", Level: "2", Status: "Open"}, by); err != nil {
		t.Fatalf("AddDefendant: %v", err)
	}
	// Booked out of order — list must come back chronological.
	if err := AddScheduledCheckIn(d, idn, "2026-07-15", "Phone", "", by); err != nil {
		t.Fatalf("AddScheduledCheckIn #1: %v", err)
	}
	if err := AddScheduledCheckIn(d, idn, "2026-07-01", "In-person", "", by); err != nil {
		t.Fatalf("AddScheduledCheckIn #2: %v", err)
	}

	scheds, err := ListScheduledCheckIns(d, idn)
	if err != nil {
		t.Fatalf("ListScheduledCheckIns: %v", err)
	}
	if len(scheds) != 2 || scheds[0].For != "2026-07-01" || scheds[1].For != "2026-07-15" {
		t.Fatalf("scheds = %+v, want 2 rows chronological", scheds)
	}
	if scheds[0].Type != "In-person" || scheds[0].CreatedBy != by {
		t.Errorf("first sched fields wrong: %+v", scheds[0])
	}

	all, err := ListAllScheduledCheckIns(d)
	if err != nil {
		t.Fatalf("ListAllScheduledCheckIns: %v", err)
	}
	var mine int
	for _, s := range all {
		if s.IDN == idn {
			mine++
		}
	}
	if mine != 2 {
		t.Errorf("global list carries %d of the client's bookings, want 2", mine)
	}

	extras, err := LoadExtras(d, idn)
	if err != nil {
		t.Fatalf("LoadExtras: %v", err)
	}
	if len(extras.ScheduledCheckIns) != 2 {
		t.Errorf("LoadExtras.ScheduledCheckIns = %d rows, want 2", len(extras.ScheduledCheckIns))
	}

	if err := DeleteScheduledCheckIn(d, scheds[0].ID, by); err != nil {
		t.Fatalf("DeleteScheduledCheckIn: %v", err)
	}
	if left, _ := ListScheduledCheckIns(d, idn); len(left) != 1 || left[0].For != "2026-07-15" {
		t.Errorf("after cancel: %+v, want just 2026-07-15", left)
	}

	audit, _ := ListAudit(d, "", 100)
	var adds, dels int
	for _, a := range audit {
		if a.Table != "scheduled_check_ins" {
			continue
		}
		switch a.Action {
		case "sched_add":
			adds++
		case "sched_delete":
			dels++
		}
	}
	if adds != 2 || dels != 1 {
		t.Errorf("audit: sched_add=%d sched_delete=%d, want 2/1", adds, dels)
	}

	// Whole-person delete purges the remaining booking.
	if err := DeletePerson(d, idn, by, "sched purge test", false); err != nil {
		t.Fatalf("DeletePerson: %v", err)
	}
	if left, _ := ListScheduledCheckIns(d, idn); len(left) != 0 {
		t.Errorf("bookings survived whole-person delete: %+v", left)
	}
}
