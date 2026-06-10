package db

import (
	"testing"
)

// TestDrugScreenCRUD covers the full lifecycle: add → list (newest first) →
// delete, with an audit breadcrumb on every write and required-field
// validation on add.
func TestDrugScreenCRUD(t *testing.T) {
	d := openEnsured(t)
	const idn = "888777666"

	// Required fields: idn + date.
	if err := AddDrugScreen(d, idn, "", "urine", "negative", "", "", "tester@knoxsheriff.org"); err != errEmptyField {
		t.Fatalf("missing date: err = %v, want errEmptyField", err)
	}
	if err := AddDrugScreen(d, "", "2026-06-01", "urine", "negative", "", "", "tester@knoxsheriff.org"); err != errEmptyField {
		t.Fatalf("missing idn: err = %v, want errEmptyField", err)
	}

	if err := AddDrugScreen(d, idn, "2026-06-01", "urine", "negative", "", "routine", "tester@knoxsheriff.org"); err != nil {
		t.Fatalf("AddDrugScreen #1: %v", err)
	}
	if err := AddDrugScreen(d, idn, "2026-06-08", "oral swab", "positive", "THC", "", "tester@knoxsheriff.org"); err != nil {
		t.Fatalf("AddDrugScreen #2: %v", err)
	}

	screens, err := ListDrugScreens(d, idn)
	if err != nil {
		t.Fatalf("ListDrugScreens: %v", err)
	}
	if len(screens) != 2 {
		t.Fatalf("want 2 screens, got %d", len(screens))
	}
	// Newest first.
	if screens[0].ScreenDate != "2026-06-08" || screens[0].Result != "positive" || screens[0].Substances != "THC" {
		t.Errorf("newest-first ordering broken: got %+v", screens[0])
	}
	if screens[1].TestType != "urine" || screens[1].Notes != "routine" {
		t.Errorf("second row fields wrong: got %+v", screens[1])
	}

	// LoadExtras carries them to the profile page.
	extras, err := LoadExtras(d, idn)
	if err != nil {
		t.Fatalf("LoadExtras: %v", err)
	}
	if len(extras.DrugScreens) != 2 {
		t.Errorf("LoadExtras.DrugScreens = %d rows, want 2", len(extras.DrugScreens))
	}

	// Cross-client list includes both.
	all, err := ListAllDrugScreens(d)
	if err != nil {
		t.Fatalf("ListAllDrugScreens: %v", err)
	}
	found := 0
	for _, s := range all {
		if s.IDN == idn {
			found++
		}
	}
	if found != 2 {
		t.Errorf("ListAllDrugScreens has %d rows for idn, want 2", found)
	}

	// Delete one; audit trail records add+add+delete.
	if err := DeleteDrugScreen(d, screens[0].ID, "tester@knoxsheriff.org"); err != nil {
		t.Fatalf("DeleteDrugScreen: %v", err)
	}
	screens, err = ListDrugScreens(d, idn)
	if err != nil {
		t.Fatalf("ListDrugScreens after delete: %v", err)
	}
	if len(screens) != 1 || screens[0].ScreenDate != "2026-06-01" {
		t.Fatalf("after delete want only the 2026-06-01 row, got %+v", screens)
	}

	audits, err := ListAudit(d, idn, 50)
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	var adds, dels int
	for _, a := range audits {
		switch {
		case a.Action == "drugscreen_add" && a.Table == "drug_screens":
			adds++
		case a.Action == "drugscreen_delete" && a.Table == "drug_screens":
			dels++
		}
	}
	if adds != 2 || dels != 1 {
		t.Errorf("audit breadcrumbs: adds=%d dels=%d, want 2/1", adds, dels)
	}
}

// TestDrugScreensPurgedOnPersonDelete pins that a whole-person delete clears
// the drug-screen log (drug_screens is in extensionTablesByIDN).
func TestDrugScreensPurgedOnPersonDelete(t *testing.T) {
	d := openEnsured(t)
	const idn = "888777555"
	if err := AddDrugScreen(d, idn, "2026-06-05", "urine", "negative", "", "", "tester@knoxsheriff.org"); err != nil {
		t.Fatalf("AddDrugScreen: %v", err)
	}
	if err := DeletePerson(d, idn, "boss@knoxsheriff.org", "test cleanup", false); err != nil {
		t.Fatalf("DeletePerson: %v", err)
	}
	screens, err := ListDrugScreens(d, idn)
	if err != nil {
		t.Fatalf("ListDrugScreens: %v", err)
	}
	if len(screens) != 0 {
		t.Errorf("drug screens should be purged on person delete, got %d rows", len(screens))
	}
}

// TestListAllDrugScreensTolerant pins the table-missing read path: a snapshot
// that predates the feature returns nil without erroring.
func TestListAllDrugScreensTolerant(t *testing.T) {
	path, _ := tempLookupDB(t)
	d, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if tableExists(d, "drug_screens") {
		t.Skip("offline snapshot already has drug_screens")
	}
	all, err := ListAllDrugScreens(d)
	if err != nil || all != nil {
		t.Errorf("ListAllDrugScreens on pre-feature DB = (%v, %v), want (nil, nil)", all, err)
	}
}
