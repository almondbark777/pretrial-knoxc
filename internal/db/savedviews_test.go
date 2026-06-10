package db

import "testing"

// TestSavedViews covers the lifecycle: save → list (alphabetical), upsert by
// name replaces the query, delete is scoped to the owning user, audited.
func TestSavedViews(t *testing.T) {
	d := openEnsured(t)
	const user = "tester@knoxsheriff.org"
	const page = "/console/clients"

	if err := SaveView(d, user, "", "comp=behind", page); err != errEmptyField {
		t.Fatalf("missing name: err = %v, want errEmptyField", err)
	}
	if err := SaveView(d, user, "Zebra view", "comp=missed", page); err != nil {
		t.Fatalf("SaveView #1: %v", err)
	}
	if err := SaveView(d, user, "Alpha view", "comp=behind&level=3", page); err != nil {
		t.Fatalf("SaveView #2: %v", err)
	}

	views, err := ListSavedViews(d, user, page)
	if err != nil {
		t.Fatalf("ListSavedViews: %v", err)
	}
	if len(views) != 2 || views[0].Name != "Alpha view" || views[1].Name != "Zebra view" {
		t.Fatalf("views = %+v, want [Alpha view, Zebra view] (alphabetical)", views)
	}

	// Upsert: re-saving a name replaces its query, count stays 2.
	if err := SaveView(d, user, "Alpha view", "gps=1", page); err != nil {
		t.Fatalf("SaveView upsert: %v", err)
	}
	views, _ = ListSavedViews(d, user, page)
	if len(views) != 2 || views[0].Query != "gps=1" {
		t.Errorf("after upsert: %d views, Alpha query = %q, want 2 / gps=1", len(views), views[0].Query)
	}

	// Another user can't delete it; the owner can.
	if err := DeleteSavedView(d, views[0].ID, "intruder@knoxsheriff.org"); err != nil {
		t.Fatalf("foreign delete errored: %v", err)
	}
	if got, _ := ListSavedViews(d, user, page); len(got) != 2 {
		t.Fatalf("foreign delete removed a view: %d left, want 2", len(got))
	}
	if err := DeleteSavedView(d, views[0].ID, user); err != nil {
		t.Fatalf("DeleteSavedView: %v", err)
	}
	if got, _ := ListSavedViews(d, user, page); len(got) != 1 || got[0].Name != "Zebra view" {
		t.Errorf("after delete: %+v, want just Zebra view", got)
	}

	// Audit breadcrumbs (saves + the one real delete).
	audit, _ := ListAudit(d, "", 50)
	var saves, deletes int
	for _, a := range audit {
		switch {
		case a.Action == "view_save" && a.Table == "saved_searches":
			saves++
		case a.Action == "view_delete" && a.Table == "saved_searches":
			deletes++
		}
	}
	if saves != 3 || deletes != 1 {
		t.Errorf("audit: view_save=%d view_delete=%d, want 3/1", saves, deletes)
	}
}
