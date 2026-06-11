// freshness_test.go — the "Data updated …" stamp every page header renders
// via the dataFreshness template func.
package handlers

import (
	"database/sql"
	"testing"
	"time"
)

func mustExec(t *testing.T, d *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := d.Exec(q, args...); err != nil {
		t.Fatalf("exec %s: %v", q, err)
	}
}

func TestDataFreshness(t *testing.T) {
	srv, _ := importTestServer(t) // EnsureSchema'd empty DB, no import_meta

	// Pre-rollout DB: no stamp, page shows nothing.
	if f := srv.DataFreshness(); f.OK {
		t.Fatalf("expected OK=false with no import_meta, got %+v", f)
	}

	// Fresh daily import 2h ago.
	mustExec(t, srv.DB, "CREATE TABLE import_meta (key TEXT PRIMARY KEY, value TEXT)")
	stamp := func(ts time.Time, mode string) {
		mustExec(t, srv.DB, "INSERT INTO import_meta(key,value) VALUES('last_import',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", ts.UTC().Format(time.RFC3339))
		mustExec(t, srv.DB, "INSERT INTO import_meta(key,value) VALUES('last_import_mode',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", mode)
	}
	stamp(time.Now().Add(-2*time.Hour), "full")
	f := srv.DataFreshness()
	if !f.OK || f.Stale || f.Mode != "daily import" || f.Ago != "2h ago" || f.Label == "" {
		t.Errorf("fresh daily import: %+v", f)
	}

	// Stale web upload 30h ago.
	stamp(time.Now().Add(-30*time.Hour), "web-upload")
	f = srv.DataFreshness()
	if !f.OK || !f.Stale || f.Mode != "web CSV upload" {
		t.Errorf("stale web upload: %+v", f)
	}
}

func TestAgoStr(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "just now"},
		{5 * time.Minute, "5m ago"},
		{3 * time.Hour, "3h ago"},
		{47 * time.Hour, "47h ago"},
		{72 * time.Hour, "3d ago"},
	}
	for _, c := range cases {
		if got := agoStr(c.d); got != c.want {
			t.Errorf("agoStr(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}
