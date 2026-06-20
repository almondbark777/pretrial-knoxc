package emfees

import (
	"testing"
	"time"
)

func d(s string) time.Time {
	t, ok := parseDate(s)
	if !ok {
		panic("bad test date " + s)
	}
	return t
}

func asOf(s string) time.Time { return d(s) }

// ---- parsing helpers ----

func TestParseDate(t *testing.T) {
	cases := []struct {
		in        string
		ok        bool
		y, m, day int
	}{
		{"5/8/2026", true, 2026, 5, 8},
		{"5/8/2026 14:30", true, 2026, 5, 8}, // trailing time-of-day dropped
		{"12/31/2025", true, 2025, 12, 31},
		{"2026-05-08", true, 2026, 5, 8},           // canonical ISO (reconcile tool)
		{"2026-5-8", true, 2026, 5, 8},             // ISO without leading zeros
		{"2026-05-08T00:00:00Z", true, 2026, 5, 8}, // ISO with time-of-day dropped
		{"", false, 0, 0, 0},
		{"   ", false, 0, 0, 0},
		{"not-a-date", false, 0, 0, 0},
	}
	for _, c := range cases {
		got, ok := parseDate(c.in)
		if ok != c.ok {
			t.Fatalf("parseDate(%q) ok=%v want %v", c.in, ok, c.ok)
		}
		if ok && (got.Year() != c.y || int(got.Month()) != c.m || got.Day() != c.day) {
			t.Fatalf("parseDate(%q)=%v want %d-%d-%d", c.in, got, c.y, c.m, c.day)
		}
	}
}

func TestParseAmount(t *testing.T) {
	cases := map[string]float64{
		"$1,234.00": 1234, "1234": 1234, "": 0, "$0.00": 0, "  $48.50 ": 48.5, "junk": 0,
	}
	for in, want := range cases {
		if got := parseAmount(in); got != want {
			t.Fatalf("parseAmount(%q)=%v want %v", in, got, want)
		}
	}
}

func TestMoney(t *testing.T) {
	cases := map[float64]string{
		0: "$0.00", 248: "$248.00", 1300: "$1,300.00",
		1234567.5: "$1,234,567.50", -50: "-$50.00",
	}
	for in, want := range cases {
		if got := Money(in); got != want {
			t.Fatalf("Money(%v)=%q want %q", in, got, want)
		}
	}
}

func TestMemoFilename(t *testing.T) {
	got := MemoFilename(Rec{Name: "SMITH, JOHN A", IDN: "123"})
	if got != "SMITH_JOHN_A_123.docx" {
		t.Fatalf("MemoFilename = %q", got)
	}
	// strips unsafe chars
	got = MemoFilename(Rec{Name: "O'NEIL/JANE", IDN: "9"})
	if got != "ONEILJANE_9.docx" {
		t.Fatalf("MemoFilename sanitize = %q", got)
	}
}

// ---- billing math (compute_owed) ----

func TestComputeOwedNoSwitch(t *testing.T) {
	// Apr 15 install through May 15 = 31 inclusive days at $8.
	days, owed := computeOwed(d("4/15/2026"), d("5/15/2026"), 8, 0, time.Time{}, false)
	if days != 31 || owed != 248 {
		t.Fatalf("no-switch: days=%d owed=%v want 31/248", days, owed)
	}
}

func TestComputeOwedSwitch(t *testing.T) {
	// Install Apr 15 (ALLIED $8), switch to SCRAM $15 on Apr 25, end May 15.
	// pre=10*8, switch day=8+15, post=20*15 -> 80+23+300=403; days=10+1+20=31.
	days, owed := computeOwed(d("4/15/2026"), d("5/15/2026"), 8, 15, d("4/25/2026"), true)
	if days != 31 || owed != 403 {
		t.Fatalf("switch: days=%d owed=%v want 31/403", days, owed)
	}
}

func TestComputeOwedSwitchOutsideWindowIgnored(t *testing.T) {
	// Switch date after the period end is ignored -> plain math.
	days, owed := computeOwed(d("4/15/2026"), d("5/15/2026"), 8, 15, d("6/1/2026"), true)
	if days != 31 || owed != 248 {
		t.Fatalf("switch-outside: days=%d owed=%v want 31/248", days, owed)
	}
}

// ---- Compute: Open pass over the 48-hour file ----

