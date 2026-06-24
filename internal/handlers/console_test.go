package handlers

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"pretrial-knoxc/internal/compute"
	"pretrial-knoxc/internal/db"
	"pretrial-knoxc/internal/models"
)

// The console is a presentation layer over the SAME server-side math as the
// tracker / existing dashboard. These tests pin that: the console's headline
// numbers and roster membership must equal what the shared roster functions
// produce — no divergence, no reimplemented rule.

func TestConsoleDashboardParity(t *testing.T) {
	d := testDB(t)
	clients, err := db.BuildClients(d, adminTrack)
	if err != nil {
		t.Fatalf("BuildClients: %v", err)
	}
	stats := computeStats(clients, adminTrack)
	dash := consoleDashboard(clients, adminTrack, nil, nil, nil, "")

	if dash.KPIs.ActiveClients != stats.Open {
		t.Errorf("ActiveClients = %d, want Open %d", dash.KPIs.ActiveClients, stats.Open)
	}
	if dash.KPIs.OverdueCheckIns != stats.MissedMonth {
		t.Errorf("OverdueCheckIns = %d, want MissedMonth %d", dash.KPIs.OverdueCheckIns, stats.MissedMonth)
	}
	if dash.KPIs.OpenViolations != 0 {
		t.Errorf("OpenViolations = %d, want 0 (no violations passed)", dash.KPIs.OpenViolations)
	}
	// The new-referrals feed is capped at 40, ReferralTotal carries the true
	// pre-cap count, and rows are sorted newest-first.
	if len(dash.Referrals) > 40 {
		t.Errorf("referrals not capped: %d", len(dash.Referrals))
	}
	if dash.ReferralTotal < len(dash.Referrals) {
		t.Errorf("ReferralTotal %d < shown %d", dash.ReferralTotal, len(dash.Referrals))
	}
	for i := 1; i < len(dash.Referrals); i++ {
		if dash.Referrals[i-1].ref.Before(dash.Referrals[i].ref) {
			t.Errorf("referrals not sorted newest-first at %d", i)
			break
		}
	}
}

// A court appearance on the dashboard's Today's Schedule must be attributed to the
// client's supervising officer, so it survives the "My caseload" filter (which
// hides rows with Mine=false). Regression for the hardcoded Mine:false bug.
func TestConsoleDashboardCourtMine(t *testing.T) {
	track := compute.Noon(2026, 6, 1)
	clients := map[string][]*compute.Client{
		"1": {{IDN: "1", Name: "Client One", Status: "Open", Officer: "Alice Smith",
			Level: "2", RefD: compute.Noon(2026, 1, 1), RefOK: true}},
	}
	courts := []models.CourtDate{{IDN: "1", CourtDate: "2026-06-01", Court: "Room 1"}}

	courtItem := func(d ConsoleDashboard) *ConsoleSched {
		for i := range d.Schedule {
			if d.Schedule[i].Time == "Court" {
				return &d.Schedule[i]
			}
		}
		return nil
	}

	// Signed in as the supervising officer → the court item is "mine".
	mineCi := courtItem(consoleDashboard(clients, track, courts, nil, nil, "Alice Smith"))
	if mineCi == nil {
		t.Fatal("expected a court schedule item")
	}
	if !mineCi.Mine {
		t.Error("court item should be Mine for the supervising officer")
	}
	// Signed in as a different officer → not mine.
	otherCi := courtItem(consoleDashboard(clients, track, courts, nil, nil, "Bob Jones"))
	if otherCi == nil {
		t.Fatal("expected a court schedule item")
	}
	if otherCi.Mine {
		t.Error("court item should not be Mine for a different officer")
	}
}

