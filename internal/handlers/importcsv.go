// importcsv.go — supervisor-gated web upload for the four SharePoint
// "Export to CSV" files, as a STOP-GAP while the site is in testing and the
// daily email import may lag the SharePoint lists.
//
// The Go app deliberately does NOT write raw_* itself (CLAUDE.md rule); it
// shells out to webapp/reconcile_import.py — the proven importer-family tool —
// which INSERTs rows missing from SQL, optionally updates changed fields,
// and NEVER deletes (rows in SQL but not in the CSV are kept; an empty CSV
// cell never blanks a stored value). Every applied run is recorded in the
// tool's import_change_log + text log, and in the app's audit_log.
//
// Flow: upload 4 files -> staged under <db_dir>/import_uploads/<token>/ ->
// dry-run preview (counts, nothing written) -> Apply re-runs the same staged
// files for real -> cache cleared so the site shows the new data immediately.
package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"pretrial-knoxc/internal/db"
)

// ReconcileCounts mirrors one dataset's counters in the tool's --summary-json.
type ReconcileCounts struct {
	Added     int `json:"added"`
	Changed   int `json:"changed"`
	Unchanged int `json:"unchanged"`
	CSVDups   int `json:"csv_dups"`
	SQLOnly   int `json:"sql_only"`
	Blanked   int `json:"blanked"`
	// Skipped is set on a per-dataset row when the tool couldn't process that
	// file (empty, or its headers don't match the expected key columns — usually
	// the wrong export in that slot). Never set on the Totals row.
	Skipped bool `json:"skipped"`
}

// ReconcileSummary mirrors the tool's --summary-json output.
type ReconcileSummary struct {
	RunID    string                     `json:"run_id"`
	DryRun   bool                       `json:"dry_run"`
	AddsOnly bool                       `json:"adds_only"`
	Datasets map[string]ReconcileCounts `json:"datasets"`
	Totals   ReconcileCounts            `json:"totals"`
	LogPath  string                     `json:"log_path"`
	OK       bool                       `json:"ok"`
}

// importDatasets maps form-field/canonical-file names to display labels, in
// the same order the reconcile tool processes them.
var importDatasets = []struct{ Name, Label string }{
	{"bluebook", "New Blue Book"},
	{"checkins", "Check Ins"},
	{"payments", "Payments"},
	{"gps", "GPS 48 Hours"},
}

var importTokenRe = regexp.MustCompile(`^[0-9a-f]{32}$`)

const importMaxUpload = 64 << 20 // 64 MB across the 4 files — far above real exports

// importStagingRoot is where uploaded CSV sets wait between preview and apply.
// Lives next to the DB (like import_logs/) so it's app-writable on ptr1.
func (s *Server) importStagingRoot() string {
	return filepath.Join(filepath.Dir(s.DBPath), "import_uploads")
}

// reconcileArgs builds the tool's argv (minus the python binary). Pure for tests.
func reconcileArgs(script, dir, dbPath, summaryPath string, apply, addsOnly bool) []string {
	args := []string{script, "--dir", dir, "--db", dbPath, "--no-email", "--summary-json", summaryPath}
	if apply {
		// A committed web sync counts as a data refresh for the console footer.
		args = append(args, "--stamp-meta", "web-upload")
	} else {
		args = append(args, "--dry-run")
	}
	if addsOnly {
		args = append(args, "--adds-only")
	}
	return args
}

