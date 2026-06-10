package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The classic interface was removed (2026-06-09); these pin that its bookmarks
// land on the right console page, with query context carried over.
func TestLegacyRedirects(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
		url     string
		want    string
	}{
		{"dashboard", redirectTo("/console"), "/dashboard", "/console"},
		{"profile with idn", legacyProfileRedirect, "/client_profile.html?idn=12345", "/console/clients/12345"},
		{"profile without idn", legacyProfileRedirect, "/client_profile.html", "/console/clients"},
		{"calendar plain", legacyCalendarRedirect, "/calendar.html", "/console/calendar"},
		{"calendar with params", legacyCalendarRedirect, "/calendar.html?idn=12345&month=2026-05", "/console/calendar?idn=12345&month=2026-05"},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", c.url, nil)
		w := httptest.NewRecorder()
		c.handler(w, r)
		if w.Code != http.StatusFound {
			t.Errorf("%s: status = %d, want 302", c.name, w.Code)
		}
		if loc := w.Header().Get("Location"); loc != c.want {
			t.Errorf("%s: Location = %q, want %q", c.name, loc, c.want)
		}
	}
}
