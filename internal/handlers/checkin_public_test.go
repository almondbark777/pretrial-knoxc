package handlers

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"pretrial-knoxc/internal/db"
	"pretrial-knoxc/internal/models"
)

func submitCheckin(t *testing.T, srv *Server, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/checkin/submit", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "TestPhone/1.0")
	req.RemoteAddr = "203.0.113.9:51000"
	w := httptest.NewRecorder()
	srv.CheckinSubmit(w, req)
	return w
}

// An off-site GPS fix (far from the office geofence) with no valid lobby code and
// an unmatched identity must record as pending with a RED badge and the expected
// flags — the whole point of the presence assessment.
func TestPublicCheckinOffSiteScoring(t *testing.T) {
	d := testDB(t)
	srv := newServer(d)

	form := url.Values{
		"report_type":     {"Pre-Trial"},
		"client_name":     {"Pat Quinlan"},
		"dob":             {"1990-04-12"},
		"signature_name":  {"Pat Quinlan"},
		"consent_accept":  {"on"},
		"consent_version": {"2026-06-25"},
		"citation_since":  {"yes"},
		"gps_perm":        {"granted"},
		"gps_lat":         {"35.5500"}, // ~50 km south of the office default
		"gps_lng":         {"-84.0100"},
		"gps_accuracy":    {"9"},
	}
	w := submitCheckin(t, srv, form)
	if w.Code != 303 || w.Header().Get("Location") != "/checkin/done" {
		t.Fatalf("submit = %d %q, want 303 /checkin/done", w.Code, w.Header().Get("Location"))
	}

	pend, err := db.ListPendingCheckins(d)
	if err != nil || len(pend) != 1 {
		t.Fatalf("pending = %d (%v), want 1", len(pend), err)
	}
	c := pend[0]
	if c.PresenceBadge != "red" {
		t.Errorf("badge = %q, want red (off-site)", c.PresenceBadge)
	}
	for _, want := range []string{"off_site", "stale_code", "identity_unmatched"} {
		if !strings.Contains(c.Flags, want) {
			t.Errorf("flags %q missing %q", c.Flags, want)
		}
	}
	if !c.CitationSince {
		t.Error("citation_since not captured")
	}
	if c.SrcIP != "203.0.113.9" {
		t.Errorf("src ip = %q, want server-observed 203.0.113.9", c.SrcIP)
	}
	if c.RecordHash == "" {
		t.Error("record not hash-sealed")
	}
}

// A GPS fix inside the office geofence scores green.
func TestPublicCheckinOnSiteScoring(t *testing.T) {
	d := testDB(t)
	srv := newServer(d)

	form := url.Values{
		"client_name":    {"Onsite Tester"},
		"dob":            {"1985-01-01"},
		"signature_name": {"Onsite Tester"},
		"consent_accept": {"on"},
		"gps_perm":       {"granted"},
		"gps_lat":        {"35.96462"}, // office default 35.9646,-83.9202
		"gps_lng":        {"-83.92018"},
		"gps_accuracy":   {"5"},
	}
	if w := submitCheckin(t, srv, form); w.Code != 303 {
		t.Fatalf("submit = %d, want 303", w.Code)
	}
	pend, _ := db.ListPendingCheckins(d)
	if len(pend) != 1 || pend[0].PresenceBadge != "green" {
		t.Fatalf("badge = %v, want one green", badgesOf(pend))
	}
}

// No consent → bounced back to the form, nothing recorded.
func TestPublicCheckinRequiresConsent(t *testing.T) {
	d := testDB(t)
	srv := newServer(d)

	w := submitCheckin(t, srv, url.Values{
		"client_name": {"No Consent"}, "dob": {"1985-01-01"}, "signature_name": {"x"},
	})
	if w.Code != 303 || w.Header().Get("Location") != "/checkin?err=consent" {
		t.Fatalf("submit = %d %q, want 303 /checkin?err=consent", w.Code, w.Header().Get("Location"))
	}
	if n, _ := db.CountPendingCheckins(d); n != 0 {
		t.Errorf("recorded %d despite no consent, want 0", n)
	}
}

// GPS denied → can't place them → yellow, with a gps_denied flag.
func TestPublicCheckinGpsDeniedYellow(t *testing.T) {
	d := testDB(t)
	srv := newServer(d)

	w := submitCheckin(t, srv, url.Values{
		"client_name": {"Denier"}, "dob": {"1985-01-01"}, "signature_name": {"Denier"},
		"consent_accept": {"on"}, "gps_perm": {"denied"},
	})
	if w.Code != 303 {
		t.Fatalf("submit = %d, want 303", w.Code)
	}
	pend, _ := db.ListPendingCheckins(d)
	if len(pend) != 1 || pend[0].PresenceBadge != "yellow" {
		t.Fatalf("badge = %v, want one yellow", badgesOf(pend))
	}
	if !strings.Contains(pend[0].Flags, "gps_denied") {
		t.Errorf("flags %q missing gps_denied", pend[0].Flags)
	}
}

func badgesOf(cs []models.Checkin) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.PresenceBadge
	}
	return out
}
