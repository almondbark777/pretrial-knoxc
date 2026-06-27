package handlers

import (
	"testing"
	"time"

	"pretrial-knoxc/internal/compute"
	"pretrial-knoxc/internal/models"
)

// TestCalendarCourtDates: app-entered court dates surface on both the per-client
// calendar (as "court" events on the right day) and the roster aggregate (as
// per-day counts), including a hearing's logged reschedule (NextDate). Dates
// outside the rendered month are excluded.
func TestCalendarCourtDates(t *testing.T) {
	c := &compute.Client{IDN: "X1", Name: "TESTER, T"}
	track := compute.Noon(2026, time.June, 15)
	cds := []models.CourtDate{
		{IDN: "X1", CourtDate: "6/10/2026", Court: "Sessions"},                       // in-month, scheduled
		{IDN: "X1", CourtDate: "5/1/2026", NextDate: "6/20/2026", Court: "Criminal"}, // scheduled out, reschedule in
		{IDN: "X1", CourtDate: "7/3/2026"},                                           // out-of-month, ignored
	}

	// Per-client: expect court events only on day 10 and day 20.
	_, days := calendarMonth(c, cds, track, 2026, time.June)
	byDay := map[int]int{}
	total := 0
	for _, d := range days {
		for _, ev := range d.Events {
			if ev.Kind == "court" {
				byDay[d.Day]++
				total++
			}
		}
	}
	if total != 2 || byDay[10] != 1 || byDay[20] != 1 {
		t.Fatalf("per-client court events: total=%d byDay=%v, want day10=1 day20=1 total=2", total, byDay)
	}

	// Roster aggregate: same two appearances counted for this one client.
	clients := map[string][]*compute.Client{"X1": {c}}
	rc := rosterCalendarMonth(clients, map[string][]models.CourtDate{"X1": cds}, track, 2026, time.June)
	if rc.TotCourt != 2 {
		t.Fatalf("rc.TotCourt = %d, want 2", rc.TotCourt)
	}
	sum := 0
	for _, day := range rc.Days {
		sum += day.Court
	}
	if sum != rc.TotCourt {
		t.Fatalf("per-day court sum %d != TotCourt %d", sum, rc.TotCourt)
	}
}
