package db

import "testing"

// TestPinToggle covers the lifecycle: toggle on → pinned (listed, newest
// first), toggle off → gone, with an audit breadcrumb each way and
// required-field validation.
func TestPinToggle(t *testing.T) {
	d := openEnsured(t)
	const user = "tester@knoxsheriff.org"
	const idn = "777666555"

	if _, err := TogglePin(d, "", idn); err != errEmptyField {
		t.Fatalf("missing user: err = %v, want errEmptyField", err)
	}
	if _, err := TogglePin(d, user, ""); err != errEmptyField {
		t.Fatalf("missing idn: err = %v, want errEmptyField", err)
	}

	pinned, err := TogglePin(d, user, idn)
	if err != nil || !pinned {
		t.Fatalf("first toggle = (%v, %v), want (true, nil)", pinned, err)
	}
	if !IsPinned(d, user, idn) {
		t.Error("IsPinned = false after pin")
	}
	if IsPinned(d, "someone.else@knoxsheriff.org", idn) {
		t.Error("pin leaked to another user")
	}

	// Second pin, newest first ordering.
	if _, err := TogglePin(d, user, "111222333"); err != nil {
		t.Fatalf("second pin: %v", err)
	}
	idns, err := PinnedIDNs(d, user)
	if err != nil {
		t.Fatalf("PinnedIDNs: %v", err)
	}
	if len(idns) != 2 || idns[0] != "111222333" || idns[1] != idn {
		t.Errorf("PinnedIDNs = %v, want [111222333 %s] (newest first)", idns, idn)
	}

	// Toggle off.
	pinned, err = TogglePin(d, user, idn)
	if err != nil || pinned {
		t.Fatalf("second toggle = (%v, %v), want (false, nil)", pinned, err)
	}
	if IsPinned(d, user, idn) {
		t.Error("IsPinned = true after unpin")
	}

	// Audit breadcrumbs for both directions.
	audit, err := ListAudit(d, idn, 50)
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	var adds, removes int
	for _, a := range audit {
		switch {
		case a.Action == "pin_add" && a.Table == "pinned_defendants":
			adds++
		case a.Action == "pin_remove" && a.Table == "pinned_defendants":
			removes++
		}
	}
	if adds != 1 || removes != 1 {
		t.Errorf("audit rows: pin_add=%d pin_remove=%d, want 1 each", adds, removes)
	}
}

// TestPinsPurgedOnPersonDelete pins that a whole-person delete clears every
// user's pin of that defendant (pinned_defendants is in extensionTablesByIDN).
func TestPinsPurgedOnPersonDelete(t *testing.T) {
	d := openEnsured(t)
	const idn = "777666444"
	if _, err := TogglePin(d, "a@knoxsheriff.org", idn); err != nil {
		t.Fatalf("pin: %v", err)
	}
	if _, err := TogglePin(d, "b@knoxsheriff.org", idn); err != nil {
		t.Fatalf("pin: %v", err)
	}
	if err := DeletePerson(d, idn, "boss@knoxsheriff.org", "test cleanup", false); err != nil {
		t.Fatalf("DeletePerson: %v", err)
	}
	if IsPinned(d, "a@knoxsheriff.org", idn) || IsPinned(d, "b@knoxsheriff.org", idn) {
		t.Error("pins should be purged on person delete")
	}
}
