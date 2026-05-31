package db

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

// tempLookupDB copies db/kh222.db to a temp file, ensures schema, opens it.
func tempLookupDB(t *testing.T) (string, func()) {
	t.Helper()
	src := filepath.Join("..", "..", "db", "kh222.db")
	if _, err := os.Stat(src); err != nil {
		t.Skipf("offline DB not present (%v)", err)
	}
	dst := filepath.Join(t.TempDir(), "lookup_test.db")
	in, err := os.Open(src)
	if err != nil {
		t.Fatalf("open src: %v", err)
	}
	out, err := os.Create(dst)
	if err != nil {
		in.Close()
		t.Fatalf("create dst: %v", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		t.Fatalf("copy: %v", err)
	}
	in.Close()
	out.Close()
	return dst, func() {}
}

// idnsIn returns the set of IDN values present in a remapped dataset.
func idnsIn(rows []map[string]string) map[string]bool {
	set := map[string]bool{}
	for _, r := range rows {
		set[r["IDN"]] = true
	}
	return set
}

// TestLookupDatasetsHonorsTombstoneAndOverride proves the tracker feed
// (/api/lookup_data) respects the same suppression + corrections as the rest of
// the app: a deleted person vanishes from bb/ci/pm/gp, and an override shows the
// corrected value under the SharePoint header the tracker reads.
func TestLookupDatasetsHonorsTombstoneAndOverride(t *testing.T) {
	path, _ := tempLookupDB(t)
	d, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer d.Close()
	if err := EnsureSchema(d); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	before, err := LookupDatasets(d)
	if err != nil {
		t.Fatalf("LookupDatasets: %v", err)
	}
	bb := before["bb"].([]map[string]string)
	if len(bb) == 0 {
		t.Skip("no blue_book rows")
	}
	victim := bb[0]["IDN"]
	if victim == "" {
		t.Fatal("first bb row has no IDN")
	}

	// Delete the person; they must vanish from every dataset in the tracker feed.
	if err := DeletePerson(d, victim, "sup@x", "tracker propagation test", false); err != nil {
		t.Fatalf("DeletePerson: %v", err)
	}
	after, err := LookupDatasets(d)
	if err != nil {
		t.Fatalf("LookupDatasets after: %v", err)
	}
	for _, ds := range []string{"bb", "ci", "pm", "gp"} {
		if idnsIn(after[ds].([]map[string]string))[victim] {
			t.Errorf("deleted IDN %s still present in tracker dataset %q", victim, ds)
		}
	}

	// Restore, then override a field; the tracker must show the corrected value.
	if err := RestorePerson(d, victim, "sup@x"); err != nil {
		t.Fatalf("RestorePerson: %v", err)
	}
	if err := SetOverride(d, victim, "pretrial_level", "1", "sup@x"); err != nil {
		t.Fatalf("SetOverride: %v", err)
	}
	ovd, _ := LookupDatasets(d)
	found := false
	for _, r := range ovd["bb"].([]map[string]string) {
		if r["IDN"] == victim {
			found = true
			if r["Pretrial Level "] != "1" {
				t.Errorf("override not reflected in tracker feed: %q want \"1\"", r["Pretrial Level "])
			}
		}
	}
	if !found {
		t.Errorf("restored IDN %s missing from tracker feed", victim)
	}
}