// A new-referral feed row must be attributed to the client's supervising officer
// so it survives the "My caseload" filter (which hides rows with Mine=false).
func TestConsoleDashboardReferralMine(t *testing.T) {
	track := compute.Noon(2026, 6, 1)
	clients := map[string][]*compute.Client{
		"1": {{IDN: "1", Name: "Client One", Status: "Open", Officer: "Alice Smith",
			Level: "2", RefD: track, RefOK: true}}, // referred today → in the 24h window
	}
	refRow := func(d ConsoleDashboard) *ConsoleReferral {
		for i := range d.Referrals {
			if d.Referrals[i].IDN == "1" {
				return &d.Referrals[i]
			}
		}
		return nil
	}
	if a := refRow(consoleDashboard(clients, track, nil, nil, nil, "Alice Smith")); a == nil || !a.Mine {
		t.Errorf("referral should be Mine for the supervising officer, got %+v", a)
	}
	if a := refRow(consoleDashboard(clients, track, nil, nil, nil, "Bob Jones")); a == nil || a.Mine {
		t.Errorf("referral should not be Mine for a different officer, got %+v", a)
	}
	// A referral older than 24 hours (in day terms) must NOT appear in the feed.
	stale := map[string][]*compute.Client{
		"2": {{IDN: "2", Name: "Old Referral", Status: "Open", Officer: "Alice Smith",
			Level: "2", RefD: compute.Noon(2026, 5, 1), RefOK: true}},
	}
	if d := consoleDashboard(stale, track, nil, nil, nil, ""); len(d.Referrals) != 0 {
		t.Errorf("stale referral leaked into the 24h feed: %d rows", len(d.Referrals))
	}
}

// The compliance page's Violations roster resolves each violation to its client,
// composes a detail string, sorts newest-first (undated last), and falls back to
// "IDN x" for an unknown client. This is what makes the dashboard's
// "Open Violations" KPI deep-link land on actual rows instead of an empty page.
func TestViolationRoster(t *testing.T) {
	clients := map[string][]*compute.Client{
		"1": {{IDN: "1", Name: "Bravo, Bob", Status: "Open", Officer: "Alice Smith", Level: "2"}},
		"2": {{IDN: "2", Name: "Alpha, Ann", Status: "Open", Officer: "Carol Jones", Level: "1"}},
	}
	viols := []models.Violation{
		{IDN: "1", Category: "Curfew", Description: "home late", ViolationDate: "2026-06-01"},
		{IDN: "2", Category: "", Description: "", ViolationDate: "2026-06-05"},
		{IDN: "9", Category: "Travel", Description: "left county", ViolationDate: ""}, // unknown client, undated
	}
	rows := violationRoster(clients, viols)
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	// newest dated first (Jun 5 → Jun 1), undated last.
	if rows[0].IDN != "2" || rows[1].IDN != "1" || rows[2].IDN != "9" {
		t.Errorf("sort order = %s,%s,%s; want 2,1,9", rows[0].IDN, rows[1].IDN, rows[2].IDN)
	}
	// name + officer + level resolved from the clients map.
	if rows[1].Name != "Bravo, Bob" || rows[1].Officer != "Alice Smith" || rows[1].Level != 2 {
		t.Errorf("row resolve = %+v, want Bravo,Bob / Alice Smith / L2", rows[1])
	}
	// unknown client falls back to "IDN 9".
	if rows[2].Name != "IDN 9" {
		t.Errorf("unknown client name = %q, want %q", rows[2].Name, "IDN 9")
	}
	// detail composes category + description; empty → placeholder.
	if rows[1].Detail != "Curfew — home late" {
		t.Errorf("detail = %q, want %q", rows[1].Detail, "Curfew — home late")
	}
	if rows[0].Detail != "Violation recorded" {
		t.Errorf("empty detail = %q, want placeholder", rows[0].Detail)
	}
	// dated rows are display-formatted.
	if rows[1].Date != "Jun 1, 2026" {
		t.Errorf("date = %q, want %q", rows[1].Date, "Jun 1, 2026")
	}
}

func TestConsoleClientRowsParity(t *testing.T) {
	d := testDB(t)
	clients, err := db.BuildClients(d, adminTrack)
	if err != nil {
		t.Fatalf("BuildClients: %v", err)
	}
	rows, _, _ := consoleClientRows(clients, adminTrack, nil)
	if len(rows) != len(defendantRows(clients, adminTrack)) {
		t.Errorf("row count %d != defendantRows %d", len(rows), len(defendantRows(clients, adminTrack)))
	}
	// "Behind on GPS" compliance chip must agree with the behind roster.
	behind := map[string]bool{}
	for _, r := range behindRoster(clients, adminTrack) {
		behind[r.IDN] = true
	}
	for _, row := range rows {
		isBehindChip := row.Compliance.Label == "Behind on GPS"
		if isBehindChip != behind[row.IDN] {
			t.Errorf("IDN %s: behind-chip=%v but roster=%v", row.IDN, isBehindChip, behind[row.IDN])
		}
		if isBehindChip && row.Compliance.Tone != "risk" {
			t.Errorf("IDN %s behind chip tone = %q, want risk", row.IDN, row.Compliance.Tone)
		}
		if row.Initials == "" {
			t.Errorf("IDN %s has empty initials", row.IDN)
		}
	}
}

