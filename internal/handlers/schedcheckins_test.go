package handlers

import (
	"testing"

	"pretrial-knoxc/internal/compute"
	"pretrial-knoxc/internal/models"
)

// A booking that falls due today must appear on the dashboard's Today's
// Schedule, attributed to the client's supervising officer (Mine) so it
// survives the "My caseload" filter; other days' bookings stay off.
func TestConsoleDashboardScheduledCheckIn(t *testing.T) {
	track := compute.Noon(2026, 6, 10)
	clients := map[string][]*compute.Client{
		"1": {{IDN: "1", Name: "Client One", Status: "Open", Officer: "Alice Smith",
			Level: "2", RefD: compute.Noon(2026, 1, 1), RefOK: true}},
	}
	scheds := []models.ScheduledCheckIn{
		{IDN: "1", For: "2026-06-10", Type: "In-person"}, // today → shows
		{IDN: "1", For: "2026-06-11", Type: "Phone"},     // tomorrow → hidden
	}
	schedItem := func(d ConsoleDashboard) *ConsoleSched {
		for i := range d.Schedule {
			if d.Schedule[i].Chip.Label == "Scheduled" {
				return &d.Schedule[i]
			}
		}
		return nil
	}

	d := consoleDashboard(clients, track, nil, nil, scheds, "Alice Smith")
	it := schedItem(d)
	if it == nil {
		t.Fatal("expected today's booking on the schedule")
	}
	if !it.Mine || it.Title != "Client One" || it.Sub != "Scheduled check-in · In-person" {
		t.Errorf("schedule item wrong: %+v", it)
	}
	var scheduled int
	for _, s := range d.Schedule {
		if s.Chip.Label == "Scheduled" {
			scheduled++
		}
	}
	if scheduled != 1 {
		t.Errorf("scheduled items on dashboard = %d, want 1 (tomorrow's must not show)", scheduled)
	}
	if it2 := schedItem(consoleDashboard(clients, track, nil, nil, scheds, "Bob Jones")); it2 == nil || it2.Mine {
		t.Errorf("booking should not be Mine for a different officer, got %+v", it2)
	}
}

// The record's Scheduled rows derive Done (a real check-in exists on the
// booked day) and Missed (day passed without one) at read time.
func TestConsoleRecordScheduledStates(t *testing.T) {
	track := compute.Noon(2026, 6, 10)
	c := &compute.Client{
		IDN: "1", Name: "Client One", Status: "Open", Level: "2",
		RefD: compute.Noon(2026, 1, 1), RefOK: true,
		CheckIns: []compute.CheckIn{{D: compute.Noon(2026, 6, 1), DOK: true, Type: "In Person"}},
	}
	extras := models.DefendantExtras{ScheduledCheckIns: []models.ScheduledCheckIn{
		{ID: 1, For: "2026-06-01", Type: "In-person"}, // fulfilled that day → Done
		{ID: 2, For: "2026-06-05"},                    // passed, no check-in → Missed
		{ID: 3, For: "2026-06-20"},                    // future → neither
	}}
	rec := consoleRecord(c, []*compute.Client{c}, track,
		compute.CheckInResult{}, compute.PTRResult{}, compute.GPSResult{}, extras)

	if len(rec.Scheduled) != 3 {
		t.Fatalf("Scheduled rows = %d, want 3", len(rec.Scheduled))
	}
	if !rec.Scheduled[0].Done || rec.Scheduled[0].Missed {
		t.Errorf("row 1 (fulfilled): %+v, want Done", rec.Scheduled[0])
	}
	if rec.Scheduled[1].Done || !rec.Scheduled[1].Missed {
		t.Errorf("row 2 (passed): %+v, want Missed", rec.Scheduled[1])
	}
	if rec.Scheduled[2].Done || rec.Scheduled[2].Missed {
		t.Errorf("row 3 (future): %+v, want neither", rec.Scheduled[2])
	}
	if rec.Scheduled[0].Date != "Jun 1, 2026" {
		t.Errorf("date formatting: %q, want \"Jun 1, 2026\"", rec.Scheduled[0].Date)
	}
}

// Every removable app-entered row on the record must carry its DB id, or the
// per-row remove form posts id=0 and silently deletes nothing. Regression for
// the Logged-check-ins panel, which shipped without one.
func TestConsoleRecordRowIDs(t *testing.T) {
	track := compute.Noon(2026, 6, 10)
	c := &compute.Client{IDN: "1", Name: "Client One", Status: "Open", Level: "2",
		RefD: compute.Noon(2026, 1, 1), RefOK: true}
	extras := models.DefendantExtras{
		AddedCheckIns: []models.AddedCheckIn{{ID: 41, Date: "2026-06-01", Type: "Phone"}},
		CourtDates:    []models.CourtDate{{ID: 42, IDN: "1", CourtDate: "2026-06-20", Court: "Division I"}},
		Notes:         []models.Note{{ID: 43, IDN: "1", Body: "note"}},
		Violations: []models.Violation{{ID: 44, IDN: "1", ViolationDate: "2026-06-05",
			Category: "Curfew violation", Severity: "High", Description: "home late"}},
		Reminders: []models.Reminder{{ID: 45, IDN: "1", Body: "court reminder",
			DueDate: "2026-06-19", CreatedAt: "2026-06-09"}},
		Letters: []models.LetterLogEntry{{ID: 46, IDN: "1", Case: "@9",
			Detail: "behind $40.00 · open", GeneratedBy: "officer.x@knoxsheriff.org",
			CreatedAt: "2026-06-08 09:00:00 EDT"}},
	}
	rec := consoleRecord(c, []*compute.Client{c}, track,
		compute.CheckInResult{}, compute.PTRResult{}, compute.GPSResult{}, extras)

	if len(rec.LoggedCheckIns) != 1 || rec.LoggedCheckIns[0].ID != 41 {
		t.Errorf("LoggedCheckIns ID = %+v, want 41", rec.LoggedCheckIns)
	}
	if len(rec.Court) != 1 || rec.Court[0].ID != 42 {
		t.Errorf("Court ID = %+v, want 42", rec.Court)
	}
	if len(rec.Notes) != 1 || rec.Notes[0].ID != 43 {
		t.Errorf("Notes ID = %+v, want 43", rec.Notes)
	}
	if len(rec.Violations) != 1 || rec.Violations[0].ID != 44 {
		t.Errorf("Violations ID = %+v, want 44", rec.Violations)
	} else if rec.Violations[0].Severity.Tone != "risk" {
		t.Errorf("High severity tone = %q, want risk", rec.Violations[0].Severity.Tone)
	}
	if len(rec.Reminders) != 1 || rec.Reminders[0].ID != 45 {
		t.Errorf("Reminders ID = %+v, want 45", rec.Reminders)
	} else if rec.Reminders[0].Due != "Jun 19, 2026" {
		t.Errorf("Reminder Due = %q, want Jun 19, 2026", rec.Reminders[0].Due)
	}
	// Generated letters surface on the Activity timeline (read-only history).
	letterSeen := false
	for _, a := range rec.Activity {
		if a.Title == "Past-due letter generated (EM fees)" {
			letterSeen = true
			if a.Detail != "behind $40.00 · open · by Officer X" {
				t.Errorf("letter activity detail = %q", a.Detail)
			}
		}
	}
	if !letterSeen {
		t.Error("generated letter missing from the Activity timeline")
	}
}