// pythonBin locates the interpreter: PYTHON_BIN env, else python3, else python.
func pythonBin() (string, error) {
	if v := os.Getenv("PYTHON_BIN"); v != "" {
		return v, nil
	}
	for _, c := range []string{"python3", "python"} {
		if p, err := exec.LookPath(c); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("no python3/python on PATH (set PYTHON_BIN)")
}

// runReconcile invokes webapp/reconcile_import.py over a staged CSV dir and
// parses its JSON summary. Tests stub via s.ReconcileExec. The returned string
// is the tool's combined output (shown collapsed on the page for diagnosis).
func (s *Server) runReconcile(ctx context.Context, dir string, apply, addsOnly bool) (*ReconcileSummary, string, error) {
	if s.ReconcileExec != nil {
		return s.ReconcileExec(ctx, dir, apply, addsOnly)
	}
	py, err := pythonBin()
	if err != nil {
		return nil, "", err
	}
	script := os.Getenv("RECONCILE_SCRIPT")
	if script == "" {
		script = filepath.Join("webapp", "reconcile_import.py") // CWD=/opt/ptr-knoxc on ptr1; repo root in dev
	}
	summaryPath := filepath.Join(dir, "summary.json")
	// Drop any summary left by an earlier run (the preview's dry-run summary lives
	// in the same dir): its presence afterward must mean THIS run wrote it, so a
	// crashed apply can't be read as a success via a stale file.
	_ = os.Remove(summaryPath)
	cmd := exec.CommandContext(ctx, py, reconcileArgs(script, dir, s.DBPath, summaryPath, apply, addsOnly)...)
	out, runErr := cmd.CombinedOutput()
	b, readErr := os.ReadFile(summaryPath)
	if readErr != nil {
		// No summary == the run failed before finishing (bad CSV set, locked DB…).
		if runErr != nil {
			return nil, string(out), fmt.Errorf("reconcile failed: %v", runErr)
		}
		return nil, string(out), fmt.Errorf("reconcile wrote no summary: %v", readErr)
	}
	var sum ReconcileSummary
	if err := json.Unmarshal(b, &sum); err != nil {
		return nil, string(out), fmt.Errorf("bad summary json: %v", err)
	}
	if runErr != nil {
		return &sum, string(out), fmt.Errorf("reconcile exited with error: %v", runErr)
	}
	return &sum, string(out), nil
}

// importViewRows orders the per-dataset counts for the template.
type importViewRow struct {
	Label string
	ReconcileCounts
}

func importViewRows(sum *ReconcileSummary) []importViewRow {
	rows := make([]importViewRow, 0, len(importDatasets))
	for _, d := range importDatasets {
		rows = append(rows, importViewRow{Label: d.Label, ReconcileCounts: sum.Datasets[d.Name]})
	}
	return rows
}

// anySkipped reports whether the tool skipped any dataset (empty / wrong-columns
// file). A skipped dataset means that export reconciled nothing — surfaced as a
// loud warning so a mis-slotted upload isn't mistaken for "already up to date".
func anySkipped(sum *ReconcileSummary) bool {
	for _, c := range sum.Datasets {
		if c.Skipped {
			return true
		}
	}
	return false
}

// tailStr keeps the last n characters (whole lines) of the tool output for display.
func tailStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	t := s[len(s)-n:]
	if i := strings.IndexByte(t, '\n'); i >= 0 && i+1 < len(t) {
		t = t[i+1:]
	}
	return "… (truncated)\n" + t
}

func (s *Server) importBase(w http.ResponseWriter, r *http.Request) map[string]any {
	data := s.consoleBase(w, r, "admin", s.trackFrom(r))
	data["CSRF"] = s.Auth.CSRF(w, r)
	data["Mode"] = "form"
	return data
}

// pruneStaleStaging drops abandoned upload dirs (preview never applied) after a day.
// Holds importMu so it can't delete a dir that a concurrent apply is reading; if an
// import is running it simply skips this pass (the next page visit prunes).
func (s *Server) pruneStaleStaging() {
	if !s.importMu.TryLock() {
		return
	}
	defer s.importMu.Unlock()
	root := s.importStagingRoot()
	ents, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range ents {
		if !e.IsDir() || !importTokenRe.MatchString(e.Name()) {
			continue
		}
		if fi, err := e.Info(); err == nil && time.Since(fi.ModTime()) > 24*time.Hour {
			_ = os.RemoveAll(filepath.Join(root, e.Name()))
		}
	}
}

// ImportPage renders the upload form. GET /console/import (supervisor).
func (s *Server) ImportPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireSupervisor(w, r); !ok {
		return
	}
	s.pruneStaleStaging()
	data := s.importBase(w, r)
	s.renderConsole(w, "console_import.html", data)
}

// importError re-renders the form with an error banner.
func (s *Server) importError(w http.ResponseWriter, r *http.Request, msg string, output string) {
	data := s.importBase(w, r)
	data["Err"] = msg
	data["Output"] = tailStr(output, 4000)
	s.renderConsole(w, "console_import.html", data)
}

// ImportPreview stages the 4 uploaded CSVs and dry-runs the reconcile.
// POST /admin/import/preview (CSRF via the /admin group; supervisor here).
func (s *Server) ImportPreview(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireSupervisor(w, r); !ok {
		return
	}
	if err := r.ParseMultipartForm(importMaxUpload); err != nil {
		s.importError(w, r, "Upload too large or malformed: "+err.Error(), "")
		return
	}
	tok := make([]byte, 16)
	if _, err := rand.Read(tok); err != nil {
		s.importError(w, r, "token: "+err.Error(), "")
		return
	}
	token := hex.EncodeToString(tok)
	dir := filepath.Join(s.importStagingRoot(), token)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		s.importError(w, r, "staging dir: "+err.Error(), "")
		return
	}
	for _, d := range importDatasets {
		f, hdr, err := r.FormFile(d.Name)
		if err != nil {
			_ = os.RemoveAll(dir)
			s.importError(w, r, "Missing file: "+d.Label+" — all four exports are required.", "")
			return
		}
		dst, err := os.Create(filepath.Join(dir, d.Name+".csv")) // canonical stem the tool recognizes
		if err == nil {
			_, err = io.Copy(dst, f)
			dst.Close()
		}
		f.Close()
		if err != nil {
			_ = os.RemoveAll(dir)
			s.importError(w, r, "Saving "+hdr.Filename+": "+err.Error(), "")
			return
		}
	}
	addsOnly := r.FormValue("addsonly") != ""
	if !s.importMu.TryLock() {
		_ = os.RemoveAll(dir)
		s.importError(w, r, "Another import is running — wait for it to finish and try again.", "")
		return
	}
	defer s.importMu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	sum, out, err := s.runReconcile(ctx, dir, false, addsOnly)
	if err != nil {
		_ = os.RemoveAll(dir)
		s.importError(w, r, "Preview failed: "+err.Error(), out)
		return
	}
	data := s.importBase(w, r)
	data["Mode"] = "preview"
	data["Sum"] = sum
	data["Rows"] = importViewRows(sum)
	data["Token"] = token
	data["AddsOnly"] = addsOnly
	data["Skipped"] = anySkipped(sum)
	data["Output"] = tailStr(out, 4000)
	s.renderConsole(w, "console_import.html", data)
}

