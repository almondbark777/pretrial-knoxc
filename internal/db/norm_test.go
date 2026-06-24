package db

import "testing"

// norm strips the SharePoint/Power-Automate stringified-list wrapper that some
// multi-choice columns import in (e.g. supervision type as ["Pre-Trial"]).
func TestNormUnwrapsListWrapper(t *testing.T) {
	for in, want := range map[string]string{
		`["Pre-Trial"]`:                 "Pre-Trial",
		`["Bond Supervison"]`:           "Bond Supervison",
		`["#4 SCRAM","#7 Supervision"]`: "#4 SCRAM, #7 Supervision",
		`[]`:                            "",
		`  ["GPS"]  `:                   "GPS",
		`Pretrial`:                      "Pretrial", // plain value untouched
		`@1606962, @1641152`:            "@1606962, @1641152",
		``:                              "",
	} {
		if got := norm(in); got != want {
			t.Errorf("norm(%q) = %q, want %q", in, got, want)
		}
	}
}

// canonSupervisionType folds the legacy Pre-Trial spelling to the dropdown's
// "Pretrial"; everything else passes through.
func TestCanonSupervisionType(t *testing.T) {
	for in, want := range map[string]string{
		"Pre-Trial":        "Pretrial",
		"Pre Trial":        "Pretrial",
		"pretrial":         "Pretrial",
		"PreTrial":         "Pretrial",
		"GPS":              "GPS",
		"Bond Supervision": "Bond Supervision",
		"Pretrial Release": "Pretrial Release",
		"":                 "",
	} {
		if got := canonSupervisionType(in); got != want {
			t.Errorf("canonSupervisionType(%q) = %q, want %q", in, got, want)
		}
	}
}
