package handlers

import (
	"database/sql"
	"encoding/json"
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pretrial-knoxc/internal/auth"
	"pretrial-knoxc/internal/compute"
	"pretrial-knoxc/internal/db"
	"pretrial-knoxc/internal/models"
)

// track for these tests matches the golden trackDate so the offline rosters are
// populated (the snapshot is stale, so "today" yields sparse rosters).
var adminTrack = compute.Noon(2026, 5, 30)

// testDB copies the offline DB to a temp file (so the committed copy is never
// mutated), runs EnsureSchema, and opens it.
func testDB(t *testing.T) *sql.DB {
	t.Helper()
	src := filepath.Join("..", "..", "db", "kh222.db")
	if _, err := os.Stat(src); err != nil {
		t.Skipf("offline DB not present (%v) — skipping admin tests", err)
	}
	dst := filepath.Join(t.TempDir(), "admin_test.db")
	cp(t, src, dst)
	d, err := db.Open(dst)
	if err != nil {
		t.Fatalf("open temp DB: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if err := db.EnsureSchema(d); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	return d
}

func cp(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatalf("open src: %v", err)
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		t.Fatalf("create dst: %v", err)
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		t.Fatalf("copy: %v", err)
	}
}

// presence is the victim's presence across every view, recomputed from a fresh
// BuildClients so we exercise the real read path (no cache).
type presence struct {
	build, grid, missed, behind, lookup bool
}

func checkPresence(t *testing.T, srv *Server, d *sql.DB, idn, name string) presence {
	t.Helper()
	clients, err := db.BuildClients(d, adminTrack)
	if err != nil {
		t.Fatalf("BuildClients: %v", err)
	}
	p := presence{}
	_, p.build = clients[idn]
	for _, row := range defendantRows(clients, adminTrack) {
		if row.IDN == idn {
			p.grid = true
		}
	}
	for _, row := range missedCheckInsRoster(clients, adminTrack) {
		if row.IDN == idn {
			p.missed = true
		}
	}
	for _, row := range behindRoster(clients, adminTrack) {
		if row.IDN == idn {
			p.behind = true
		}
	}
	// Lookup goes through the live handler (cache cleared so it rebuilds).
	srv.clearCache()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/lookup?q="+name, nil)
	srv.APILookup(rec, req)
	var hits []models.SearchHit
	_ = json.Unmarshal(rec.Body.Bytes(), &hits)
	for _, h := range hits {
		if h.IDN == idn {
			p.lookup = true
		}
	}
	return p
}

func auditCount(t *testing.T, d *sql.DB, action, rowID string) int {
	t.Helper()
	var n int
	if err := d.QueryRow("SELECT COUNT(*) FROM audit_log WHERE action = ? AND row_id = ?", action, rowID).Scan(&n); err != nil {
		t.Fatalf("audit count: %v", err)
	}
	return n
}

func newServer(d *sql.DB) *Server {
	a := auth.New("pw", "secret", nil, []string{"alexander.bentley@knoxsheriff.org"})
	return New(d, a, nil, time.Minute, false)
}

// TestDeleteSuppressesEverywhere is the headline guarantee: deleting an IDN
// removes it from BuildClients, the grid, every roster, and lookup — and a
// restore brings it back. An audit row is written for each action.
func TestDeleteSuppressesEverywhere(t *testing.T) {
	d := testDB(t)
	srv := newServer(d)

	// Pick a victim guaranteed to be on the missed roster (open, non-L1, no
	// check-in this month in the stale snapshot).
	clients, err := db.BuildClients(d, adminTrack)
	if err != nil {
		t.Fatalf("BuildClients: %v", err)
	}
	missed := missedCheckInsRoster(clients, adminTrack)
	if len(missed) == 0 {
		t.Skip("no missed-roster clients in offline DB")
	}
	victim := missed[0].IDN
	name := missed[0].Name
	searchTerm := name
	if len(searchTerm) > 4 {
		searchTerm = searchTerm[:4]
	}

	// Before: present in build/grid/missed/lookup.
	before := checkPresence(t, srv, d, victim, searchTerm)
	if !before.build || !before.grid || !before.missed || !before.lookup {
		t.Fatalf("victim %s (%s) not present before delete: %+v", victim, name, before)
	}

	// Delete the whole person.
	if err := db.DeletePerson(d, victim, "alexander.bentley@knoxsheriff.org", "wrong entry", false); err != nil {
		t.Fatalf("DeletePerson: %v", err)
	}
	after := checkPresence(t, srv, d, victim, searchTerm)
	if after.build || after.grid || after.missed || after.behind || after.lookup {
		t.Fatalf("victim %s still present after delete: %+v", victim, after)
	}
	if auditCount(t, d, "delete_person", victim) != 1 {
		t.Errorf("expected 1 delete_person audit row for %s", victim)
	}

	// A second BuildClients (simulating the next import-driven rebuild) — still gone.
	stillGone := checkPresence(t, srv, d, victim, searchTerm)
	if stillGone.build || stillGone.missed {
		t.Errorf("victim reappeared on rebuild (tombstone not honored): %+v", stillGone)
	}

	// Restore brings them back everywhere.
	if err := db.RestorePerson(d, victim, "alexander.bentley@knoxsheriff.org"); err != nil {
		t.Fatalf("RestorePerson: %v", err)
	}
	restored := checkPresence(t, srv, d, victim, searchTerm)
	if !restored.build || !restored.grid || !restored.missed || !restored.lookup {
		t.Fatalf("victim %s not restored: %+v", victim, restored)
	}
	if auditCount(t, d, "restore_person", victim) != 1 {
		t.Errorf("expected 1 restore_person audit row for %s", victim)
	}
}

// TestDeleteCaseKeepsPerson verifies single-case granularity: deleting one case
// token of a multi-case person suppresses that case while the person and their
// other cases remain.
func TestDeleteCaseKeepsPerson(t *testing.T) {
	d := testDB(t)

	clients, err := db.BuildClients(d, adminTrack)
	if err != nil {
		t.Fatalf("BuildClients: %v", err)
	}
	// Find a multi-case IDN with a token contained in only SOME of its rows, so
	// deleting that token leaves at least one row standing (the person survives).
	// (Some grouped-token IDNs share every token across rows — deleting one token
	// correctly removes them all; those aren't the case to assert "person remains".)
	var idn, tok string
	var rowsWith int
	for k, cs := range clients {
		if len(cs) < 2 {
			continue
		}
		for _, cand := range caseOptions(cs) {
			n := 0
			for _, c := range cs {
				for _, t := range compute.CaseTokens(c.CaseNo) {
					if t == cand {
						n++
						break
					}
				}
			}
			if n >= 1 && n < len(cs) {
				idn, tok, rowsWith = k, cand, n
				break
			}
		}
		if idn != "" {
			break
		}
	}
	if idn == "" {
		t.Skip("no multi-case IDN with a partial-coverage case token in offline DB")
	}
	beforeRows := len(clients[idn])

	if err := db.DeleteCase(d, idn, tok, "alexander.bentley@knoxsheriff.org", "duplicate case", false); err != nil {
		t.Fatalf("DeleteCase: %v", err)
	}
	clients2, _ := db.BuildClients(d, adminTrack)
	after := clients2[idn]
	if len(after) == 0 {
		t.Fatalf("whole IDN %s vanished after a single-case delete", idn)
	}
	if len(after) != beforeRows-rowsWith {
		t.Errorf("IDN %s rows after case delete = %d, want %d (before %d, token in %d rows)",
			idn, len(after), beforeRows-rowsWith, beforeRows, rowsWith)
	}
	for _, o := range caseOptions(after) {
		if o == tok {
			t.Errorf("deleted case token %s still present in caseOptions for %s", tok, idn)
		}
	}
	if auditCount(t, d, "delete_case", idn) != 1 {
		t.Errorf("expected 1 delete_case audit row for %s", idn)
	}

	// Restore the case.
	if err := db.RestoreCase(d, idn, tok, "alexander.bentley@knoxsheriff.org"); err != nil {
		t.Fatalf("RestoreCase: %v", err)
	}
	clients3, _ := db.BuildClients(d, adminTrack)
	if len(clients3[idn]) != beforeRows {
		t.Errorf("case not restored: before=%d now=%d", beforeRows, len(clients3[idn]))
	}
}

// TestOverrideApplies proves a supervisor override is spliced into BuildClients
// after the raw read (so compute uses the corrected value) and is flagged on the
// Client, with audit rows for set + clear.
func TestOverrideApplies(t *testing.T) {
	d := testDB(t)

	clients, err := db.BuildClients(d, adminTrack)
	if err != nil {
		t.Fatalf("BuildClients: %v", err)
	}
	// Find an IDN whose level is NOT 1, so overriding to L1 is an observable change.
	var idn string
	for k, cs := range clients {
		lvl, _ := compute.ParseLevel(openRep(cs).Level)
		if lvl == 2 || lvl == 3 {
			idn = k
			break
		}
	}
	if idn == "" {
		t.Skip("no L2/L3 client in offline DB")
	}

	if err := db.SetOverride(d, idn, "pretrial_level", "1", "alexander.bentley@knoxsheriff.org"); err != nil {
		t.Fatalf("SetOverride: %v", err)
	}
	clients2, _ := db.BuildClients(d, adminTrack)
	rep := openRep(clients2[idn])
	if rep.Level != "1" {
		t.Errorf("override not applied to Level: got %q want \"1\"", rep.Level)
	}
	if rep.Overrides["pretrial_level"] != "1" {
		t.Errorf("override not flagged on Client.Overrides: %+v", rep.Overrides)
	}
	// Compute must see L1: flat $20 one-time PTR fee.
	ptr := compute.ComputePTRFees(*rep, adminTrack, "")
	if ptr.TotalOwed != 20 || len(ptr.MonthsOwed) != 1 {
		t.Errorf("override did not feed compute: PTR owed=%d months=%d want 20/1", ptr.TotalOwed, len(ptr.MonthsOwed))
	}
	if auditCount(t, d, "override_set", idn) != 1 {
		t.Errorf("expected 1 override_set audit row for %s", idn)
	}

	// Clear reverts.
	if err := db.ClearOverride(d, idn, "pretrial_level", "alexander.bentley@knoxsheriff.org"); err != nil {
		t.Fatalf("ClearOverride: %v", err)
	}
	clients3, _ := db.BuildClients(d, adminTrack)
	rep3 := openRep(clients3[idn])
	if rep3.Level == "1" {
		t.Errorf("override not cleared: Level still %q", rep3.Level)
	}
	if len(rep3.Overrides) != 0 {
		t.Errorf("override flag not cleared: %+v", rep3.Overrides)
	}
	if auditCount(t, d, "override_clear", idn) != 1 {
		t.Errorf("expected 1 override_clear audit row for %s", idn)
	}
}

// runReq drives a handler through the real auth.Middleware so the user context
// is set exactly as in production — identity comes from the trusted Cf-Access
// header (the email must be on the allow-list).
func runReq(a *auth.Authenticator, h http.HandlerFunc, method, path, asEmail string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if asEmail != "" {
		req.Header.Set("Cf-Access-Authenticated-User-Email", asEmail)
	}
	rec := httptest.NewRecorder()
	a.Middleware(h).ServeHTTP(rec, req)
	return rec
}

// TestSupervisorGating confirms the supervisor wins / non-supervisor is blocked
// at the real handler boundary, while an officer-level CRUD write succeeds and
// is audited.
func TestSupervisorGating(t *testing.T) {
	d := testDB(t)
	a := auth.New("pw", "secret", nil, []string{"alexander.bentley@knoxsheriff.org"})
	// A minimal template so the HTML 403 path (message.html) can render.
	tmpl := template.Must(template.New("").Parse(`{{define "message.html"}}{{.Title}}{{end}}`))
	srv := New(d, a, tmpl, time.Minute, false)

	if !a.IsSupervisor("alexander.bentley@knoxsheriff.org") {
		t.Fatal("expected configured supervisor")
	}
	if a.IsSupervisor("Daniel.Harris@knoxsheriff.org") {
		t.Fatal("non-supervisor should not be a supervisor")
	}

	// Supervisor passes requireSupervisor; non-supervisor gets a 403.
	supOK := false
	runReq(a, func(w http.ResponseWriter, r *http.Request) {
		_, supOK = srv.requireSupervisor(w, r)
	}, "POST", "/admin/delete", "alexander.bentley@knoxsheriff.org")
	if !supOK {
		t.Error("supervisor did not pass requireSupervisor")
	}

	denied := false
	rec := runReq(a, func(w http.ResponseWriter, r *http.Request) {
		_, ok := srv.requireSupervisor(w, r)
		denied = !ok
	}, "POST", "/admin/delete", "Daniel.Harris@knoxsheriff.org")
	if !denied {
		t.Error("non-supervisor passed requireSupervisor")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 for non-supervisor, got %d", rec.Code)
	}

	// Officer CRUD (note add) works for any allowed user and audits.
	clients, _ := db.BuildClients(d, adminTrack)
	var idn string
	for k := range clients {
		idn = k
		break
	}
	if err := db.AddNote(d, idn, "test note from officer", "Daniel.Harris@knoxsheriff.org"); err != nil {
		t.Fatalf("AddNote: %v", err)
	}
	notes, err := db.ListNotes(d, idn)
	if err != nil || len(notes) == 0 {
		t.Fatalf("note not stored: %v (n=%d)", err, len(notes))
	}
	if auditCount(t, d, "note_add", idn) != 1 {
		t.Errorf("expected 1 note_add audit row for %s", idn)
	}
}
