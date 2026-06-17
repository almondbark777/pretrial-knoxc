package db

import (
	"testing"
	"time"
)

func TestAppUsersSeedAndCRUD(t *testing.T) {
	d := openEnsured(t)

	// Fresh DB: the table exists (EnsureSchema) but is unseeded.
	if roles, err := LoadUserRoles(d); err != nil || len(roles) != 0 {
		t.Fatalf("expected empty roster, got %v (err %v)", roles, err)
	}

	// Seed: higher roles win for someone on multiple lists.
	if err := SeedUsersIfEmpty(d,
		[]string{"a@k.org", "b@k.org"}, []string{"b@k.org"}, []string{"c@k.org"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	roles, _ := LoadUserRoles(d)
	if roles["a@k.org"] != "officer" || roles["b@k.org"] != "supervisor" || roles["c@k.org"] != "admin" {
		t.Fatalf("seed roles wrong: %v", roles)
	}

	// Seeding is one-time: a second call is a no-op once the table has rows.
	if err := SeedUsersIfEmpty(d, []string{"z@k.org"}, nil, nil); err != nil {
		t.Fatalf("re-seed: %v", err)
	}
	if r, _ := LoadUserRoles(d); r["z@k.org"] != "" {
		t.Error("seed should be a no-op when the table is non-empty")
	}

	// SetUserRole upserts (email lower-cased) and validates the role.
	if err := SetUserRole(d, "A@K.org", "admin", "tester"); err != nil {
		t.Fatalf("SetUserRole: %v", err)
	}
	if r, _ := LoadUserRoles(d); r["a@k.org"] != "admin" {
		t.Errorf("upsert role = %q, want admin", r["a@k.org"])
	}
	if err := SetUserRole(d, "a@k.org", "superuser", "tester"); err != errBadRole {
		t.Errorf("bad role err = %v, want errBadRole", err)
	}

	// RemoveUser revokes.
	if err := RemoveUser(d, "b@k.org", "tester"); err != nil {
		t.Fatalf("RemoveUser: %v", err)
	}
	if r, _ := LoadUserRoles(d); r["b@k.org"] != "" {
		t.Error("user not removed")
	}

	// ListAppUsers is sorted admin-first.
	list, err := ListAppUsers(d)
	if err != nil || len(list) == 0 {
		t.Fatalf("ListAppUsers: %v (n=%d)", err, len(list))
	}
	if list[0].Role != "admin" {
		t.Errorf("roster not admin-first: %+v", list)
	}
}

func TestRoleCache(t *testing.T) {
	d := openEnsured(t)
	if err := SetUserRole(d, "x@k.org", "supervisor", "t"); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	rc := NewRoleCache(d, time.Hour) // long TTL so only Invalidate refreshes

	if role, ok := rc.RoleOf("x@k.org"); !ok || role != "supervisor" {
		t.Fatalf("RoleOf(x) = %q,%v want supervisor,true", role, ok)
	}
	// Unknown user → authoritative (dbOK true) with empty role = no access.
	if role, ok := rc.RoleOf("nobody@k.org"); !ok || role != "" {
		t.Fatalf("RoleOf(unknown) = %q,%v want \"\",true", role, ok)
	}

	// A change isn't seen until the cache is invalidated (TTL is an hour).
	if err := SetUserRole(d, "x@k.org", "officer", "t"); err != nil {
		t.Fatalf("re-role: %v", err)
	}
	if role, _ := rc.RoleOf("x@k.org"); role != "supervisor" {
		t.Errorf("expected stale cached role supervisor, got %q", role)
	}
	rc.Invalidate()
	if role, _ := rc.RoleOf("x@k.org"); role != "officer" {
		t.Errorf("after Invalidate, RoleOf(x) = %q, want officer", role)
	}
}