func gpsRow(idn, name, caseNo, status, gtype, install string) map[string]string {
	return map[string]string{
		"idn": idn, "defendant": name, "case_number": caseNo,
		"case_status": status, "gps_type": gtype, "gps_install_date": install,
	}
}

func payRow(idn, caseNo, ptype, amt, date string) map[string]string {
	return map[string]string{
		"idn": idn, "case_number": caseNo, "payment_type": ptype,
		"payment_amount": amt, "payment_date": date,
	}
}

func TestComputeOpenThresholdAndRates(t *testing.T) {
	gps := []map[string]string{
		gpsRow("1", "SMITH, JOHN", "@100", "OPEN", "ALLIED", "4/1/2026"), // 31d*8=248 behind -> in
		gpsRow("2", "DOE, JANE", "@200", "OPEN", "ALLIED", "4/28/2026"),  // 4d -> below 5-day threshold -> out
		gpsRow("3", "ROE, RICH", "@300", "OPEN", "SCRAM", "4/1/2026"),    // 31d*15=465 behind -> in
	}
	res := Compute(gps, nil, nil, asOf("5/1/2026"))
	if len(res.Open) != 2 || len(res.Closed) != 0 {
		t.Fatalf("open=%d closed=%d want 2/0", len(res.Open), len(res.Closed))
	}
	byIDN := map[string]Rec{}
	for _, r := range res.Open {
		byIDN[r.IDN] = r
	}
	if r := byIDN["1"]; r.Behind != 248 || r.Type != "ALLIED" || r.Days != 31 {
		t.Fatalf("idn1 = %+v", r)
	}
	if r := byIDN["3"]; r.Behind != 465 || r.Type != "SCRAM" {
		t.Fatalf("idn3 = %+v", r)
	}
	if _, ok := byIDN["2"]; ok {
		t.Fatalf("idn2 should be below threshold")
	}
}

func TestPaymentTypeFilterAndCaseFallback(t *testing.T) {
	gps := []map[string]string{
		gpsRow("1", "A B", "@100", "OPEN", "ALLIED", "4/1/2026"), // owed 248
		gpsRow("2", "C D", "@200", "OPEN", "ALLIED", "4/1/2026"), // owed 248
	}
	pays := []map[string]string{
		payRow("1", "@100", "GPS", "100", "4/2/2026"),         // counts (case match)
		payRow("1", "@100", "Drug Screen", "500", "4/2/2026"), // excluded type
		payRow("1", "@100", "PTR Fee", "500", "4/2/2026"),     // excluded type
		payRow("2", "@999", "Scram", "40", "4/2/2026"),        // idn2: no @200 payments -> byCase 0 -> falls back to byIdn 40
	}
	res := Compute(gps, pays, nil, asOf("5/1/2026"))
	byIDN := map[string]Rec{}
	for _, r := range res.Open {
		byIDN[r.IDN] = r
	}
	if r := byIDN["1"]; r.Paid != 100 || r.Behind != 148 {
		t.Fatalf("idn1 payment filter wrong: paid=%v behind=%v", r.Paid, r.Behind)
	}
	if r := byIDN["2"]; r.Paid != 40 || r.Behind != 208 {
		t.Fatalf("idn2 case->idn fallback wrong: paid=%v behind=%v", r.Paid, r.Behind)
	}
}

func TestJunkAndNoTypeSkipped(t *testing.T) {
	gps := []map[string]string{
		gpsRow("1", "!!!scratch", "@1", "OPEN", "ALLIED", "4/1/2026"),
		gpsRow("2", "TEST DUMMY", "@2", "OPEN", "ALLIED", "4/1/2026"),
		gpsRow("3", "REAL ONE", "@3", "OPEN", "IN CUSTODY", "4/1/2026"), // no billable type, no bb fallback
		gpsRow("4", "BILLABLE", "@4", "OPEN", "SCRAM", "4/1/2026"),
	}
	res := Compute(gps, nil, nil, asOf("5/1/2026"))
	if len(res.Open) != 1 || res.Open[0].IDN != "4" {
		t.Fatalf("expected only idn4, got %+v", res.Open)
	}
	if res.SkippedJunk != 2 {
		t.Fatalf("SkippedJunk=%d want 2", res.SkippedJunk)
	}
	if res.SkippedNoType != 1 {
		t.Fatalf("SkippedNoType=%d want 1", res.SkippedNoType)
	}
}

