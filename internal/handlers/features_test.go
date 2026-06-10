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

	// Week-row + weekday-column totals (STATUS nice-to-have) must reconcile
	// with the grand totals, and every week row must hold exactly 7 cells.
	if len(rc.ColTotals) != 7 {
		t.Fatalf("ColTotals len = %d, want 7", len(rc.ColTotals))
	}
	if want := (wantLen + 6) / 7; len(rc.Weeks) != want {
		t.Errorf("Weeks len = %d, want %d", len(rc.Weeks), want)
	}
	var wk, col models.RosterTotals
	for i, w := range rc.Weeks {
		if len(w.Days) != 7 {
			t.Fatalf("Weeks[%d] has %d cells, want 7", i, len(w.Days))
		}
		wk.CheckIns += w.Tot.CheckIns
		wk.Payments += w.Tot.Payments
		wk.Missed += w.Tot.Missed
		wk.Due += w.Tot.Due
	}
	for _, c := range rc.ColTotals {
		col.CheckIns += c.CheckIns
		col.Payments += c.Payments
		col.Missed += c.Missed
		col.Due += c.Due
	}
	want := models.RosterTotals{CheckIns: rc.TotCheckIns, Payments: rc.TotPayments,
		Missed: rc.TotMissed, Due: rc.TotDue}
	if wk != want {
		t.Errorf("sum of week totals %+v != month totals %+v", wk, want)
	}
	if col != want {
		t.Errorf("sum of column totals %+v != month totals %+v", col, want)
	}
	if rc.Month != want {
		t.Errorf("rc.Month %+v != month totals %+v", rc.Month, want)
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

// TestExportHonorsAsOf pins that the roster CSV exports compute against the
// console's as-of date (ptc_asof cookie), not today — so a file downloaded from a
// "historical view" matches the on-screen roster and is stamped with that date.
func TestExportHonorsAsOf(t *testing.T) {
	d := testDB(t)
	srv := newServer(d)
	clients, err := db.BuildClients(d, adminTrack)
	if err != nil {
		t.Fatalf("BuildClients: %v", err)
	}
	const asofStr = "2026-01-15"
	asof, ok := compute.ParseDay(asofStr)
	if !ok {
		t.Fatalf("ParseDay(%q) failed", asofStr)
	}
	want := len(behindRoster(clients, asof))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/export/behind.csv", nil)
	req.AddCookie(&http.Cookie{Name: "ptc_asof", Value: asofStr})
	srv.ExportBehind(rec, req)

	// Filename carries the as-of date — only possible if the handler used the cookie.
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, asofStr) {
		t.Errorf("filename should carry as-of date %s, got %q", asofStr, cd)
	}
	recs, err := csv.NewReader(rec.Body).ReadAll()
	if err != nil {
		t.Fatalf("CSV parse: %v", err)
	}
	if got := len(recs) - 1; got != want {
		t.Errorf("rows = %d, want %d (behind roster as of %s)", got, want, asofStr)
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