// The roster's Next Court / Next Check-in columns must carry ISO sort keys so the
// table sorts chronologically, not alphabetically by month name. Missing dates
// fall back to the far-future sentinel so they sort last.
func TestConsoleClientRowsDateSortKeys(t *testing.T) {
	d := testDB(t)
	clients, err := db.BuildClients(d, adminTrack)
	if err != nil {
		t.Fatalf("BuildClients: %v", err)
	}
	rows, _, _ := consoleClientRows(clients, adminTrack, nil) // nil court map → every Next Court blank
	iso := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	for _, r := range rows {
		if !iso.MatchString(r.NextCheckInSort) {
			t.Errorf("IDN %s NextCheckInSort=%q not ISO-sortable", r.IDN, r.NextCheckInSort)
		}
		if (r.NextCheckIn == "—") != (r.NextCheckInSort == blankDateSort) {
			t.Errorf("IDN %s check-in display/sort disagree: %q / %q", r.IDN, r.NextCheckIn, r.NextCheckInSort)
		}
		// Referred carries an ISO key, or "" when there is no referral date (so the
		// roster's default newest-first sort drops it to the bottom).
		if r.Referred == "—" {
			if r.ReferredSort != "" {
				t.Errorf("IDN %s: no referral but ReferredSort=%q", r.IDN, r.ReferredSort)
			}
		} else if !iso.MatchString(r.ReferredSort) {
			t.Errorf("IDN %s ReferredSort=%q not ISO-sortable", r.IDN, r.ReferredSort)
		}
		// No court map was supplied, so every Next Court is blank → sentinel.
		if r.NextCourt != "—" || r.NextCourtSort != blankDateSort {
			t.Errorf("IDN %s expected blank court cell, got %q / %q", r.IDN, r.NextCourt, r.NextCourtSort)
		}
	}
	// And a populated court map must yield a chronological ISO key, not "Jan 2".
	cc := courtCell{Display: "Jan 2", Sort: "2026-01-02"}
	court := map[string]courtCell{}
	for idn := range clients {
		court[idn] = cc
		break
	}
	if len(court) == 1 {
		got, _, _ := consoleClientRows(clients, adminTrack, court)
		var found bool
		for _, r := range got {
			if r.NextCourt == "Jan 2" {
				found = true
				if r.NextCourtSort != "2026-01-02" {
					t.Errorf("populated court cell sort = %q, want ISO 2026-01-02", r.NextCourtSort)
				}
			}
		}
		if !found {
			t.Error("expected one row to pick up the seeded court date")
		}
	}
}

func TestConsoleRecordParity(t *testing.T) {
	d := testDB(t)
	clients, err := db.BuildClients(d, adminTrack)
	if err != nil {
		t.Fatalf("BuildClients: %v", err)
	}
	behind := behindRoster(clients, adminTrack)
	if len(behind) == 0 {
		t.Skip("no behind-on-GPS clients in fixture at adminTrack")
	}
	idn := behind[0].IDN
	cases := clients[idn]
	c := openRep(cases)
	ci := compute.ComputeCheckIns(*c, adminTrack)
	ptr := compute.ComputePTRFees(*c, adminTrack, "")
	gps := compute.ComputeGPS(*c, adminTrack, nil, "")
	rec := consoleRecord(c, cases, adminTrack, ci, ptr, gps, models.DefendantExtras{}, db.Ledger{})

	// The record must carry the exact computed numbers (single source of truth).
	if rec.GPS.SurplusDollars == nil || *rec.GPS.SurplusDollars >= 0 {
		t.Errorf("behind client surplus should be negative, got %v", rec.GPS.SurplusDollars)
	}
	if rec.PTR.Balance != ptr.Balance {
		t.Errorf("record PTR balance %v != computed %v", rec.PTR.Balance, ptr.Balance)
	}
	// A GPS condition must be present and flagged behind (risk) for this client.
	var gpsCond *ConsoleCondition
	for i := range rec.Conditions {
		if rec.Conditions[i].Name == "GPS electronic monitoring" {
			gpsCond = &rec.Conditions[i]
		}
	}
	if gpsCond == nil {
		t.Fatalf("expected a GPS condition for a GPS-active client")
	}
	if !rec_isWaived(c) && gpsCond.Chip.Tone != "risk" {
		t.Errorf("behind GPS condition tone = %q, want risk", gpsCond.Chip.Tone)
	}
	if len(rec.Summary) == 0 {
		t.Errorf("record summary is empty")
	}
}

