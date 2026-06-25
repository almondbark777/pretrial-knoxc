package handlers

import (
	"database/sql"
	"encoding/csv"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"pretrial-knoxc/internal/compute"
	"pretrial-knoxc/internal/db"
	"pretrial-knoxc/internal/models"
)

// ensuredLettersDB is a bare temp DB with the schema provisioned — no offline
// snapshot needed, so this test always runs (it stubs the roster).
func ensuredLettersDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "letters_test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if err := db.EnsureSchema(d); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	return d
}

// TestLetterReportRows pins the pure formatter: names resolve from the roster,
// an unknown IDN falls back to the IDN itself, and the type token is labeled.
func TestLetterReportRows(t *testing.T) {
	names := clientNames(map[string][]*compute.Client{
		"111": {{IDN: "111", Name: "Jane Doe"}},
		"222": {{IDN: "222", Name: ""}}, // blank name → not mapped
	})
	if names["111"] != "Jane Doe" {
		t.Errorf("clientNames[111] = %q, want Jane Doe", names["111"])
	}
	if _, ok := names["222"]; ok {
		t.Errorf("blank-name client should not be mapped")
	}

	rows := letterReportRows([]models.LetterLogEntry{
		{IDN: "111", Case: "@1", Type: "em_fees", Detail: "behind $40.00 · open", GeneratedBy: "a@knoxsheriff.org", CreatedAt: "2026-06-25 09:00:00 EDT"},
		{IDN: "999", Case: "@9", Type: "em_fees", Detail: "behind $5.00 · open", GeneratedBy: "b@knoxsheriff.org", CreatedAt: "2026-06-24 09:00:00 EDT"},
	}, names)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	if len(rows[0]) != len(letterReportColumns) {
		t.Fatalf("row width %d != header width %d", len(rows[0]), len(letterReportColumns))
	}
	if rows[0][1] != "Jane Doe" {
		t.Errorf("known IDN name = %q, want Jane Doe", rows[0][1])
	}
	if rows[1][1] != "999" {
		t.Errorf("unknown IDN should fall back to IDN, got %q", rows[1][1])
	}
	if rows[0][4] != "Past-due EM fee" {
		t.Errorf("type label = %q, want Past-due EM fee", rows[0][4])
	}
	if strings.Contains(rows[0][6], "@") {
		t.Errorf("By column should be a display name, not an email: %q", rows[0][6])
	}
}

// TestExportLettersCSV exercises the export end-to-end: logged letters come back
// newest-first, with the roster name spliced in and an unknown IDN passed
// through. (No templates touched — the CSV path renders without them.)
func TestExportLettersCSV(t *testing.T) {
	d := ensuredLettersDB(t)
	srv := newServer(d)
	srv.buildClientsFunc = func() (map[string][]*compute.Client, error) {
		return map[string][]*compute.Client{
			"111": {{IDN: "111", Name: "Jane Doe"}},
		}, nil
	}

	if err := db.LogLetters(d, "officer@knoxsheriff.org", "em_fees", []db.LetterRef{
		{IDN: "111", Case: "@1", Detail: "behind $40.00 · open"},
	}); err != nil {
		t.Fatalf("LogLetters 1: %v", err)
	}
	if err := db.LogLetters(d, "officer@knoxsheriff.org", "em_fees", []db.LetterRef{
		{IDN: "999", Case: "@9", Detail: "behind $5.00 · open"},
	}); err != nil {
		t.Fatalf("LogLetters 2: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/export/letters.csv", nil)
	srv.ExportLetters(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/csv") {
		t.Errorf("Content-Type = %q, want text/csv", ct)
	}

	recs, err := csv.NewReader(strings.NewReader(rec.Body.String())).ReadAll()
	if err != nil {
		t.Fatalf("parse CSV: %v", err)
	}
	if len(recs) != 3 { // header + 2 letters
		t.Fatalf("got %d CSV lines, want 3", len(recs))
	}
	if recs[0][0] != letterReportColumns[0] || len(recs[0]) != len(letterReportColumns) {
		t.Errorf("header mismatch: %v", recs[0])
	}
	// Newest first: 999 (second batch) leads, name unknown so IDN passes through.
	if recs[1][2] != "999" {
		t.Errorf("first data row IDN = %q, want 999 (newest)", recs[1][2])
	}
	// Older 111 row resolves to its roster name.
	if recs[2][2] != "111" || recs[2][1] != "Jane Doe" {
		t.Errorf("second data row = %v, want IDN 111 / Jane Doe", recs[2])
	}
}
