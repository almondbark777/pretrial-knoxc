package db

import "testing"

func TestClientFlags(t *testing.T) {
	d := openEnsured(t)

	if err := AddClientFlag(d, "555", "red", "absconding risk", "officer.a"); err != nil {
		t.Fatal(err)
	}
	if err := AddClientFlag(d, "555", "amber", "owes fees", "officer.b"); err != nil {
		t.Fatal(err)
	}
	// Severity normalization: garbage → red.
	if err := AddClientFlag(d, "777", "purple", "weird", "officer.c"); err != nil {
		t.Fatal(err)
	}

	flags, err := ListActiveFlags(d, "555")
	if err != nil {
		t.Fatal(err)
	}
	if len(flags) != 2 {
		t.Fatalf("expected 2 active flags, got %d", len(flags))
	}
	// Red sorts before amber.
	if flags[0].Severity != "red" || flags[1].Severity != "amber" {
		t.Errorf("severity ordering wrong: %s then %s", flags[0].Severity, flags[1].Severity)
	}

	// Roster decoration: highest severity per idn.
	sev := ActiveFlagSeverity(d)
	if sev["555"] != "red" || sev["777"] != "red" {
		t.Errorf("severity map wrong: %v", sev)
	}

	// Clear the red flag; amber remains and the roster severity drops to amber.
	if err := ClearClientFlag(d, flags[0].ID, "officer.x"); err != nil {
		t.Fatal(err)
	}
	flags, _ = ListActiveFlags(d, "555")
	if len(flags) != 1 || flags[0].Severity != "amber" {
		t.Fatalf("after clear expected 1 amber flag, got %+v", flags)
	}
	if ActiveFlagSeverity(d)["555"] != "amber" {
		t.Error("roster severity should drop to amber after clearing the red flag")
	}

	// Empty idn rejected.
	if err := AddClientFlag(d, "", "red", "x", "y"); err == nil {
		t.Error("blank idn should be rejected")
	}
}

func TestBulletins(t *testing.T) {
	d := openEnsured(t)

	if err := AddBulletin(d, "Normal notice", "body", "normal", false, "officer.a"); err != nil {
		t.Fatal(err)
	}
	if err := AddBulletin(d, "Urgent notice", "", "high", false, "officer.b"); err != nil {
		t.Fatal(err)
	}
	if err := AddBulletin(d, "Pinned notice", "", "normal", true, "officer.c"); err != nil {
		t.Fatal(err)
	}

	list, err := ListBulletins(d)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 bulletins, got %d", len(list))
	}
	// Pinned first, then high priority, then the rest.
	if !list[0].Pinned {
		t.Errorf("pinned notice should sort first, got %q", list[0].Title)
	}
	if list[1].Priority != "high" {
		t.Errorf("high-priority should sort second, got %q (%s)", list[1].Title, list[1].Priority)
	}

	// Remove the pinned one; it drops off the active list.
	if err := RemoveBulletin(d, list[0].ID, "officer.x"); err != nil {
		t.Fatal(err)
	}
	if list, _ = ListBulletins(d); len(list) != 2 {
		t.Fatalf("after remove expected 2 bulletins, got %d", len(list))
	}

	// Blank title rejected.
	if err := AddBulletin(d, "", "x", "normal", false, "y"); err == nil {
		t.Error("blank title should be rejected")
	}
}