// The roster ships to the browser as a compact JSON array (client-side windowing);
// this pins the short-key contract the template's JS depends on, and that
// json.Marshal escapes < so the blob is safe to embed inside a <script> tag.
func TestRosterRowsJSON(t *testing.T) {
	rows := []ConsoleClientRow{
		{IDN: "1", Name: "ABBOTT <b>", Initials: "AB", CaseNo: "@1", Level: 3,
			StatusChip: Chip{Label: "Active"}, NextCourt: "Jan 2", NextCourtSort: "2026-01-02",
			NextCheckIn: "Jun 5", NextCheckInSort: "2026-06-05", CheckInOverdue: true,
			Referred: "Jan 1, 2026", ReferredSort: "2026-01-01",
			Compliance: Chip{Label: "Behind on GPS"}, GpsActive: true, Officer: "Alice Smith",
			Search: "abbott 1 @1 alice smith"},
	}
	blob := string(rosterRowsJSON(rows))

	// Safe to embed in <script>: Go escapes < to <, so no literal '<' survives.
	if strings.Contains(blob, "<") {
		t.Errorf("blob has an unescaped '<' (unsafe in <script>): %s", blob)
	}

	var got []map[string]any
	if err := json.Unmarshal([]byte(blob), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, blob)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	r := got[0]
	for k, want := range map[string]string{
		"i": "1", "n": "ABBOTT <b>", "a": "AB", "c": "@1", "st": "Active",
		"nc": "Jan 2", "ncs": "2026-01-02", "ci": "Jun 5", "cis": "2026-06-05",
		"rd": "Jan 1, 2026", "rds": "2026-01-01",
		"cm": "Behind on GPS", "o": "Alice Smith", "s": "abbott 1 @1 alice smith",
	} {
		if r[k] != want {
			t.Errorf("key %q = %v, want %q", k, r[k], want)
		}
	}
	if r["l"].(float64) != 3 {
		t.Errorf("l = %v, want 3", r["l"])
	}
	if r["ov"] != true || r["g"] != true {
		t.Errorf("ov/g = %v/%v, want true/true", r["ov"], r["g"])
	}
}

// The referral feed shows at most the 40 most recent rows, but ReferralTotal must
// carry the real pre-cap count so the dashboard can say "40 of N" honestly.
func TestConsoleDashboardReferralCap(t *testing.T) {
	track := compute.Noon(2026, 6, 10)
	clients := map[string][]*compute.Client{}
	for i := 0; i < 55; i++ {
		idn := strconv.Itoa(i)
		clients[idn] = []*compute.Client{{IDN: idn, Name: "Client " + idn, Status: "Open",
			Level: "1", RefD: track, RefOK: true}} // all referred today
	}
	d := consoleDashboard(clients, track, nil, nil, nil, "")
	if len(d.Referrals) != 40 {
		t.Errorf("referrals shown = %d, want capped at 40", len(d.Referrals))
	}
	if d.ReferralTotal != 55 {
		t.Errorf("ReferralTotal = %d, want 55 (the pre-cap count)", d.ReferralTotal)
	}
}

