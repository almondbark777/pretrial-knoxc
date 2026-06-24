package handlers

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"pretrial-knoxc/internal/db"
)

// TestReportProblemPersistsAndLists: a submission stores the description, the
// page, and the user-agent; a blank description is rejected (no row).
func TestReportProblemPersistsAndLists(t *testing.T) {
	d := testDB(t)
	srv := newServer(d)

	req := httptest.NewRequest("POST", "/admin/problem/report", strings.NewReader(url.Values{
		"body": {"GPS card won't save the install date"},
		"page": {"http://ptr/console/clients/12345"},
		"next": {"/console/clients/12345"},
	}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "TestBrowser/1.0")
	srv.ReportProblem(httptest.NewRecorder(), req)

	list, err := db.ListProblemReports(d, 50)
	if err != nil {
		t.Fatalf("ListProblemReports: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("reports = %d, want 1", len(list))
	}
	p := list[0]
	if !strings.Contains(p.Body, "install date") || p.Page == "" || p.UserAgent != "TestBrowser/1.0" {
		t.Errorf("unexpected report: %+v", p)
	}

	// Blank description must not create a row.
	req2 := httptest.NewRequest("POST", "/admin/problem/report", strings.NewReader("body=%20&next=/console"))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.ReportProblem(httptest.NewRecorder(), req2)
	if l2, _ := db.ListProblemReports(d, 50); len(l2) != 1 {
		t.Errorf("blank body should not insert; total reports = %d, want 1", len(l2))
	}
}
