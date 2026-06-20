package emfees

import "testing"

// Custody days lower the arrearage on the show-cause letter, and can drop a person
// below the 5-day threshold entirely.
func TestComputeWithCustody(t *testing.T) {
	// Installed 4/1, as-of 5/1 → 31 days × $8 = $248 owed, $0 paid.
	gps := []map[string]string{gpsRow("1", "SMITH, JOHN", "@100", "OPEN", "ALLIED", "4/1/2026")}

	// In custody 4/10 → back on GPS 4/20 = 10 days excluded → 21 × $8 = $168 behind.
	custody := map[string][]CustodyRange{"1": {{Start: "4/10/2026", End: "4/20/2026"}}}
	res := ComputeWithCustody(gps, nil, nil, custody, asOf("5/1/2026"))
	if len(res.Open) != 1 {
		t.Fatalf("open = %d, want 1", len(res.Open))
	}
	r := res.Open[0]
	if r.CustodyDays != 10 || r.Days != 21 || r.Owed != 168 || r.Behind != 168 {
		t.Fatalf("custody rec wrong: days=%d custody=%d owed=%v behind=%v", r.Days, r.CustodyDays, r.Owed, r.Behind)
	}

	// A long custody stint clears them below the 5-day threshold → no letter.
	clear := map[string][]CustodyRange{"1": {{Start: "4/2/2026", End: "5/1/2026"}}} // excludes 4/2..4/30 = 29
	res2 := ComputeWithCustody(gps, nil, nil, clear, asOf("5/1/2026"))
	if len(res2.Open) != 0 {
		t.Fatalf("open = %d, want 0 (custody cleared the arrears)", len(res2.Open))
	}

	// No custody arg behaves exactly like Compute.
	if a, b := Compute(gps, nil, nil, asOf("5/1/2026")), ComputeWithCustody(gps, nil, nil, nil, asOf("5/1/2026")); len(a.Open) != len(b.Open) || a.OpenTotal() != b.OpenTotal() {
		t.Fatalf("nil custody diverges from Compute")
	}
}