// Aggregate violation tallies count only from the go-live epoch; pre-go-live,
// undated, and unparseable rows are dropped. Per-client history is unaffected
// (this filter is only applied to the dashboard aggregate).
func TestViolationsSinceEpoch(t *testing.T) {
	vs := []models.Violation{
		{IDN: "1", ViolationDate: "2026-06-01"}, // on the epoch → kept
		{IDN: "2", ViolationDate: "2026-07-15"}, // after → kept
		{IDN: "3", ViolationDate: "2026-05-31"}, // before go-live → dropped
		{IDN: "4", ViolationDate: ""},           // undated → dropped
		{IDN: "5", ViolationDate: "not-a-date"}, // unparseable → dropped
	}
	got := violationsSinceEpoch(vs)
	if len(got) != 2 {
		t.Fatalf("kept %d, want 2 (%+v)", len(got), got)
	}
	ids := map[string]bool{}
	for _, v := range got {
		ids[v.IDN] = true
	}
	if !ids["1"] || !ids["2"] {
		t.Errorf("expected IDNs 1 and 2 kept, got %+v", got)
	}
}

func TestChipHelpers(t *testing.T) {
	cases := []struct {
		level int
		tone  string
	}{{1, "ok"}, {2, "warn"}, {3, "risk"}, {0, "neutral"}}
	for _, tc := range cases {
		if got := levelChip(tc.level).Tone; got != tc.tone {
			t.Errorf("levelChip(%d).Tone = %q, want %q", tc.level, got, tc.tone)
		}
	}
	if statusChip("Open").Tone != "info" {
		t.Errorf("Open status should be info-toned")
	}
	if statusChip("Closed - dismissed").Label != "Closed" {
		t.Errorf("closed status label = %q", statusChip("Closed - dismissed").Label)
	}
	if c := complianceChip(true, false, 0, true); c.Tone != "risk" || c.Label != "Behind on GPS" {
		t.Errorf("behind compliance chip = %+v", c)
	}
	if c := complianceChip(false, false, 0, true); c.Tone != "ok" || c.Icon != "✓" {
		t.Errorf("compliant chip = %+v", c)
	}
	if c := complianceChip(false, false, 0, false); c.Label != "No referral" {
		t.Errorf("no-referral chip = %+v", c)
	}
}

