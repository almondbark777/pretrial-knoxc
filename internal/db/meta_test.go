package db

import (
	"path/filepath"
	"testing"
	"time"
)

// LastImport must be tolerant: no table (pre-rollout DBs, the offline fixture)
// and junk values read as "no stamp", never an error; a real stamp parses.
func TestLastImport(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "meta_test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer d.Close()

	if _, ok := LastImport(d); ok {
		t.Error("missing table should read as no stamp, got ok=true")
	}

	if _, err := d.Exec(`CREATE TABLE import_meta (key TEXT PRIMARY KEY, value TEXT)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, ok := LastImport(d); ok {
		t.Error("missing row should read as no stamp, got ok=true")
	}

	if _, err := d.Exec(`INSERT INTO import_meta VALUES('last_import','not-a-time')`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, ok := LastImport(d); ok {
		t.Error("unparseable value should read as no stamp, got ok=true")
	}

	// The exact format sharepoint_import.py writes.
	if _, err := d.Exec(`UPDATE import_meta SET value='2026-06-10T11:45:00Z' WHERE key='last_import'`); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, ok := LastImport(d)
	if !ok {
		t.Fatal("valid stamp should parse, got ok=false")
	}
	want := time.Date(2026, 6, 10, 11, 45, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("LastImport = %v, want %v", got, want)
	}
}
