package auth

import "testing"

// TestAllowListFallback: an unset ALLOWED_EMAILS (nil) falls back to the built-in
// defaultUsers, preserving the prior 22-user behavior.
func TestAllowListFallback(t *testing.T) {
	a := New("pw", "sec", nil, nil, nil)
	if !a.IsAllowed("alexander.bentley@knoxsheriff.org") {
		t.Error("a built-in default user should be allowed when ALLOWED_EMAILS is unset")
	}
	if !a.IsAllowed("mickey.flynt@knoxsheriff.org") {
		t.Error("mickey.flynt should be on the built-in allow-list")
	}
	if a.IsAllowed("stranger@example.com") {
		t.Error("a non-allow-list email must be rejected")
	}
}

// TestAllowListCustom: a configured allow-list REPLACES the defaults (no merge),
// and matching is case-insensitive.
func TestAllowListCustom(t *testing.T) {
	// Pass an explicit admin (the same custom user) so the break-glass default admin
	// (alexander.bentley) doesn't implicitly re-allow the built-in user this test
	// asserts is excluded.
	a := New("pw", "sec", []string{"Custom.User@knoxsheriff.org"}, nil, []string{"custom.user@knoxsheriff.org"})
	if !a.IsAllowed("custom.user@knoxsheriff.org") {
		t.Error("configured allow-list email should be allowed (case-insensitive)")
	}
	if a.IsAllowed("alexander.bentley@knoxsheriff.org") {
		t.Error("a configured allow-list should replace the defaults, not merge with them")
	}
}

// TestSupervisorMustBeAllowed: SUPERVISOR_EMAILS entries are honored only when
// they're also on the (effective) allow-list.
func TestSupervisorMustBeAllowed(t *testing.T) {
	a := New("pw", "sec",
		[]string{"sup@knoxsheriff.org", "officer@knoxsheriff.org"},
		[]string{"sup@knoxsheriff.org", "ghost@nowhere.com"}, nil)
	if !a.IsSupervisor("sup@knoxsheriff.org") {
		t.Error("an allow-listed supervisor should be recognized")
	}
	if a.IsSupervisor("ghost@nowhere.com") {
		t.Error("a supervisor not on the allow-list must be ignored")
	}
	if a.IsSupervisor("officer@knoxsheriff.org") {
		t.Error("an allow-listed non-supervisor should not be a supervisor")
	}
}

// TestRoleResolution covers the three-tier precedence: break-glass admins always
// win, a wired DB role source is authoritative once it answers, and the env lists
// are the fallback only when the DB is unavailable.
func TestRoleResolution(t *testing.T) {
	a := New("pw", "sec",
		[]string{"alice@knoxsheriff.org", "bob@knoxsheriff.org", "dave@knoxsheriff.org"},
		[]string{"bob@knoxsheriff.org"},
		[]string{"carol@knoxsheriff.org"})

	// No DB source wired → env fallback (today's behavior).
	if !a.IsAdmin("carol@knoxsheriff.org") || !a.IsSupervisor("carol@knoxsheriff.org") {
		t.Error("break-glass admin should be admin (and supervisor-or-above)")
	}
	if !a.IsSupervisor("bob@knoxsheriff.org") || a.IsAdmin("bob@knoxsheriff.org") {
		t.Error("env supervisor should be supervisor but not admin")
	}
	if !a.IsAllowed("alice@knoxsheriff.org") || a.IsSupervisor("alice@knoxsheriff.org") {
		t.Error("env officer should be allowed but not supervisor")
	}
	if a.IsAllowed("stranger@example.com") {
		t.Error("a non-listed email must be denied")
	}

	// Wire a DB source (authoritative: dbOK always true). DB promotes alice, demotes
	// bob, and omits dave entirely.
	roles := map[string]string{"alice@knoxsheriff.org": "admin", "bob@knoxsheriff.org": "officer"}
	a.SetRoleSource(func(e string) (string, bool) { return roles[e], true })
	if !a.IsAdmin("alice@knoxsheriff.org") {
		t.Error("DB role should win: alice promoted to admin")
	}
	if a.IsSupervisor("bob@knoxsheriff.org") {
		t.Error("DB role should win: bob demoted to officer")
	}
	if !a.IsAdmin("carol@knoxsheriff.org") {
		t.Error("break-glass admin must override the DB even when absent from it")
	}
	if a.IsAllowed("dave@knoxsheriff.org") {
		t.Error("DB is authoritative: a user absent from it is denied even if in the env allow-list")
	}

	// DB unavailable (dbOK=false) → fall back to the env lists.
	a.SetRoleSource(func(e string) (string, bool) { return "", false })
	if !a.IsSupervisor("bob@knoxsheriff.org") || !a.IsAllowed("dave@knoxsheriff.org") {
		t.Error("when the DB lookup fails, resolution falls back to the env lists")
	}

	if !a.IsBreakGlassAdmin("carol@knoxsheriff.org") || a.IsBreakGlassAdmin("alice@knoxsheriff.org") {
		t.Error("IsBreakGlassAdmin should report only the configured admin set")
	}
}