func TestProfileBackNext(t *testing.T) {
	mk := func(next string) string {
		body := "idn=123&next=" + url.QueryEscape(next)
		r := httptest.NewRequest("POST", "/admin/note/add", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		_, to := profileBack(r)
		return to
	}
	if to := mk("/console/clients/123"); to != "/console/clients/123" {
		t.Errorf("same-origin next not honored: %q", to)
	}
	for _, bad := range []string{"https://evil.com", "//evil.com", ""} {
		if to := mk(bad); to != "/console/clients/123" {
			t.Errorf("next %q should fall back to the console record, got %q", bad, to)
		}
	}
}

// TestSanitizeViewQuery pins that saved views only carry the roster's known
// filter params, re-encoded deterministically — junk and injected params drop.
func TestSanitizeViewQuery(t *testing.T) {
	got := sanitizeViewQuery("comp=behind&evil=<script>&level=3&next=//evil.com&q=smith")
	if got != "comp=behind&level=3&q=smith" {
		t.Errorf("sanitizeViewQuery = %q, want comp/level/q only, sorted", got)
	}
	if got := sanitizeViewQuery(""); got != "" {
		t.Errorf("empty query = %q, want empty", got)
	}
	if got := sanitizeViewQuery("due=today&status=active"); got != "due=today&status=active" {
		t.Errorf("due param dropped: %q (the Due-Today KPI deep-link must be saveable)", got)
	}
	if got := sanitizeViewQuery("q=O%27Brien"); got != "q=O%27Brien" {
		t.Errorf("escaped value mangled: %q", got)
	}
}

// TestPinnedRows pins the dashboard quick-list resolution: pin order kept
// (newest first as PinnedIDNs returns), deleted/unknown IDNs skipped silently.
func TestPinnedRows(t *testing.T) {
	clients := map[string][]*compute.Client{
		"1": {{IDN: "1", Name: "ALPHA TEST", Status: "Open", Level: "3", Officer: "Alice Smith"}},
		"2": {{IDN: "2", Name: "BRAVO TEST", Status: "Open", Level: "2"}},
	}
	rows := pinnedRows(clients, []string{"2", "999", "1"}) // 999 = deleted/unknown
	if len(rows) != 2 {
		t.Fatalf("pinnedRows = %d rows, want 2 (unknown skipped)", len(rows))
	}
	if rows[0].IDN != "2" || rows[1].IDN != "1" {
		t.Errorf("pin order = [%s %s], want [2 1]", rows[0].IDN, rows[1].IDN)
	}
	if rows[1].Detail != "L3 · Alice Smith" {
		t.Errorf("detail = %q, want %q", rows[1].Detail, "L3 · Alice Smith")
	}
	if rows[0].Detail != "L2" {
		t.Errorf("officer-less detail = %q, want %q", rows[0].Detail, "L2")
	}
}

func TestDrugScreenChip(t *testing.T) {
	cases := []struct{ result, tone, label string }{
		{"positive", "risk", "Positive"},
		{"refused", "risk", "Refused"},
		{"diluted", "warn", "Diluted"},
		{"negative", "ok", "Negative"},
		{"pending", "neutral", "Pending"},
		{" Positive ", "risk", "Positive"}, // trimmed + case-insensitive
		{"", "neutral", "—"},
		{"inconclusive", "neutral", "Inconclusive"},
	}
	for _, c := range cases {
		got := drugScreenChip(c.result)
		if got.Tone != c.tone || got.Label != c.label {
			t.Errorf("drugScreenChip(%q) = {%s %s}, want {%s %s}", c.result, got.Tone, got.Label, c.tone, c.label)
		}
	}
}

// TestConsoleRecordDrugScreens pins the record carry-through: screen rows with
// toned result chips, the "Last Drug Screen" summary field (newest, risk-toned
// when positive), and the screens merged into the Activity timeline.
func TestConsoleRecordDrugScreens(t *testing.T) {
	c := &compute.Client{IDN: "1", Name: "Test Client", Status: "Open", Level: "2",
		RefD: compute.Noon(2026, 1, 1), RefOK: true}
	track := compute.Noon(2026, 6, 1)
	ci := compute.ComputeCheckIns(*c, track)
	ptr := compute.ComputePTRFees(*c, track, "")
	gps := compute.ComputeGPS(*c, track, nil, "")
	extras := models.DefendantExtras{DrugScreens: []models.DrugScreen{ // newest first, like ListDrugScreens
		{ID: 2, IDN: "1", ScreenDate: "2026-05-20", TestType: "urine", Result: "positive",
			Substances: "THC", Officer: "tester@knoxsheriff.org"},
		{ID: 1, IDN: "1", ScreenDate: "2026-05-01", TestType: "urine", Result: "negative",
			Officer: "tester@knoxsheriff.org"},
	}}
	rec := consoleRecord(c, []*compute.Client{c}, track, ci, ptr, gps, extras, db.Ledger{})

	if len(rec.DrugScreens) != 2 {
		t.Fatalf("DrugScreens = %d rows, want 2", len(rec.DrugScreens))
	}
	if rec.DrugScreens[0].Result.Tone != "risk" || rec.DrugScreens[0].Result.Label != "Positive" {
		t.Errorf("newest screen chip = %+v, want risk/Positive", rec.DrugScreens[0].Result)
	}
	if rec.DrugScreens[1].Result.Tone != "ok" {
		t.Errorf("older screen chip tone = %q, want ok", rec.DrugScreens[1].Result.Tone)
	}
	if rec.DrugScreens[0].Author != "Tester" {
		t.Errorf("screen author = %q, want display name Tester", rec.DrugScreens[0].Author)
	}

	var last *ConsoleField
	for i := range rec.Summary {
		if rec.Summary[i].K == "Last Drug Screen" {
			last = &rec.Summary[i]
		}
	}
	if last == nil {
		t.Fatal("summary is missing the Last Drug Screen field")
	}
	if !strings.Contains(last.V, "May 20, 2026") || !strings.Contains(last.V, "Positive") || last.Tone != "risk" {
		t.Errorf("Last Drug Screen = %q (tone %q), want newest positive screen with risk tone", last.V, last.Tone)
	}

	found := false
	for _, a := range rec.Activity {
		if strings.HasPrefix(a.Title, "Drug screen") && strings.Contains(a.Detail, "THC") {
			found = true
		}
	}
	if !found {
		t.Error("activity timeline has no drug-screen entry")
	}
}

func TestConsoleRecordActivitySorted(t *testing.T) {
	c := &compute.Client{IDN: "1", Name: "Test Client", Status: "Open", Level: "2",
		RefD: compute.Noon(2026, 1, 1), RefOK: true}
	track := compute.Noon(2026, 6, 1)
	ci := compute.ComputeCheckIns(*c, track)
	ptr := compute.ComputePTRFees(*c, track, "")
	gps := compute.ComputeGPS(*c, track, nil, "")
	extras := models.DefendantExtras{Notes: []models.Note{
		{IDN: "1", Author: "alex.bentley@knoxsheriff.org", Body: "newest note", CreatedAt: "2026-12-31 09:00:00"},
	}}
	rec := consoleRecord(c, []*compute.Client{c}, track, ci, ptr, gps, extras, db.Ledger{})
	if len(rec.Activity) == 0 {
		t.Fatal("activity empty")
	}
	if !strings.HasPrefix(rec.Activity[0].Title, "Note") {
		t.Errorf("newest note should sort to top, got %q", rec.Activity[0].Title)
	}
}

func TestPct(t *testing.T) {
	if got := pct(50, 200); got != "25.0%" {
		t.Errorf("pct(50,200) = %q, want 25.0%%", got)
	}
	if got := pct(1, 0); got != "—" {
		t.Errorf("pct(1,0) = %q, want em-dash", got)
	}
	if got := pct(0, 0); got != "—" {
		t.Errorf("pct(0,0) = %q, want em-dash", got)
	}
}

func TestDistinctOfficers(t *testing.T) {
	clients := map[string][]*compute.Client{
		"1": {{IDN: "1", Officer: "Bravo Officer", Status: "Open"}},
		"2": {{IDN: "2", Officer: "Alpha Officer", Status: "Open"}},
		"3": {{IDN: "3", Officer: "Alpha Officer", Status: "Open"}}, // dup
		"4": {{IDN: "4", Officer: "", Status: "Open"}},              // empty excluded
	}
	got := distinctOfficers(clients)
	want := []string{"Alpha Officer", "Bravo Officer"} // sorted, deduped
	if len(got) != len(want) {
		t.Fatalf("distinctOfficers = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("distinctOfficers[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestInitials(t *testing.T) {
	cases := map[string]string{
		"Alex Bentley":        "AB",
		"ABBOTT, ROBERT LEON": "AL", // first word + last word
		"Cher":                "C",
		"":                    "?",
	}
	for in, want := range cases {
		if got := Initials(in); got != want {
			t.Errorf("Initials(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestConsoleClientRowsStatsDedup asserts that the Stats assembled from the
// behind/missed sets returned by consoleClientRows are identical to those produced
// by the full computeStats pass — verifying the #11 dedup is parity-correct.
func TestConsoleClientRowsStatsDedup(t *testing.T) {
	d := testDB(t)
	clients, err := db.BuildClients(d, adminTrack)
	if err != nil {
		t.Fatalf("BuildClients: %v", err)
	}
	// Build Stats the old way (reference).
	wantStats := computeStats(clients, adminTrack)

	// Build Stats the new way (dedup path used by ConsoleClients).
	_, behind, missed := consoleClientRows(clients, adminTrack, nil)
	st := rosterStateCounts(clients)
	st.BehindGPS = len(behind)
	st.MissedMonth = len(missed)

	if st != wantStats {
		t.Errorf("deduped Stats %+v != computeStats %+v", st, wantStats)
	}
}

// TestAnalyticsDataStatsDedup asserts that analyticsData builds Stats from a
// single behind/missed pass, matching computeStats (#14 dedup parity check).
func TestAnalyticsDataStatsDedup(t *testing.T) {
	d := testDB(t)
	clients, err := db.BuildClients(d, adminTrack)
	if err != nil {
		t.Fatalf("BuildClients: %v", err)
	}
	wantStats := computeStats(clients, adminTrack)
	a := analyticsData(clients, adminTrack)
	if a.Stats != wantStats {
		t.Errorf("analyticsData Stats %+v != computeStats %+v", a.Stats, wantStats)
	}
}