// importStagingFor validates a preview token and returns its staging dir.
func (s *Server) importStagingFor(token string) (string, error) {
	if !importTokenRe.MatchString(token) {
		return "", fmt.Errorf("bad token")
	}
	dir := filepath.Join(s.importStagingRoot(), token)
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("staged files not found (already applied, discarded, or expired) — upload again")
	}
	return dir, nil
}

// ImportApply re-runs the previewed staging dir for real, audits, and clears
// the cache. POST /admin/import/apply (supervisor).
func (s *Server) ImportApply(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireSupervisor(w, r)
	if !ok {
		return
	}
	dir, err := s.importStagingFor(r.FormValue("token"))
	if err != nil {
		s.importError(w, r, err.Error(), "")
		return
	}
	addsOnly := r.FormValue("addsonly") != ""
	if !s.importMu.TryLock() {
		s.importError(w, r, "Another import is running — wait for it to finish and try again.", "")
		return
	}
	defer s.importMu.Unlock()
	// Detach from the request context: a client disconnect or the Cloudflare
	// proxy timeout (~100s) must NOT kill the reconcile mid-commit. importMu
	// (single-flight) and the 10-minute cap bound it.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	sum, out, err := s.runReconcile(ctx, dir, true, addsOnly)
	// "Committed" is decided by a fresh, non-dry-run summary — the tool writes it
	// immediately after COMMIT — not by the exit code. A nonzero exit during the
	// tool's post-commit bookkeeping must not be reported as "nothing committed"
	// (which would also skip the audit row and cache clear on committed data).
	committed := sum != nil && !sum.DryRun
	if !committed {
		msg := "Apply failed (nothing committed — the tool runs one transaction)"
		if err != nil {
			msg += ": " + err.Error()
		}
		s.importError(w, r, msg, out)
		return
	}
	mode := ""
	if addsOnly {
		mode = " (adds only)"
	}
	if aerr := db.WriteAudit(s.DB, db.AuditEvent{
		User: user, Action: "csv_reconcile", Table: "raw_*", RowID: sum.RunID,
		NewValue: fmt.Sprintf("web upload%s: +%d added, ~%d changed, %d blanks-kept, %d kept-not-in-csv",
			mode, sum.Totals.Added, sum.Totals.Changed, sum.Totals.Blanked, sum.Totals.SQLOnly),
	}); aerr != nil {
		// The data is committed; an audit-write failure is logged, not hidden
		// (the tool's own import_change_log is the backstop record).
		log.Printf("csv_reconcile audit write failed (data committed, run %s): %v", sum.RunID, aerr)
	}
	s.clearCache()
	_ = os.RemoveAll(dir)
	data := s.importBase(w, r)
	data["Mode"] = "done"
	data["Sum"] = sum
	data["Rows"] = importViewRows(sum)
	data["AddsOnly"] = addsOnly
	data["Skipped"] = anySkipped(sum)
	data["Output"] = tailStr(out, 4000)
	s.renderConsole(w, "console_import.html", data)
}

// ImportDiscard throws away a previewed staging dir. POST /admin/import/discard.
// Holds importMu so a discard can't delete the dir out from under a running apply.
func (s *Server) ImportDiscard(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireSupervisor(w, r); !ok {
		return
	}
	if !s.importMu.TryLock() {
		redirectMsg(w, r, "/console/import", "An import is running — try discarding again once it finishes.")
		return
	}
	defer s.importMu.Unlock()
	if dir, err := s.importStagingFor(r.FormValue("token")); err == nil {
		_ = os.RemoveAll(dir)
	}
	redirectMsg(w, r, "/console/import", "Upload discarded — nothing was written.")
}