// TestOpenNameFallsBackToBlueBook reproduces the live-export defect where the GPS
// 48-hour file ships a blank Defendant column for every row: the Open show-cause
// record must still get its name from the blue book (by IDN) so the letter is never
// printed nameless. The junk filter must also run on the resolved name.
func TestOpenNameFallsBackToBlueBook(t *testing.T) {
	gps := []map[string]string{
		gpsRow("1", "", "@100", "OPEN", "ALLIED", "4/1/2026"), // blank name (live export)
		gpsRow("2", "", "@200", "OPEN", "SCRAM", "4/1/2026"),  // blank name, junk in blue book
	}
	bb := []map[string]string{
		{"idn": "1", "defendant": "SMITH, JOHN", "case_status": "OPEN", "warrant_case_num": "@100"},
		{"idn": "2", "defendant": "TEST DUMMY", "case_status": "OPEN", "warrant_case_num": "@200"},
	}
	res := Compute(gps, nil, bb, asOf("5/1/2026"))
	if len(res.Open) != 1 {
		t.Fatalf("open=%d want 1 (idn2 is junk via blue-book name)", len(res.Open))
	}
	if res.Open[0].Name != "SMITH, JOHN" {
		t.Fatalf("name not recovered from blue book: %q", res.Open[0].Name)
	}
	if res.SkippedJunk != 1 {
		t.Fatalf("SkippedJunk=%d want 1 (idn2 resolved to a TEST name)", res.SkippedJunk)
	}
}

// reliefRow is a 48-hour row that also records a "Switched To"/"Switched GPS Date".
func reliefRow(idn, name, caseNo, status, gtype, install, switchedTo, switchDate string) map[string]string {
	r := gpsRow(idn, name, caseNo, status, gtype, install)
	r["switched_to"] = switchedTo
	r["switched_gps_date"] = switchDate
	return r
}

func TestComputeReliefSwitchFreezesBilling(t *testing.T) {
	// Off GPS / relieved on Apr 15 — billing must FREEZE there, not run to as-of
	// (May 1). Apr 1 -> Apr 15 inclusive = 15 days at $8 = $120, not 31 days/$248.
	gps := []map[string]string{
		reliefRow("1", "ABSHER, KELLY", "@100", "OPEN", "ALLIED", "4/1/2026", "No GPS", "4/15/2026"),
	}
	res := Compute(gps, nil, nil, asOf("5/1/2026"))
	if len(res.Open) != 1 {
		t.Fatalf("open=%d want 1", len(res.Open))
	}
	r := res.Open[0]
	if r.Days != 15 || r.Owed != 120 || r.Behind != 120 {
		t.Fatalf("relief freeze: days=%d owed=%v behind=%v want 15/120/120", r.Days, r.Owed, r.Behind)
	}
	if !r.End.Equal(d("4/15/2026")) {
		t.Fatalf("relief freeze end=%v want 4/15/2026", r.End)
	}
}

func TestComputeReliefSwitchPaidUpDropsOffList(t *testing.T) {
	// The reported scenario: relieved Apr 15, paid through the frozen window. With
	// the freeze she is within threshold and drops off; without it she'd still owe
	// days she was never on a device and stay on the past-due list (the PTR1 bug).
	gps := []map[string]string{
		reliefRow("1", "ABSHER, KELLY", "@100", "OPEN", "SCRAM", "4/1/2026", "No GPS", "4/15/2026"),
	}
	pays := []map[string]string{payRow("1", "@100", "SCRAM", "225", "4/16/2026")} // 15d*15 = 225 (paid up)
	res := Compute(gps, pays, nil, asOf("6/1/2026"))
	if len(res.Open) != 0 {
		t.Fatalf("paid-up relieved client should drop off, got %+v", res.Open)
	}
}

func TestComputeRealDeviceSwitchNotFrozen(t *testing.T) {
	// A genuine ALLIED->SCRAM switch is NOT a relief — billing runs to as-of and
	// dual-bills across the switch date. Apr 1 install ALLIED, switch SCRAM Apr 25,
	// as-of May 15: pre 10*8=80, switch day 8+15=23, post 20*15=300 => 403 / 31 days.
	gps := []map[string]string{
		reliefRow("1", "ROE, RICH", "@100", "OPEN", "ALLIED", "4/15/2026", "SCRAM", "4/25/2026"),
	}
	res := Compute(gps, nil, nil, asOf("5/15/2026"))
	if len(res.Open) != 1 {
		t.Fatalf("open=%d want 1", len(res.Open))
	}
	if r := res.Open[0]; r.Days != 31 || r.Owed != 403 || !r.HasSwitch {
		t.Fatalf("device switch should dual-bill to as-of: %+v", r)
	}
}

