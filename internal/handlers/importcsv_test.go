// importcsv_test.go — the stop-gap SharePoint CSV upload page (/console/import).
// Handler flow is tested against a stubbed ReconcileExec; the real python tool
// is exercised end-to-end in TestImportReconcileRealPython (skipped when no
// python interpreter is available).
package handlers

import (
	"bytes"
	"context"
	"html/template"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"pretrial-knoxc/internal/auth"
	"pretrial-knoxc/internal/db"
)

const supEmail = "alexander.bentley@knoxsheriff.org"

func importTestServer(t *testing.T) (*Server, *auth.Authenticator) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "import_test.db")
	d, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if err := db.EnsureSchema(d); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	a := auth.New("pw", "secret", nil, []string{supEmail})
	tmpl := template.Must(template.New("").Parse(
		`{{define "console_import.html"}}MODE={{.Mode}}|TOK={{.Token}}|ERR={{.Err}}|ADDED={{if .Sum}}{{.Sum.Totals.Added}}{{end}}{{end}}` +
			`{{define "message.html"}}{{.Title}}{{end}}`))
	srv := New(d, a, tmpl, time.Minute, false)
	srv.DBPath = dbPath
	return srv, a
}

func stubReconcile(srv *Server, runID string) {
	srv.ReconcileExec = func(ctx context.Context, dir string, apply, addsOnly bool) (*ReconcileSummary, string, error) {
		return &ReconcileSummary{
			RunID: runID, DryRun: !apply, AddsOnly: addsOnly, OK: true,
			Datasets: map[string]ReconcileCounts{"bluebook": {Added: 2}, "checkins": {Added: 3}},
			Totals:   ReconcileCounts{Added: 5, Changed: 1, Blanked: 2, SQLOnly: 4},
		}, "stub output", nil
	}
}

func do(a *auth.Authenticator, h http.HandlerFunc, req *http.Request) *httptest.ResponseRecorder {
	req.Header.Set("Cf-Access-Authenticated-User-Email", supEmail)
	rec := httptest.NewRecorder()
	a.Middleware(h).ServeHTTP(rec, req)
	return rec
}

func multipartUpload(t *testing.T, fields map[string]string, fileNames ...string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, name := range fileNames {
		fw, err := mw.CreateFormFile(name, name+" export.csv")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte("IDN,Case Number\n1,@1\n")); err != nil {
			t.Fatal(err)
		}
	}
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	mw.Close()
	req := httptest.NewRequest("POST", "/admin/import/preview", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

// TestImportSupervisorOnly: officers are blocked from the whole import surface.
func TestImportSupervisorOnly(t *testing.T) {
	srv, a := importTestServer(t)
	req := httptest.NewRequest("GET", "/console/import", nil)
	req.Header.Set("Cf-Access-Authenticated-User-Email", "Daniel.Harris@knoxsheriff.org")
	rec := httptest.NewRecorder()
	a.Middleware(http.HandlerFunc(srv.ImportPage)).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("officer GET /console/import = %d, want 403", rec.Code)
	}
}

// TestReconcileArgs pins the tool argv contract (flags the python side parses).
func TestReconcileArgs(t *testing.T) {
	got := strings.Join(reconcileArgs("s.py", "D", "x.db", "D/sum.json", false, false), " ")
	want := "s.py --dir D --db x.db --no-email --summary-json D/sum.json --dry-run"
	if got != want {
		t.Errorf("dry-run args = %q, want %q", got, want)
	}
	got = strings.Join(reconcileArgs("s.py", "D", "x.db", "D/sum.json", true, true), " ")
	want = "s.py --dir D --db x.db --no-email --summary-json D/sum.json --stamp-meta web-upload --adds-only"
	if got != want {
		t.Errorf("apply args = %q, want %q", got, want)
	}
}

// TestImportPreviewApplyFlow walks upload -> staged files -> preview -> apply:
// audit row written, cache cleared, staging removed.
func TestImportPreviewApplyFlow(t *testing.T) {
	srv, a := importTestServer(t)
	stubReconcile(srv, "RUN1")

	// Preview: all four files staged under canonical names, dry-run rendered.
	rec := do(a, srv.ImportPreview, multipartUpload(t, nil, "bluebook", "checkins", "payments", "gps"))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "MODE=preview") {
		t.Fatalf("preview: code=%d body=%q", rec.Code, rec.Body.String())
	}
	m := regexp.MustCompile(`TOK=([0-9a-f]{32})`).FindStringSubmatch(rec.Body.String())
	if m == nil {
		t.Fatalf("no staging token in %q", rec.Body.String())
	}
	token := m[1]
	staged := filepath.Join(srv.importStagingRoot(), token)
	for _, n := range []string{"bluebook.csv", "checkins.csv", "payments.csv", "gps.csv"} {
		if _, err := os.Stat(filepath.Join(staged, n)); err != nil {
			t.Errorf("staged file %s missing: %v", n, err)
		}
	}

	// Apply: done page with totals, audit row, staging cleaned up.
	form := url.Values{"token": {token}}
	req := httptest.NewRequest("POST", "/admin/import/apply", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = do(a, srv.ImportApply, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "MODE=done") || !strings.Contains(rec.Body.String(), "ADDED=5") {
		t.Fatalf("apply: code=%d body=%q", rec.Code, rec.Body.String())
	}
	if n := auditCount(t, srv.DB, "csv_reconcile", "RUN1"); n != 1 {
		t.Errorf("audit rows = %d, want 1", n)
	}
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Errorf("staging dir not removed after apply")
	}
}

