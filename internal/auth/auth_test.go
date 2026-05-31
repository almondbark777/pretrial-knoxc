package auth

import "testing"

// TestAllowListFallback: an unset ALLOWED_EMAILS (nil) falls back to the built-in
// defaultUsers, preserving the prior 22-user behavior.
func TestAllowListFallback(t *testing.T) {
	a := New("pw", "sec", nil, nil)
	if !a.IsAllowed("alexander.bentley@knoxsheriff.org") {
		t.Error("a built-in default user should be allowed when ALLOWED_EMAILS is unset")
	}
	if a.IsAllowed("stranger@example.com") {
		t.Error("a non-allow-list email must be rejected")
	}
}

// TestAllowListCustom: a configured allow-list REPLACES the defaults (no merge),
// and matching is case-insensitive.
func TestAllowListCustom(t *testing.T) {
	a := New("pw", "sec", []string{"Custom.User@knoxsheriff.org"}, nil)
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
		[]string{"sup@knoxsheriff.org", "ghost@nowhere.com"})
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
