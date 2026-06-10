package handlers

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"pretrial-knoxc/internal/models"
)

// The global search must find a client by case number — officers start from
// court paperwork that names a case ("@1606962" or just the digits), not the
// person's IDN. The match scans every case the person has.
func TestAPILookupByCaseNumber(t *testing.T) {
	d := testDB(t)
	srv := newServer(d)
	clients, err := srv.clients()
	if err != nil {
		t.Fatalf("clients: %v", err)
	}

	// Pick a real case number out of the fixture.
	var idn, caseNo string
	for id, cases := range clients {
		for _, c := range cases {
			if len(strings.TrimPrefix(c.CaseNo, "@")) >= 5 {
				idn, caseNo = id, c.CaseNo
				break
			}
		}
		if caseNo != "" {
			break
		}
	}
	if caseNo == "" {
		t.Skip("fixture has no case numbers")
	}

	for _, q := range []string{caseNo, strings.TrimPrefix(caseNo, "@")} {
		rec := httptest.NewRecorder()
		srv.APILookup(rec, httptest.NewRequest("GET", "/api/lookup?q="+url.QueryEscape(q), nil))
		var hits []models.SearchHit
		if err := json.Unmarshal(rec.Body.Bytes(), &hits); err != nil {
			t.Fatalf("q=%q: bad JSON: %v", q, err)
		}
		found := false
		for _, h := range hits {
			if h.IDN == idn {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("q=%q: IDN %s (case %s) not in %d hits", q, idn, caseNo, len(hits))
		}
	}
}
