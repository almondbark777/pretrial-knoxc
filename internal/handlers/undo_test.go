package handlers

import (
	"html/template"
	"net/http"
	"strings"
	"testing"
	"time"

	"pretrial-knoxc/internal/auth"
	"pretrial-knoxc/internal/db"
)

// TestUndoLastDelete drives the one-click restore end-to-end: delete a person,
// undo as a supervisor (newest tombstone restored, client visible again),
// verify the non-supervisor is blocked and the empty case is graceful.
func TestUndoLastDelete(t *testing.T) {
	d := testDB(t)
	a := auth.New("pw", "secret", nil, []string{"alexander.bentley@knoxsheriff.org"}, nil)
	tmpl := template.Must(template.New("").Parse(`{{define "message.html"}}{{.Title}}{{end}}`))
	srv := New(d, a, tmpl, time.Minute, false)

	clients, err := db.BuildClients(d, adminTrack)
	if err != nil {
		t.Fatalf("BuildClients: %v", err)
	}
	var idn string
	for k := range clients {
		idn = k
		break
	}
	if idn == "" {
		t.Skip("no clients in offline DB")
	}
	if err := db.DeletePerson(d, idn, "alexander.bentley@knoxsheriff.org", "undo test", false); err != nil {
		t.Fatalf("DeletePerson: %v", err)
	}
	srv.clearCache()

	// Non-supervisor is blocked.
	rec := runReq(a, srv.UndoLastDelete, "POST", "/admin/undo_last_delete", "Daniel.Harris@knoxsheriff.org")
	if rec.Code != http.StatusForbidden {
		t.Errorf("non-supervisor: status = %d, want 403", rec.Code)
	}
	if tombs, _ := db.ListTombstones(d); len(tombs) != 1 {
		t.Fatalf("tombstone count after blocked undo = %d, want 1", len(tombs))
	}

	// Supervisor undo restores the newest tombstone.
	rec = runReq(a, srv.UndoLastDelete, "POST", "/admin/undo_last_delete", "alexander.bentley@knoxsheriff.org")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("supervisor undo: status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/console/admin?msg=Restored") {
		t.Errorf("redirect = %q, want /console/admin?msg=Restored…", loc)
	}
	if tombs, _ := db.ListTombstones(d); len(tombs) != 0 {
		t.Errorf("tombstone count after undo = %d, want 0", len(tombs))
	}
	clients, err = db.BuildClients(d, adminTrack)
	if err != nil {
		t.Fatalf("BuildClients after undo: %v", err)
	}
	if len(clients[idn]) == 0 {
		t.Errorf("client %s not visible after undo", idn)
	}

	// Nothing left to undo → graceful flash, no error.
	rec = runReq(a, srv.UndoLastDelete, "POST", "/admin/undo_last_delete", "alexander.bentley@knoxsheriff.org")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("empty undo: status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "Nothing+to+undo") {
		t.Errorf("empty-undo redirect = %q, want a Nothing-to-undo flash", loc)
	}
}
