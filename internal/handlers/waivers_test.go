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

// TestFeeWaiverHandlers drives grant/clear through the real middleware: the
// non-supervisor is blocked at the handler boundary, the supervisor's grant
// persists and redirects back to the record with a flash, clear undoes it.
func TestFeeWaiverHandlers(t *testing.T) {
	d := testDB(t)
	a := auth.New("pw", "secret", nil, []string{"alexander.bentley@knoxsheriff.org"})
	tmpl := template.Must(template.New("").Parse(`{{define "message.html"}}{{.Title}}{{end}}`))
	srv := New(d, a, tmpl, time.Minute, false)
	const idn = "999000555"
	if err := db.AddDefendant(d, db.NewDefendant{
		IDN: idn, Name: "ZZWAIVE, WEB", Level: "2", Status: "Open", GPS: "true",
	}, "alexander.bentley@knoxsheriff.org"); err != nil {
		t.Fatalf("AddDefendant: %v", err)
	}

	// Non-supervisor is blocked; nothing written.
	rec := runReq(a, srv.SetFeeWaiver, "POST", "/admin/waiver?idn="+idn+"&reason=x", "Daniel.Harris@knoxsheriff.org")
	if rec.Code != http.StatusForbidden {
		t.Errorf("non-supervisor grant: status = %d, want 403", rec.Code)
	}
	if db.HasFeeWaiver(d, idn) {
		t.Fatal("blocked grant still wrote a waiver")
	}

	// Supervisor grant persists and lands back on the record with a flash.
	rec = runReq(a, srv.SetFeeWaiver, "POST", "/admin/waiver?idn="+idn+"&reason=indigent", "alexander.bentley@knoxsheriff.org")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("grant: status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/console/clients/"+idn+"?msg=") {
		t.Errorf("grant redirect = %q, want /console/clients/%s?msg=…", loc, idn)
	}
	if !db.HasFeeWaiver(d, idn) {
		t.Fatal("waiver not written by supervisor grant")
	}

	// Non-supervisor can't clear it either.
	rec = runReq(a, srv.ClearFeeWaiver, "POST", "/admin/waiver/clear?idn="+idn, "Daniel.Harris@knoxsheriff.org")
	if rec.Code != http.StatusForbidden {
		t.Errorf("non-supervisor clear: status = %d, want 403", rec.Code)
	}
	if !db.HasFeeWaiver(d, idn) {
		t.Fatal("blocked clear removed the waiver")
	}

	// Supervisor clear removes it.
	rec = runReq(a, srv.ClearFeeWaiver, "POST", "/admin/waiver/clear?idn="+idn, "alexander.bentley@knoxsheriff.org")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("clear: status = %d, want 303", rec.Code)
	}
	if db.HasFeeWaiver(d, idn) {
		t.Error("waiver survived supervisor clear")
	}
}
