package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"pretrial-knoxc/internal/db"
)

// postCase posts a form to one handler and asserts a redirect/OK, returning the
// recorder. Mirrors postAdd for the case-info / status / date endpoints.
func postCase(t *testing.T, h http.HandlerFunc, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusSeeOther && rec.Code != http.StatusOK {
		t.Fatalf("POST %s status=%d body=%s", path, rec.Code, rec.Body.String())
	}
	return rec
}

// TestUpdateCaseInfoPersistsAndShows: the "Edit case info" form writes changed
// fields as overrides that BuildClients splices back onto the row — so the
// computed client reflects exactly what was posted, importer-proof.
func TestUpdateCaseInfoPersistsAndShows(t *testing.T) {
	d := testDB(t)
	srv := newServer(d)
	idn := "999781222"
	postAdd(t, srv, url.Values{
		"defendant": {"EDIT, ME"}, "idn": {idn}, "warrant_case_num": {"@781222"},
	})

	postCase(t, srv.UpdateCaseInfo, "/admin/case/update", url.Values{
		"idn":              {idn},
		"charge_type":      {"ROBBERY"},
		"bond_amount":      {"$5,000"},
		"pretrial_level":   {"2"},
		"supervision_type": {"GPS"},
		"order_from":       {"Judge"},
		"birthdate":        {"01/02/1990"},
		"referral_date":    {"2026-03-15"},
	})

	clients, err := db.BuildClients(d, time.Now())
	if err != nil {
		t.Fatalf("BuildClients: %v", err)
	}
	cs := clients[idn]
	if len(cs) == 0 {
		t.Fatalf("client %s not found after case update", idn)
	}
	c := cs[0]
	for label, gw := range map[string][2]string{
		"charges":     {c.ChargeType, "ROBBERY"},
		"bond":        {c.BondAmount, "$5,000"},
		"level":       {c.Level, "2"},
		"supervision": {c.SupervisionType, "GPS"},
		"orderFrom":   {c.OrderFrom, "Judge"},
		"birthdate":   {c.Birthdate, "01/02/1990"},
	} {
		if gw[0] != gw[1] {
			t.Errorf("%s = %q, want %q", label, gw[0], gw[1])
		}
	}
	if !c.RefOK || c.RefD.Format("2006-01-02") != "2026-03-15" {
		t.Errorf("referral = %v (ok=%v), want 2026-03-15", c.RefD, c.RefOK)
	}
}

// TestToggleCaseStatusPersists: the one-click open/closed toggle writes a
// case_status override visible to every read path.
func TestToggleCaseStatusPersists(t *testing.T) {
	d := testDB(t)
	srv := newServer(d)
	idn := "999782333"
	postAdd(t, srv, url.Values{
		"defendant": {"CLOSE, ME"}, "idn": {idn}, "warrant_case_num": {"@782333"},
		"case_status": {"Open"},
	})

	postCase(t, srv.ToggleCaseStatus, "/admin/case/status", url.Values{
		"idn": {idn}, "status": {"Closed"},
	})

	clients, err := db.BuildClients(d, time.Now())
	if err != nil {
		t.Fatalf("BuildClients: %v", err)
	}
	if c := clients[idn]; len(c) == 0 || c[0].Status != "Closed" {
		t.Fatalf("status after toggle = %v, want Closed", clients[idn])
	}
}

// TestClientDatesCRUD: additional profile dates round-trip (newest-first) and
// delete cleanly.
func TestClientDatesCRUD(t *testing.T) {
	d := testDB(t)
	idn := "999783444"
	if err := db.AddClientDate(d, idn, "Referral", "2026-02-01", "second referral", "tester"); err != nil {
		t.Fatalf("AddClientDate: %v", err)
	}
	if err := db.AddClientDate(d, idn, "Court", "2026-04-10", "", "tester"); err != nil {
		t.Fatalf("AddClientDate 2: %v", err)
	}
	list, err := db.ListClientDates(d, idn)
	if err != nil {
		t.Fatalf("ListClientDates: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len = %d, want 2", len(list))
	}
	if list[0].Date != "2026-04-10" || list[1].Date != "2026-02-01" {
		t.Errorf("order = %q,%q want newest-first 2026-04-10,2026-02-01", list[0].Date, list[1].Date)
	}
	if err := db.DeleteClientDate(d, list[0].ID, "tester"); err != nil {
		t.Fatalf("DeleteClientDate: %v", err)
	}
	list2, _ := db.ListClientDates(d, idn)
	if len(list2) != 1 || list2[0].Label != "Referral" {
		t.Fatalf("after delete: %+v", list2)
	}
}