// TestImportPreviewMissingFile: a partial upload is rejected and nothing is staged.
func TestImportPreviewMissingFile(t *testing.T) {
	srv, a := importTestServer(t)
	stubReconcile(srv, "RUNX")
	rec := do(a, srv.ImportPreview, multipartUpload(t, nil, "bluebook", "checkins", "payments")) // no gps
	if !strings.Contains(rec.Body.String(), "ERR=Missing file") {
		t.Fatalf("want missing-file error, got %q", rec.Body.String())
	}
	if ents, err := os.ReadDir(srv.importStagingRoot()); err == nil && len(ents) != 0 {
		t.Errorf("staging not cleaned after rejected upload: %d entries", len(ents))
	}
}

// TestImportApplyBadToken: a forged/expired token cannot reach the runner.
func TestImportApplyBadToken(t *testing.T) {
	srv, a := importTestServer(t)
	called := false
	srv.ReconcileExec = func(ctx context.Context, dir string, apply, addsOnly bool) (*ReconcileSummary, string, error) {
		called = true
		return nil, "", nil
	}
	for _, tok := range []string{"", "zzz", "../../etc", strings.Repeat("a", 32) /* non-hex */} {
		form := url.Values{"token": {tok}}
		req := httptest.NewRequest("POST", "/admin/import/apply", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := do(a, srv.ImportApply, req)
		if !strings.Contains(rec.Body.String(), "ERR=") {
			t.Errorf("token %q: want error page, got %q", tok, rec.Body.String())
		}
	}
	if called {
		t.Error("runner must not run for a bad token")
	}
	if n := auditCount(t, srv.DB, "csv_reconcile", ""); n != 0 {
		t.Errorf("unexpected audit rows: %d", n)
	}
}

// TestImportReconcileRealPython runs the REAL webapp/reconcile_import.py over a
// minimal CSV set against a fresh DB: rows insert, the run is idempotent, and
// the import_meta freshness stamp is written. Skips when python is unavailable.
func TestImportReconcileRealPython(t *testing.T) {
	py, err := exec.LookPath("python3")
	if err != nil {
		if py, err = exec.LookPath("python"); err != nil {
			t.Skip("no python on PATH")
		}
	}
	t.Setenv("PYTHON_BIN", py)
	script, err := filepath.Abs(filepath.Join("..", "..", "webapp", "reconcile_import.py"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("RECONCILE_SCRIPT", script)

	srv, _ := importTestServer(t)
	// The reconcile tool requires the raw_* tables to exist (the importer owns
	// them); a single key column is enough — the tool ALTERs in the rest.
	for _, tbl := range []string{"raw_blue_book", "raw_check_ins", "raw_payments", "raw_gps_48_hours"} {
		if _, err := srv.DB.Exec("CREATE TABLE " + tbl + " (idn NVARCHAR(500))"); err != nil {
			t.Fatal(err)
		}
	}
	dir := filepath.Join(srv.importStagingRoot(), strings.Repeat("ab", 16))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"bluebook.csv": "IDN,Defendant,Case Number,Pretrial Level,Referral Date,Case Status\n111,TEST GUY,@123,3,3/1/2026,Open\n",
		"checkins.csv": "IDN,Case Number,Date,Type of check in,Supervising Officer\n111,@123,3/5/2026 10:00,In Person,Test.Officer@knoxsheriff.org\n",
		"payments.csv": "IDN,Case Number,Payment Date,Payment Amount,Payment Type\n111,@123,3/6/2026 1:00,$20.00,GPS\n",
		"gps.csv":      "IDN,Case Number,GPS Type,GPS Install Date\n111,@123,ALLIED,3/2/2026\n",
	}
	for n, c := range files {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	sum, out, err := srv.runReconcile(ctx, dir, true, false)
	if err != nil {
		t.Fatalf("real reconcile: %v\n%s", err, out)
	}
	if sum.Totals.Added != 4 {
		t.Errorf("added = %d, want 4 (one per dataset)\n%s", sum.Totals.Added, out)
	}
	var n int
	if err := srv.DB.QueryRow("SELECT COUNT(*) FROM raw_blue_book WHERE idn='111'").Scan(&n); err != nil || n != 1 {
		t.Errorf("raw_blue_book rows = %d (%v), want 1", n, err)
	}
	if _, ok := db.LastImport(srv.DB); !ok {
		t.Error("import_meta freshness stamp not written on apply")
	}
	// Idempotency: the same files change nothing on a second run.
	sum2, out2, err := srv.runReconcile(ctx, dir, true, false)
	if err != nil {
		t.Fatalf("second run: %v\n%s", err, out2)
	}
	if sum2.Totals.Added != 0 || sum2.Totals.Changed != 0 {
		t.Errorf("second run not idempotent: +%d ~%d\n%s", sum2.Totals.Added, sum2.Totals.Changed, out2)
	}
}
