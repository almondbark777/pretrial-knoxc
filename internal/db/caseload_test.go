package db

import "testing"

func TestLastNameInitial(t *testing.T) {
	cases := map[string]string{
		"NOBLE, JARROD BRANDON": "N", // canonical LAST, FIRST
		"bohrer, noah joseph":   "B", // lower-case is upper-cased
		"  McCammon, Lilly":     "M", // leading space trimmed
		"Aguilar Andy":          "A", // no comma → first whitespace token
		"9TEST, NINE":           "",  // non-alphabetic initial → unmapped
		"":                      "",  // empty
		", FIRSTONLY":           "",  // empty last name before the comma
	}
	for name, want := range cases {
		if got := lastNameInitial(name); got != want {
			t.Errorf("lastNameInitial(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestCaseloadAssignmentsAndLookup(t *testing.T) {
	d := openEnsured(t)

	// Nothing assigned yet → no officer (caller leaves it blank, no guess).
	if got := OfficerForLastName(d, "NOBLE, JARROD"); got != "" {
		t.Fatalf("unmapped lookup = %q, want empty", got)
	}

	if err := SetCaseloadAssignments(d, map[string]string{
		"N": "Nicholas Loveless",
		"A": "Carla Kidwell",
		"b": "Kathy Jones", // lower-case key is normalized to 'B'
	}, "sup@knoxsheriff.org"); err != nil {
		t.Fatalf("SetCaseloadAssignments: %v", err)
	}

	checks := map[string]string{
		"NOBLE, JARROD": "Nicholas Loveless", // N
		"bohrer, noah":  "Kathy Jones",       // B (lower-case name)
		"Aguilar Andy":  "Carla Kidwell",     // A, no comma
		"ZTEST, ZED":    "",                  // Z unmapped
	}
	for name, want := range checks {
		if got := OfficerForLastName(d, name); got != want {
			t.Errorf("OfficerForLastName(%q) = %q, want %q", name, got, want)
		}
	}

	// A save replaces the whole map (one owner per letter): move N, drop A and B.
	if err := SetCaseloadAssignments(d, map[string]string{"N": "Marcus Olsen"}, "sup@knoxsheriff.org"); err != nil {
		t.Fatalf("re-set: %v", err)
	}
	if got := OfficerForLastName(d, "NOBLE, JARROD"); got != "Marcus Olsen" {
		t.Errorf("after re-set, N = %q, want Marcus Olsen", got)
	}
	if got := OfficerForLastName(d, "Aguilar Andy"); got != "" {
		t.Errorf("A should be cleared by the full replace, got %q", got)
	}

	m, err := LoadCaseloadLetters(d)
	if err != nil {
		t.Fatalf("LoadCaseloadLetters: %v", err)
	}
	if len(m) != 1 || m["N"] != "Marcus Olsen" {
		t.Errorf("LoadCaseloadLetters = %v, want {N: Marcus Olsen}", m)
	}

	// Empty officer / out-of-range letter are ignored, not stored.
	if err := SetCaseloadAssignments(d, map[string]string{"N": "", "1": "Nobody", "AA": "Nobody"}, "sup"); err != nil {
		t.Fatalf("set with junk: %v", err)
	}
	if m, _ := LoadCaseloadLetters(d); len(m) != 0 {
		t.Errorf("junk assignments stored: %v", m)
	}
}
