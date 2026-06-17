package handlers

import (
	"html/template"
	"net/http"
	"testing"
	"time"

	"pretrial-knoxc/internal/auth"
	"pretrial-knoxc/internal/db"
)

// TestUserManagement exercises the admin-only user/role handlers end to end: a
// supervisor is blocked, an admin can add/re-role, the break-glass admin can't be
// demoted or removed, and a removal revokes access. Form values ride in the query
// string (r.FormValue reads them) so runReq — which can't set a body — still works.
func TestUserManagement(t *testing.T) {
	d := testDB(t)
	// Seed so the DB role source is authoritative for sup (not just empty). alex is
	// the break-glass admin (nil → default in auth.New).
	if err := db.SeedUsersIfEmpty(d,
		[]string{"sup@knoxsheriff.org", "alexander.bentley@knoxsheriff.org"},
		[]string{"sup@knoxsheriff.org"},
		[]string{"alexander.bentley@knoxsheriff.org"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := auth.New("pw", "secret", []string{"sup@knoxsheriff.org"}, []string{"sup@knoxsheriff.org"}, nil)
	rc := db.NewRoleCache(d, time.Hour)
	a.SetRoleSource(rc.RoleOf)
	tmpl := template.Must(template.New("").Parse(`{{define "message.html"}}{{.Title}}{{end}}`))
	srv := New(d, a, tmpl, time.Minute, false)
	srv.Roles = rc

	const alex = "alexander.bentley@knoxsheriff.org"

	// A supervisor (not admin) is blocked from managing users.
	rec := runReq(a, srv.SaveUser, "POST", "/admin/user/save?email=newbie@knoxsheriff.org&role=officer", "sup@knoxsheriff.org")
	if rec.Code != http.StatusForbidden {
		t.Errorf("supervisor SaveUser: got %d, want 403", rec.Code)
	}

	// The admin can add a user; it takes effect (cache invalidated on write).
	rec = runReq(a, srv.SaveUser, "POST", "/admin/user/save?email=newbie@knoxsheriff.org&role=supervisor", alex)
	if rec.Code != http.StatusSeeOther {
		t.Errorf("admin SaveUser: got %d, want 303", rec.Code)
	}
	if !a.IsSupervisor("newbie@knoxsheriff.org") {
		t.Error("newly added user should resolve as supervisor")
	}

	// The break-glass admin can't be demoted.
	runReq(a, srv.SaveUser, "POST", "/admin/user/save?email="+alex+"&role=officer", alex)
	if !a.IsAdmin(alex) {
		t.Error("break-glass admin must stay admin after a demote attempt")
	}

	// Removing a user revokes access.
	runReq(a, srv.RemoveUser, "POST", "/admin/user/remove?email=newbie@knoxsheriff.org", alex)
	if a.IsAllowed("newbie@knoxsheriff.org") {
		t.Error("removed user should lose access")
	}

	// The break-glass admin can't be removed.
	runReq(a, srv.RemoveUser, "POST", "/admin/user/remove?email="+alex, alex)
	if !a.IsAdmin(alex) {
		t.Error("break-glass admin must remain after a remove attempt")
	}
}