func TestComputeReliefSwitchOutsideWindowIgnored(t *testing.T) {
	// Relief date after the period end (a stale/future entry) must not shorten the
	// window — billing runs to as-of as normal. Apr 1 -> May 1 = 31 days * 8 = 248.
	gps := []map[string]string{
		reliefRow("1", "LATE, LARRY", "@100", "OPEN", "ALLIED", "4/1/2026", "off GPS", "6/1/2026"),
	}
	res := Compute(gps, nil, nil, asOf("5/1/2026"))
	if len(res.Open) != 1 || res.Open[0].Days != 31 || res.Open[0].Owed != 248 {
		t.Fatalf("out-of-window relief should be ignored: %+v", res.Open)
	}
}

func TestGpsTypeFallbackToBlueBook(t *testing.T) {
	// 48hr row has no usable type; blue book supplies SCRAM.
	gps := []map[string]string{gpsRow("1", "A B", "@100", "OPEN", "", "4/1/2026")}
	bb := []map[string]string{{"idn": "1", "gps_type": "SCRAM", "case_status": "OPEN", "warrant_case_num": "@100"}}
	res := Compute(gps, nil, bb, asOf("5/1/2026"))
	if len(res.Open) != 1 || res.Open[0].Type != "SCRAM" || res.Open[0].Behind != 465 {
		t.Fatalf("bb type fallback failed: %+v", res.Open)
	}
}

// ---- Compute: Closed pass over blue-book-only people ----

func TestClosedFromBlueBookFirstPayment(t *testing.T) {
	bb := []map[string]string{{
		"idn": "5", "defendant": "GONE, GUY", "warrant_case_num": "@500",
		"case_status": "CLOSED", "gps": "True", "gps_type": "SCRAM",
		"closed_date": "5/1/2026", "referral_date": "1/1/2026",
	}}
	pays := []map[string]string{payRow("5", "@500", "SCRAM", "50", "2/1/2026")}
	res := Compute(nil, pays, bb, asOf("5/1/2026"))
	if len(res.Closed) != 1 || len(res.Open) != 0 {
		t.Fatalf("closed=%d open=%d want 1/0", len(res.Closed), len(res.Open))
	}
	r := res.Closed[0]
	// Feb 1 -> May 1 inclusive = 90 days at $15 = $1350; paid 50 -> behind 1300.
	if r.Days != 90 || r.Owed != 1350 || r.Behind != 1300 || r.StartSrc != "First payment" || r.Case != "@500" {
		t.Fatalf("closed rec wrong: %+v", r)
	}
}

func TestClosedStartFallbackChain(t *testing.T) {
	// No daily-fee payment -> Released to Hilltop wins over Referral.
	bb := []map[string]string{{
		"idn": "6", "defendant": "PAST, PAT", "warrant_case_num": "@600",
		"case_status": "CLOSED", "gps_type": "ALLIED",
		"closed_date": "3/1/2026", "released_to_hilltop_date": "1/1/2026", "referral_date": "12/1/2025",
	}}
	res := Compute(nil, nil, bb, asOf("5/1/2026"))
	if len(res.Closed) != 1 || res.Closed[0].StartSrc != "Released to Hilltop" {
		t.Fatalf("hilltop fallback failed: %+v", res.Closed)
	}
}

func TestClosedExcludedWhenAnyOpen(t *testing.T) {
	// An IDN with any OPEN case is not a closed-only person -> skipped in pass 2.
	bb := []map[string]string{
		{"idn": "7", "defendant": "MIX, MAX", "case_status": "OPEN", "gps_type": "SCRAM", "closed_date": "5/1/2026"},
		{"idn": "7", "defendant": "MIX, MAX", "case_status": "CLOSED", "gps_type": "SCRAM", "closed_date": "5/1/2026"},
	}
	res := Compute(nil, nil, bb, asOf("5/1/2026"))
	if len(res.Closed) != 0 {
		t.Fatalf("open-bearing idn must be excluded from closed: %+v", res.Closed)
	}
}
