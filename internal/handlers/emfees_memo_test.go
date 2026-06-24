package handlers

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"pretrial-knoxc/internal/db"
)

// TestEMFeeMemoPostLogsOneRow (#16): the single-memo endpoint is now a POST that
// reads its target from the form body, streams the .docx, and records exactly one
// letter_log row (the no-letter-unlogged invariant). A request with no posted
// target writes nothing.
func TestEMFeeMemoPostLogsOneRow(t *testing.T) {
	d := testDB(t)
	srv := newServer(d)

	res, err := db.EMFees(d, emFeeAsOf())
	if err != nil {
		t.Fatalf("EMFees: %v", err)
	}
	if len(res.Open) == 0 {
		t.Skip("fixture has no open past-due records")
	}
	pick := res.Open[0]

	// A POST with no idn → 404 and no letter logged.
	emptyReq := httptest.NewRequest("POST", "/reports/em-fees/memo", strings.NewReader(""))
	emptyReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	emptyRec := httptest.NewRecorder()
	srv.EMFeeMemo(emptyRec, emptyReq)
	if emptyRec.Code != 404 {
		t.Errorf("empty POST: status = %d, want 404", emptyRec.Code)
	}
	if _, ok := db.LastLetters(d, "em_fees")[pick.IDN]; ok {
		t.Errorf("no letter should be logged before any successful memo")
	}

	// A valid POST → 200 docx + exactly one logged row.
	form := url.Values{"kind": {"open"}, "idn": {pick.IDN}, "case": {pick.Case}}
	req := httptest.NewRequest("POST", "/reports/em-fees/memo", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.EMFeeMemo(rec, req)
	if rec.Code != 200 {
		t.Fatalf("valid POST: status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "wordprocessingml") {
		t.Errorf("Content-Type = %q, want a .docx type", ct)
	}
	if _, ok := db.LastLetters(d, "em_fees")[pick.IDN]; !ok {
		t.Errorf("letter_log has no stamp for %s after a successful memo", pick.IDN)
	}
}
