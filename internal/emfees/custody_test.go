package emfees

import "testing"

// ── #4: custody credit across a real device switch ──────────────────────────
// A real ALLIED→SCRAM switch dual-bills: pre-switch days @ $8, the switch day @
// $8+$15=$23, post-switch days @ $15. In-custody days must be CREDITED at the rate
// they were billed at — crediting a $15 post-switch day at $8 (the flat-rate bug)
// overstates the owed on the show-cause letter. These hand-compute the dollars.
//
// Common scenario: ALLIED $8 → SCRAM $15, install 4/1, switch 4/16, asOf 5/1.
//
//	computeOwed: pre 15d×$8=120, switch day $23, post 15d×$15=225 → $368 / 31 days.
func switchRow() []map[string]string {
	return []map[string]string{
		reliefRow("1", "SWAP, SAM", "@100", "OPEN", "ALLIED", "4/1/2026", "SCRAM", "4/16/2026"),
	}
}

func TestCustodyCreditBeforeSwitch(t *testing.T) {
	// Custody 4/5→4/10: take-off 4/5 + reinstall 4/10 billed, only 4/6..4/9 = 4 days
	// excluded, all pre-switch @ $8 = $32. owed = 368 − 32 = 336; days = 31 − 4 = 27.
	custody := map[string][]CustodyRange{"1": {{Start: "4/5/2026", End: "4/10/2026"}}}
	res := ComputeWithCustody(switchRow(), nil, nil, custody, asOf("5/1/2026"))
	if len(res.Open) != 1 {
		t.Fatalf("open=%d want 1", len(res.Open))
	}
	r := res.Open[0]
	if r.CustodyDays != 4 || r.Days != 27 || r.Owed != 336 || r.Behind != 336 {
		t.Fatalf("before-switch: custody=%d days=%d owed=%v behind=%v want 4/27/336/336", r.CustodyDays, r.Days, r.Owed, r.Behind)
	}
}

func TestCustodyCreditAfterSwitch(t *testing.T) {
	// Custody 4/20→4/25: only 4/21..4/24 = 4 days excluded, all post-switch @ $15 =
	// $60. owed = 368 − 60 = 308; days = 27. (Flat-rate bug would credit $32 → owed $336.)
	custody := map[string][]CustodyRange{"1": {{Start: "4/20/2026", End: "4/25/2026"}}}
	res := ComputeWithCustody(switchRow(), nil, nil, custody, asOf("5/1/2026"))
	r := res.Open[0]
	if r.CustodyDays != 4 || r.Days != 27 || r.Owed != 308 || r.Behind != 308 {
		t.Fatalf("after-switch: custody=%d days=%d owed=%v behind=%v want 4/27/308/308", r.CustodyDays, r.Days, r.Owed, r.Behind)
	}
}

func TestCustodyCreditStraddlesSwitch(t *testing.T) {
	// Custody 4/14→4/19: take-off 4/14 billed, so excludes 4/15 (pre @ $8), 4/16
	// (switch day @ $23), 4/17,4/18 (post @ $15) = 4 days. credit = 1×8 + 23 + 2×15 = 61.
	// owed = 368 − 61 = 307; days = 27.
	custody := map[string][]CustodyRange{"1": {{Start: "4/14/2026", End: "4/19/2026"}}}
	res := ComputeWithCustody(switchRow(), nil, nil, custody, asOf("5/1/2026"))
	r := res.Open[0]
	if r.CustodyDays != 4 || r.Days != 27 || r.Owed != 307 || r.Behind != 307 {
		t.Fatalf("straddle-switch: custody=%d days=%d owed=%v behind=%v want 4/27/307/307", r.CustodyDays, r.Days, r.Owed, r.Behind)
	}
}

// Custody days lower the arrearage on the show-cause letter, and can drop a person
// below the 5-day threshold entirely.
func TestComputeWithCustody(t *testing.T) {
	// Installed 4/1, as-of 5/1 → 31 days × $8 = $248 owed, $0 paid.
	gps := []map[string]string{gpsRow("1", "SMITH, JOHN", "@100", "OPEN", "ALLIED", "4/1/2026")}

	// In custody 4/10 → back on GPS 4/20: both endpoints billed, only 4/11..4/19 (9
	// days) excluded → 22 × $8 = $176 behind.
	custody := map[string][]CustodyRange{"1": {{Start: "4/10/2026", End: "4/20/2026"}}}
	res := ComputeWithCustody(gps, nil, nil, custody, asOf("5/1/2026"))
	if len(res.Open) != 1 {
		t.Fatalf("open = %d, want 1", len(res.Open))
	}
	r := res.Open[0]
	if r.CustodyDays != 9 || r.Days != 22 || r.Owed != 176 || r.Behind != 176 {
		t.Fatalf("custody rec wrong: days=%d custody=%d owed=%v behind=%v", r.Days, r.CustodyDays, r.Owed, r.Behind)
	}

	// A long custody stint clears them below the 5-day threshold → no letter.
	clear := map[string][]CustodyRange{"1": {{Start: "4/2/2026", End: "5/1/2026"}}} // excludes 4/3..4/30 = 28
	res2 := ComputeWithCustody(gps, nil, nil, clear, asOf("5/1/2026"))
	if len(res2.Open) != 0 {
		t.Fatalf("open = %d, want 0 (custody cleared the arrears)", len(res2.Open))
	}

	// No custody arg behaves exactly like Compute.
	if a, b := Compute(gps, nil, nil, asOf("5/1/2026")), ComputeWithCustody(gps, nil, nil, nil, asOf("5/1/2026")); len(a.Open) != len(b.Open) || a.OpenTotal() != b.OpenTotal() {
		t.Fatalf("nil custody diverges from Compute")
	}
}
