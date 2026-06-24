package handlers

import (
	"encoding/json"
	"testing"
	"time"

	"pretrial-knoxc/internal/compute"
)

// The PTR-check page parses the dropped export in the browser; the Go side only
// has to (a) format each referral so the page's M/D/YYYY parser reads it and
// (b) emit valid, complete JSON for the live Blue Book side. These guard both.

func TestPtrCheckRefStr(t *testing.T) {
	withTime := &compute.Client{RefDT: time.Date(2026, 1, 11, 10, 19, 0, 0, time.UTC), RefDTOK: true}
	if got := ptrCheckRefStr(withTime); got != "1/11/2026 10:19" {
		t.Fatalf("timestamped ref = %q, want 1/11/2026 10:19", got)
	}
	dateOnly := &compute.Client{RefD: time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC), RefOK: true}
	if got := ptrCheckRefStr(dateOnly); got != "3/2/2026" {
		t.Fatalf("date-only ref = %q, want 3/2/2026", got)
	}
	if got := ptrCheckRefStr(&compute.Client{}); got != "" {
		t.Fatalf("blank ref = %q, want empty string", got)
	}
}

func TestPtrCheckBlueBookJSON(t *testing.T) {
	clients := map[string][]*compute.Client{
		"123": {{
			IDN: "123", Name: "DOE, JANE", CaseNo: "@1, @2", Level: "2",
			SupervisionType: "Pre-Trial", OrderFrom: "Judge", Status: "OPEN",
			RefDT: time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC), RefDTOK: true,
		}},
	}
	var rows []ptrCheckRow
	if err := json.Unmarshal([]byte(ptrCheckBlueBookJSON(clients)), &rows); err != nil {
		t.Fatalf("embedded JSON is not valid: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	r := rows[0]
	if r.IDN != "123" || r.Name != "DOE, JANE" || r.Cases != "@1, @2" ||
		r.Level != "2" || r.Sup != "Pre-Trial" || r.Order != "Judge" ||
		r.Status != "OPEN" || r.Ref != "6/1/2026 09:30" {
		t.Fatalf("unexpected row: %+v", r)
	}
}
