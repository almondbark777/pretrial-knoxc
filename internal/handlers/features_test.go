package handlers

import (
	"encoding/csv"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pretrial-knoxc/internal/compute"
	"pretrial-knoxc/internal/db"
	"pretrial-knoxc/internal/models"
)

// TestRosterCalendarMonth checks the team-calendar aggregation: correct grid
// shape (Sunday padding + day cells), and per-day counts that sum to the month
// totals across all four categories.
func TestRosterCalendarMonth(t *testing.T) {
	d := testDB(t)
	clients, err := db.BuildClients(d, adminTrack)
	if err != nil {
		t.Fatalf("BuildClients: %v", err)
	}
	rc := rosterCalendarMonth(clients, adminTrack, 2026, time.May)
	if rc.Title != "May 2026" {
		t.Errorf("title = %q, want %q", rc.Title, "May 2026")
	}
	first := compute.Noon(2026, time.May, 1)
	wantLen := int(first.Weekday()) + 31 // leading Sunday padding + 31 days in May
	if len(rc.Days) != wantLen {
		t.Errorf("Days len = %d, want %d", len(rc.Days), wantLen)
	}
	for i := 0; i < int(first.Weekday()); i++ {
		if rc.Days[i].Day != 0 {
			t.Errorf("Days[%d] should be a padding cell (Day 0), got %d", i, rc.Days[i].Day)
		}
	}
	var ci, pm, ms, du int
	for _, x := range rc.Days {
		ci += x.CheckIns
		pm += x.Payments
		ms += x.Missed
		du += x.Due
	}
	if ci != rc.TotCheckIns || pm != rc.TotPayments || ms != rc.TotMissed || du != rc.TotDue {
		t.Errorf("per-day sums (%d/%d/%d/%d) != totals (%d/%d/%d/%d)",
			ci, pm, ms, du, rc.TotCheckIns, rc.TotPayments, rc.TotMissed, rc.TotDue)
	}
	if rc.TotMissed == 0 {
		t.Error("expected some missed windows in the stale offline snapshot")
	}
}

// TestMyDay verifies the per-officer worklist only contains that officer's
// clients, reports a non-zero caseload, and is empty for an unknown officer.
func TestMyDay(t *testing.T) {
	d := testDB(t)
	clients, err := db.BuildClients(d, adminTrack)
	if err != nil {
		t.Fatalf("BuildClients: %v", err)
	}
	var officer string
	for _, cs := range clients {
		if c := openRep(cs); c != nil && strings.TrimSpace(c.Officer) != "" {
			officer = c.Officer
			break
		}
	}
	if officer == "" {
		t.Skip("no supervising officer in offline data")
	}

	md := myDay(clients, adminTrack, officer)
	if md.Caseload == 0 {
		t.Fatalf("expected caseload > 0 for officer %q", officer)
	}
	belongs := func(label string, rows []models.RosterRow) {
		for _, r := range rows {
			c := openRep(clients[r.IDN])
			if c == nil || !strings.EqualFold(strings.TrimSpace(c.Officer), strings.TrimSpace(officer)) {
				t.Errorf("%s row IDN %s is not supervised by %q", label, r.IDN, officer)
			}
		}
	}
	belongs("behind", md.Behind)
	belongs("missed", md.Missed)
	belongs("dueSoon", md.DueSoon)

	empty := myDay(clients, adminTrack, "Nobody McNoone")
	if empty.Caseload != 0 || len(empty.Behind) != 0 || len(empty.Missed) != 0 || len(empty.DueSoon) != 0 {
		t.Errorf("unknown officer should yield an empty MyDay, got caseload=%d", empty.Caseload)
	}
}

// TestExportCSV checks each export streams a valid, attachment-flagged CSV whose
// header width and data-row count match the underlying roster.
func TestExportCSV(t *testing.T) {
	d := testDB(t)
	srv := newServer(d)
	clients, err := db.BuildClients(d, adminTrack)
	if err != nil {
		t.Fatalf("BuildClients: %v", err)
	}
	track := compute.TodayET() // the handlers compute at "today" — match it here
	cases := []struct {
		name string
		h    http.HandlerFunc
		want int
		cols int
	}{
		{"behind", srv.ExportBehind, len(behindRoster(clients, track)), 6},
		{"missed", srv.ExportMissed, len(missedCheckInsRoster(clients, track)), 5},
		{"cases", srv.ExportCases, len(defendantRows(clients, track)), 12},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		tc.h(rec, httptest.NewRequest("GET", "/export/"+tc.name+".csv", nil))
		if rec.Code != 200 {
			t.Errorf("%s: code %d", tc.name, rec.Code)
			continue
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
			t.Errorf("%s: content-type %q", tc.name, ct)
		}
		if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") || !strings.Contains(cd, ".csv") {
			t.Errorf("%s: content-disposition %q", tc.name, cd)
		}
		recs, err := csv.NewReader(rec.Body).ReadAll()
		if err != nil {
			t.Errorf("%s: CSV parse: %v", tc.name, err)
			continue
		}
		if len(recs) == 0 || len(recs[0]) != tc.cols {
			t.Errorf("%s: header width = %d, want %d", tc.name, headerWidth(recs), tc.cols)
		}
		if got := len(recs) - 1; got != tc.want {
			t.Errorf("%s: data rows = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func headerWidth(recs [][]string) int {
	if len(recs) == 0 {
		return 0
	}
	return len(recs[0])
}

// TestListAudit verifies newest-first ordering, idn filtering, and the limit.
func TestListAudit(t *testing.T) {
	d := testDB(t)
	for _, ev := range []db.AuditEvent{
		{User: "u", Action: "a1", Table: "t", RowID: "111"},
		{User: "u", Action: "a2", Table: "t", RowID: "222"},
		{User: "u", Action: "a3", Table: "t", RowID: "111"},
	} {
		if err := db.WriteAudit(d, ev); err != nil {
			t.Fatalf("WriteAudit: %v", err)
		}
	}
	all, err := db.ListAudit(d, "", 200)
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	if len(all) < 3 {
		t.Fatalf("want >= 3 audit rows, got %d", len(all))
	}
	if all[0].Action != "a3" {
		t.Errorf("newest-first: want a3 first, got %q", all[0].Action)
	}
	f, err := db.ListAudit(d, "111", 200)
	if err != nil {
		t.Fatalf("ListAudit(111): %v", err)
	}
	if len(f) < 2 {
		t.Errorf("want >= 2 rows for idn 111, got %d", len(f))
	}
	for _, r := range f {
		if r.RowID != "111" {
			t.Errorf("idn filter leaked row_id %q", r.RowID)
		}
	}
	if lim, _ := db.ListAudit(d, "", 1); len(lim) != 1 {
		t.Errorf("limit 1 -> %d rows", len(lim))
	}
}
