package handlers

import (
	"archive/zip"
	"bytes"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"pretrial-knoxc/internal/db"
)

// The batch zip honors the report's selection exactly: only the ticked
// (kind|idn|case) records are rendered, each is recorded in letter_log, and an
// empty selection is a 400, not an everything-zip.
func TestEMFeeMemosZipSelection(t *testing.T) {
	d := testDB(t)
	srv := newServer(d)

	res, err := db.EMFees(d, emFeeAsOf())
	if err != nil {
		t.Fatalf("EMFees: %v", err)
	}
	if len(res.Open) < 2 {
		t.Skip("fixture has fewer than 2 open past-due records")
	}
	pick := res.Open[0]

	form := url.Values{"sel": {"open|" + pick.IDN + "|" + pick.Case}}
	req := httptest.NewRequest("POST", "/reports/em-fees/memos.zip",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.EMFeeMemosZip(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}

	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatalf("not a zip: %v", err)
	}
	if len(zr.File) != 1 {
		names := []string{}
		for _, f := range zr.File {
			names = append(names, f.Name)
		}
		t.Fatalf("zip has %d files, want exactly the 1 selected: %v", len(zr.File), names)
	}
	if !strings.HasPrefix(zr.File[0].Name, "Open/") {
		t.Errorf("memo path = %q, want Open/ folder", zr.File[0].Name)
	}

	// The generation is in the letter log → the report's Last-letter column.
	if _, ok := db.LastLetters(d, "em_fees")[pick.IDN]; !ok {
		t.Errorf("letter_log has no stamp for %s after batch generation", pick.IDN)
	}
	if _, ok := db.LastLetters(d, "em_fees")[res.Open[1].IDN]; ok {
		t.Errorf("unselected client %s must not be logged", res.Open[1].IDN)
	}

	// Empty selection → 400.
	req2 := httptest.NewRequest("POST", "/reports/em-fees/memos.zip", strings.NewReader(""))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec2 := httptest.NewRecorder()
	srv.EMFeeMemosZip(rec2, req2)
	if rec2.Code != 400 {
		t.Errorf("empty selection: status = %d, want 400", rec2.Code)
	}
}
