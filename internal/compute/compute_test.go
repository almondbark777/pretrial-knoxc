package compute

import (
	"math"
	"strconv"
	"testing"
	"time"
)

// track date used across the golden cases (matches PHASE_2 §4 and parity_ref.py).
var track = Noon(2026, 5, 30)

func mkClient(level, ref, closed string) Client {
	c := Client{Level: level}
	if d, ok := ParseDay(ref); ok {
		c.RefD, c.RefOK = d, true
	}
	if d, ok := ParseDay(closed); ok {
		c.ClosedD, c.ClosedOK = d, true
	}
	return c
}

func (c *Client) addCI(date, typ string) {
	d, ok := ParseDay(date)
	c.CheckIns = append(c.CheckIns, CheckIn{D: d, DOK: ok, Type: typ})
}
func (c *Client) addPay(date string, amt float64, typ string) {
	d, ok := ParseDay(date)
	c.Payments = append(c.Payments, Payment{D: d, DOK: ok, Amt: amt, Type: typ})
}

// ── ParseDay ────────────────────────────────────────────────────────────────

func TestParseDay(t *testing.T) {
	cases := []struct {
		in   string
		want time.Time
		ok   bool
	}{
		{"4/27/2026 13:16", Noon(2026, 4, 27), true},
		{"2026-04-27T20:00:00Z", Noon(2026, 4, 27), true},
		{"2026-4-7", Noon(2026, 4, 7), true},
		{"4/29/2026", Noon(2026, 4, 29), true},
		{"", time.Time{}, false},
		{"garbage", time.Time{}, false},
	}
	for _, c := range cases {
		got, ok := ParseDay(c.in)
		if ok != c.ok || (ok && !got.Equal(c.want)) {
			t.Errorf("ParseDay(%q) = %v,%v; want %v,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

// ── ParseLevel ────────────────────────────────────────────────────────────────

func TestParseLevel(t *testing.T) {
	for _, s := range []string{"1", "L1", "LEVEL 1", "LEVEL ONE", "I", "l1", " 1 "} {
		if n, ok := ParseLevel(s); !ok || n != 1 {
			t.Errorf("ParseLevel(%q)=%d,%v want 1", s, n, ok)
		}
	}
	for _, s := range []string{"2", "L2", "LEVEL 2", "LEVEL TWO", "II"} {
		if n, ok := ParseLevel(s); !ok || n != 2 {
			t.Errorf("ParseLevel(%q)=%d,%v want 2", s, n, ok)
		}
	}
	for _, s := range []string{"3", "L3", "LEVEL 3", "LEVEL THREE", "III"} {
		if n, ok := ParseLevel(s); !ok || n != 3 {
			t.Errorf("ParseLevel(%q)=%d,%v want 3", s, n, ok)
		}
	}
	if _, ok := ParseLevel(""); ok {
		t.Error("ParseLevel(\"\") should be unknown")
	}
}

// ── Check-ins: golden cases (PHASE_2 §4) ──────────────────────────────────────

func TestCheckIns_L1_Jones(t *testing.T) {
	c := mkClient("1", "4/29/2026 16:41", "")
	r := ComputeCheckIns(c, track)
	if len(r.Windows) != 1 || len(r.Missed) != 1 || !r.Windows[0].Missed {
		t.Fatalf("L1 JONES: windows=%d missed=%d want 1/1 (initial missed)", len(r.Windows), len(r.Missed))
	}
}

func TestCheckIns_L2_Reasonover(t *testing.T) {
	c := mkClient("2", "1/1/2026 11:58", "")
	c.addCI("4/7/2026", "In Person")
	r := ComputeCheckIns(c, track)
	if len(r.Windows) != 5 {
		t.Fatalf("L2 windows=%d want 5", len(r.Windows))
	}
	// Both in-person AND phone are required. April has only an in-person visit, so
	// it is NOT satisfied (the in-person half is met, the phone half is not).
	if r.Windows[3].Satisfied || !r.Windows[3].SatisfiedInPerson || r.Windows[3].SatisfiedPhone {
		t.Fatalf("Apr in-person-only: satisfied=%v ip=%v ph=%v want false/true/false",
			r.Windows[3].Satisfied, r.Windows[3].SatisfiedInPerson, r.Windows[3].SatisfiedPhone)
	}
	// initial, Feb, Mar, Apr missed (Apr now missed for lack of a phone); May current.
	if len(r.Missed) != 4 {
		t.Fatalf("L2 missed=%d want 4 (initial,Feb,Mar,Apr)", len(r.Missed))
	}
	if r.LastInPerson == nil || !r.LastInPerson.Equal(Noon(2026, 4, 7)) || r.LastPhone != nil {
		t.Fatalf("L2 lastInPerson=%v lastPhone=%v want 4/7 / nil", r.LastInPerson, r.LastPhone)
	}

	// Add a phone in April → both halves met → April satisfied, missed drops to 3.
	c.addCI("4/8/2026", "Phone")
	r = ComputeCheckIns(c, track)
	if !r.Windows[3].Satisfied || r.Windows[4].Missed {
		t.Fatalf("L2 Apr.satisfied=%v May.missed=%v want true/false",
			r.Windows[3].Satisfied, r.Windows[4].Missed)
	}
	if len(r.Missed) != 3 {
		t.Fatalf("L2 missed=%d want 3 (initial,Feb,Mar)", len(r.Missed))
	}
	if r.LastPhone == nil || !r.LastPhone.Equal(Noon(2026, 4, 8)) {
		t.Fatalf("L2 lastPhone=%v want 4/8", r.LastPhone)
	}
}

func TestCheckIns_L3_Hancock(t *testing.T) {
	c := mkClient("3", "4/30/2026 7:30", "")
	r := ComputeCheckIns(c, track)
	if len(r.Windows) != 5 || len(r.Missed) != 5 {
		t.Fatalf("L3 HANCOCK windows=%d missed=%d want 5/5", len(r.Windows), len(r.Missed))
	}
	// First weekly window starts Monday 5/4 (week after the initial-deadline week).
	if !r.Windows[1].Start.Equal(Noon(2026, 5, 4)) {
		t.Fatalf("L3 first week start=%v want 2026-05-04", r.Windows[1].Start)
	}
}

func TestCheckIns_Closed_Collins(t *testing.T) {
	c := mkClient("2", "4/26/2026 8:00", "4/26/2026")
	r := ComputeCheckIns(c, track)
	if len(r.Windows) != 1 || len(r.Missed) != 0 {
		t.Fatalf("closed COLLINS windows=%d missed=%d want 1/0 (stops at closed date)",
			len(r.Windows), len(r.Missed))
	}
}

// L1 has only the initial 3-day window. When it is MISSED, nextDue must point at
// the initial window (Type=="initial", Deadline==refDate+3) — not nil — so the
// "next due" column still flags the L1 clients who most need a visit. When the
// initial is MADE, nextDue is nil. Mirrors tools/parity_ref.py compute_check_ins.
func TestCheckIns_L1_NextDueOnMissedInitial(t *testing.T) {
	// Missed: referral 1/1, no check-ins, effEnd (track 5/30) past the 3-day deadline.
	c := mkClient("1", "1/1/2026", "")
	r := ComputeCheckIns(c, track)
	if r.NextDue == nil {
		t.Fatalf("L1 missed: NextDue=nil want initial window")
	}
	if r.NextDue.Type != "initial" {
		t.Fatalf("L1 missed: NextDue.Type=%q want \"initial\"", r.NextDue.Type)
	}
	if want := addDays(c.RefD, 3); !r.NextDue.Deadline.Equal(want) {
		t.Fatalf("L1 missed: NextDue.Deadline=%v want %v (refDate+3)", r.NextDue.Deadline, want)
	}

	// Made: both an in-person AND a phone contact inside the initial window → nextDue nil.
	c2 := mkClient("1", "1/1/2026", "")
	c2.addCI("1/2/2026", "In Person")
	c2.addCI("1/2/2026", "Phone")
	r2 := ComputeCheckIns(c2, track)
	if r2.NextDue != nil {
		t.Fatalf("L1 made: NextDue=%v want nil", r2.NextDue)
	}
}

// ── PTR fees: golden cases ────────────────────────────────────────────────────

func TestPTR_L1Flat(t *testing.T) {
	c := mkClient("1", "4/29/2026 16:41", "")
	r := ComputePTRFees(c, track, "")
	if !r.Applies || r.TotalOwed != 20 || len(r.MonthsOwed) != 1 ||
		r.MonthsOwed[0].Label != "One-time L1 fee" || r.Balance != -20 {
		t.Fatalf("L1 PTR = %+v want flat $20", r)
	}
}

func TestPTR_L2_Reasonover(t *testing.T) {
	c := mkClient("2", "1/1/2026 11:58", "")
	r := ComputePTRFees(c, track, "")
	if r.TotalOwed != 100 || len(r.MonthsOwed) != 5 {
		t.Fatalf("L2 PTR owed=%d months=%d want 100/5 (Jan..May)", r.TotalOwed, len(r.MonthsOwed))
	}
}

func TestPTR_L3_Hancock(t *testing.T) {
	c := mkClient("3", "4/30/2026 7:30", "")
	r := ComputePTRFees(c, track, "")
	if r.TotalOwed != 40 || len(r.MonthsOwed) != 2 {
		t.Fatalf("L3 PTR owed=%d months=%d want 40/2 (Apr,May)", r.TotalOwed, len(r.MonthsOwed))
	}
}

func TestPTR_Closed_Collins(t *testing.T) {
	c := mkClient("2", "4/26/2026 8:00", "4/26/2026")
	r := ComputePTRFees(c, track, "")
	if r.TotalOwed != 20 || len(r.MonthsOwed) != 1 {
		t.Fatalf("closed PTR owed=%d months=%d want 20/1", r.TotalOwed, len(r.MonthsOwed))
	}
}

func TestPTR_PaymentFilter(t *testing.T) {
	c := mkClient("2", "1/1/2026", "")
	c.addPay("1/5/2026", 20, "Drug Screen") // must NOT count
	c.addPay("1/6/2026", 20, "GPS")         // must NOT count
	c.addPay("1/7/2026", 20, "Ptr")         // counts
	c.addPay("1/8/2026", 20, "PTR Fee")     // counts
	r := ComputePTRFees(c, track, "")
	if r.TotalPaid != 40 {
		t.Fatalf("PTR totalPaid=%v want 40 (only Ptr + PTR Fee)", r.TotalPaid)
	}
}

func TestPTR_CaseFilter(t *testing.T) {
	// A multi-case defendant: PTR payments tagged to two different cases.
	c := mkClient("2", "1/1/2026", "")
	c.Payments = []Payment{
		{Amt: 20, Type: "PTR", Case: "@111", DOK: false},
		{Amt: 20, Type: "PTR", Case: "@222", DOK: false},
		{Amt: 20, Type: "PTR", Case: "@111, @222", DOK: false}, // grouped: matches either
	}
	if r := ComputePTRFees(c, track, ""); r.TotalPaid != 60 {
		t.Fatalf("no filter: totalPaid=%v want 60", r.TotalPaid)
	}
	if r := ComputePTRFees(c, track, "@111"); r.TotalPaid != 40 {
		t.Fatalf("filter @111: totalPaid=%v want 40 (own + grouped)", r.TotalPaid)
	}
	if r := ComputePTRFees(c, track, "@222"); r.TotalPaid != 40 {
		t.Fatalf("filter @222: totalPaid=%v want 40 (own + grouped)", r.TotalPaid)
	}
	if r := ComputePTRFees(c, track, "@999"); r.TotalPaid != 0 {
		t.Fatalf("filter @999: totalPaid=%v want 0 (no match)", r.TotalPaid)
	}
}

func TestGPS_CaseFilter(t *testing.T) {
	// GPS paid side narrows by case; owed/days (single install) unchanged.
	c := gpsClient("SCRAM", "1/1/2026")
	c.Payments = []Payment{
		{Amt: 300, Type: "GPS", Case: "@111"},
		{Amt: 200, Type: "SCRAM", Case: "@222"},
	}
	all := ComputeGPS(c, Noon(2026, 1, 31), nil, "")
	one := ComputeGPS(c, Noon(2026, 1, 31), nil, "@111")
	if all.TotalGpsPaid != 500 || one.TotalGpsPaid != 300 {
		t.Fatalf("gps paid all=%v one=%v want 500/300", all.TotalGpsPaid, one.TotalGpsPaid)
	}
	if *all.TotalOwedDollars != *one.TotalOwedDollars {
		t.Fatalf("owed should not change with case filter: all=%v one=%v",
			*all.TotalOwedDollars, *one.TotalOwedDollars)
	}
}

// ── GPS: golden cases ─────────────────────────────────────────────────────────

func gpsClient(typ, install string) Client {
	c := mkClient("3", "4/17/2026", "")
	c.GpsActive = true
	c.GpsType = typ
	c.GpInstall = install
	return c
}

func TestGPS_SCRAM_Aguilar(t *testing.T) {
	c := gpsClient("SCRAM", "4/20/2026")
	g := ComputeGPS(c, track, nil, "")
	if g.Vendor != "SCRAM" || *g.DailyRate != 15 || *g.DaysActive != 41 ||
		*g.TotalOwedDollars != 615 || *g.SurplusDollars != -615 || *g.SurplusDays != -41 {
		t.Fatalf("SCRAM AGUILAR = %+v want rate15 days41 owed615 surplus-615/-41", dumpGPS(g))
	}
}

func TestGPS_ALLIED_Piety(t *testing.T) {
	c := gpsClient("ALLIED", "4/28/2026")
	g := ComputeGPS(c, track, nil, "")
	if g.Vendor != "ALLIED" || *g.DailyRate != 8 || *g.DaysActive != 33 ||
		*g.TotalOwedDollars != 264 || *g.SurplusDays != -33 {
		t.Fatalf("ALLIED PIETY = %v want rate8 days33 owed264 surplus-33", dumpGPS(g))
	}
}

func TestGPS_Switch(t *testing.T) {
	c := gpsClient("SCRAM", "1/1/2026")
	c.GpSwitchedTo = "ALLIED"
	c.GpSwitchedDate = "1/15/2026"
	g := ComputeGPS(c, Noon(2026, 1, 31), nil, "")
	// 14*15 + 23 + 16*8 = 210 + 23 + 128 = 361
	if !g.HasSwitch || *g.TotalOwedDollars != 361 {
		t.Fatalf("switch owed=%v hasSwitch=%v want 361/true", g.TotalOwedDollars, g.HasSwitch)
	}
}

func TestGPS_Relief(t *testing.T) {
	c := gpsClient("SCRAM", "1/1/2026")
	c.GpSwitchedTo = "no gps"
	c.GpSwitchedDate = "1/10/2026"
	g := ComputeGPS(c, Noon(2026, 1, 31), nil, "")
	// window capped at 1/10 => 10 days * 15 = 150
	if !g.ReliefSwitch || *g.DaysActive != 10 || *g.TotalOwedDollars != 150 {
		t.Fatalf("relief days=%v owed=%v relief=%v want 10/150/true",
			g.DaysActive, g.TotalOwedDollars, g.ReliefSwitch)
	}
}

func TestGPS_IC_Zero(t *testing.T) {
	c := gpsClient("IC", "1/1/2026")
	g := ComputeGPS(c, Noon(2026, 1, 31), nil, "")
	if g.Vendor != "IC" || *g.DailyRate != 0 || *g.TotalOwedDollars != 0 {
		t.Fatalf("IC = %v want rate0 owed0", dumpGPS(g))
	}
}

func TestGPS_SurplusPaidAhead(t *testing.T) {
	c := gpsClient("SCRAM", "1/1/2026")
	c.DayAdj = 2
	c.addPay("1/5/2026", 500, "GPS")
	g := ComputeGPS(c, Noon(2026, 1, 31), nil, "")
	// owed 31*15=465; paid 500 + adj 2*15=30 => surplus 65 => ceil(65/15)=5
	if *g.SurplusDollars != 65 || *g.SurplusDays != 5 {
		t.Fatalf("surplus$=%v days=%v want 65/5", g.SurplusDollars, g.SurplusDays)
	}
}

// daysCovered = paid/rate + adj; surplus (real days) = surplusDollars/rate.
// Mirrors the offline tracker's "Days Covered" stat and the ceil(surplus) hero.
func TestGPS_DaysCoveredAndSurplus(t *testing.T) {
	c := gpsClient("SCRAM", "1/1/2026")
	c.DayAdj = 2
	c.addPay("1/5/2026", 500, "GPS")
	g := ComputeGPS(c, Noon(2026, 1, 31), nil, "")
	// rate 15; daysCovered = 500/15 + 2 = 35.3333…
	if g.DaysCovered == nil || math.Abs(*g.DaysCovered-(500.0/15.0+2)) > 1e-9 {
		t.Fatalf("daysCovered=%v want %v", g.DaysCovered, 500.0/15.0+2)
	}
	// surplusDollars 65; surplus = 65/15 = 4.3333…; surplusDays = ceil = 5
	if g.Surplus == nil || math.Abs(*g.Surplus-(65.0/15.0)) > 1e-9 {
		t.Fatalf("surplus=%v want %v", g.Surplus, 65.0/15.0)
	}
	if g.SurplusDays == nil || *g.SurplusDays != 5 {
		t.Fatalf("surplusDays=%v want 5", g.SurplusDays)
	}
}

// daysCovered clamps the paid-side to >= 0 before adding the adjustment (the JS
// Math.max(0, paid/rate)); with zero payments it's just the adjustment.
func TestGPS_DaysCoveredFloorsPaidSide(t *testing.T) {
	c := gpsClient("ALLIED", "1/1/2026") // rate 8, no payments
	g := ComputeGPS(c, Noon(2026, 1, 31), nil, "")
	if g.DaysCovered == nil || *g.DaysCovered != 0 {
		t.Fatalf("daysCovered=%v want 0 (no payments, no adj)", g.DaysCovered)
	}
}

// JSToFixed must match JavaScript Number.toFixed digit-for-digit (the offline
// tracker renders Days Covered with .toFixed(1)). Expected values were read off
// the actual JS engine. Note the half-away-from-zero rounding (381.25 -> 381.3,
// not the 381.2 that Go's FormatFloat round-to-even would give) and the "-0.0".
func TestJSToFixed(t *testing.T) {
	cases := []struct {
		x    float64
		prec int
		want string
	}{
		{3050.0 / 8.0, 1, "381.3"},
		{381.25, 1, "381.3"},
		{0.05, 1, "0.1"},
		{2.5, 0, "3"},
		{1.15, 1, "1.1"},
		{0, 1, "0.0"},
		{-1.25, 1, "-1.3"},
		{381.2, 1, "381.2"},
		{7140.0 / 15.0, 1, "476.0"},
		{-0.04, 1, "-0.0"},
	}
	for _, c := range cases {
		if got := JSToFixed(c.x, c.prec); got != c.want {
			t.Errorf("JSToFixed(%v,%d)=%q want %q", c.x, c.prec, got, c.want)
		}
	}
}

func TestGPS_InCustodyUnknownVendor(t *testing.T) {
	// Real data uses "IN CUSTODY"; it must NOT match the IC vendor (no "IC"
	// substring) and yields an uncomputable (null) rate => MISSING in the UI.
	c := gpsClient("IN CUSTODY", "1/1/2026")
	g := ComputeGPS(c, Noon(2026, 1, 31), nil, "")
	if g.Vendor != "" || g.DailyRate != nil || g.TotalOwedDollars != nil {
		t.Fatalf("IN CUSTODY = %v want vendor='' rate=nil owed=nil", dumpGPS(g))
	}
}

func dumpGPS(g GPSResult) string {
	i := func(p *int) string {
		if p == nil {
			return "nil"
		}
		return strconv.Itoa(*p)
	}
	f := func(p *float64) string {
		if p == nil {
			return "nil"
		}
		return strconv.FormatFloat(*p, 'f', -1, 64)
	}
	return "vendor=" + g.Vendor + " rate=" + i(g.DailyRate) + " days=" + i(g.DaysActive) +
		" owed=" + f(g.TotalOwedDollars) + " surplus$=" + f(g.SurplusDollars) +
		" surplusDays=" + i(g.SurplusDays)
}

// TestCheckInKind is the direct unit-test for the CheckInKind classifier (item
// #30). The vocabulary is cross-checked against tools/parity_ref.py lines 136-144:
// in-person = "office", "walkin", "in_person" variants; phone = "phone", "text",
// "call", "virtual", "video", "tele" (incl. punctuated / prefixed forms);
// neither = blank, "Court", "Note", unrecognised.
func TestCheckInKind(t *testing.T) {
	cases := []struct {
		typ    string
		wantIP bool
		wantPh bool
	}{
		// ── in-person variants ──────────────────────────────────────────────
		{"In Person", true, false},    // imported SharePoint value
		{"In-person", true, false},    // app dropdown
		{"in person", true, false},    // lowercase
		{"IN PERSON", true, false},    // caps
		{"office", true, false},       // keyword: office
		{"Office Visit", true, false}, // prefixed
		{"walkin", true, false},       // keyword: walkin
		{"Walk-in", true, false},      // hyphenated
		{"Walk In", true, false},      // two words

		// ── phone / remote variants ─────────────────────────────────────────
		{"Phone", false, true}, // imported
		{"phone", false, true},
		{"Phone Call", false, true},
		{"Text", false, true},
		{"Text Message", false, true},
		{"Call", false, true},
		{"Virtual", false, true},
		{"virtual check-in", false, true},
		{"Video", false, true},
		{"Video Call", false, true},
		{"Tele", false, true},
		{"Teleconference", false, true},

		// ── neither ─────────────────────────────────────────────────────────
		{"", false, false},
		{"Court", false, false},
		{"Court Date", false, false},
		{"Note", false, false},
		{"xyz", false, false},
		{"123", false, false},
	}

	for _, c := range cases {
		ip, ph := CheckInKind(c.typ)
		if ip != c.wantIP || ph != c.wantPh {
			t.Errorf("CheckInKind(%q) = inPerson=%v phone=%v; want inPerson=%v phone=%v",
				c.typ, ip, ph, c.wantIP, c.wantPh)
		}
	}
}

// ── Relief-switch boundary tests for compute.ComputeGPS (item #31) ──────────
// The engines are already correct; these are regression guards for the
// equal-date edge cases. Run with: go test -run Relief ./internal/compute/...

// TestReliefGPSSwitchedDateEqualInstall: when the GPS switched (relief) date
// equals the install date, billing freezes there: DaysActive = 1, owed = 1 * rate.
// compute.go: ge(rd, start) && lt(rd, end) — equal to start satisfies ge.
func TestReliefGPSSwitchedDateEqualInstall(t *testing.T) {
	c := gpsClient("SCRAM", "1/1/2026")
	c.GpSwitchedTo = "no gps"
	c.GpSwitchedDate = "1/1/2026" // == install
	g := ComputeGPS(c, Noon(2026, 1, 31), nil, "")
	if !g.ReliefSwitch {
		t.Fatalf("relief==install: ReliefSwitch=false want true")
	}
	if g.DaysActive == nil || *g.DaysActive != 1 {
		t.Fatalf("relief==install: DaysActive=%v want 1", g.DaysActive)
	}
	if g.TotalOwedDollars == nil || *g.TotalOwedDollars != 15 {
		t.Fatalf("relief==install: owed=%v want 15 (1 day * $15)", g.TotalOwedDollars)
	}
}

// TestReliefGPSSwitchedDateEqualTrack: when the GPS switched (relief) date
// equals the track date (end), the condition lt(rd, end) is false so the freeze
// does NOT apply and billing runs to track normally.
// SCRAM, 1/1/2026 install, track 1/31/2026 = 31 days * $15 = $465.
func TestReliefGPSSwitchedDateEqualTrack(t *testing.T) {
	c := gpsClient("SCRAM", "1/1/2026")
	c.GpSwitchedTo = "no gps"
	c.GpSwitchedDate = "1/31/2026" // == track (end)
	g := ComputeGPS(c, Noon(2026, 1, 31), nil, "")
	// relief date == track: freeze excluded; billing = 31 days * $15 = $465
	if g.DaysActive == nil || *g.DaysActive != 31 {
		t.Fatalf("relief==track: DaysActive=%v want 31 (freeze excluded)", g.DaysActive)
	}
	if g.TotalOwedDollars == nil || *g.TotalOwedDollars != 465 {
		t.Fatalf("relief==track: owed=%v want 465", g.TotalOwedDollars)
	}
}

func TestParseDateTime(t *testing.T) {
	// Preserves the clock time from the real referral_date format.
	if got, ok := ParseDateTime("4/30/2026 7:30"); !ok || got.Hour() != 7 || got.Minute() != 30 ||
		got.Year() != 2026 || got.Month() != time.April || got.Day() != 30 {
		t.Fatalf("US datetime: got %v ok=%v", got, ok)
	}
	// 24-hour times stay 24-hour (16:41 → 4:41 PM on screen).
	if got, ok := ParseDateTime("4/29/2026 16:41"); !ok || got.Hour() != 16 || got.Minute() != 41 {
		t.Fatalf("24h time: got %v ok=%v", got, ok)
	}
	// Same-day ordering: a later time sorts After an earlier one (feed = newest first).
	a, _ := ParseDateTime("4/30/2026 7:30")
	b, _ := ParseDateTime("4/30/2026 4:36")
	if !a.After(b) {
		t.Fatalf("expected 7:30 to sort after 4:36")
	}
	// ISO with time.
	if got, ok := ParseDateTime("2026-04-30T09:15:00"); !ok || got.Hour() != 9 || got.Minute() != 15 {
		t.Fatalf("ISO datetime: got %v ok=%v", got, ok)
	}
	// Date-only falls back to noon (still ok, so it's always safe to sort).
	if got, ok := ParseDateTime("4/30/2026"); !ok || got.Hour() != 12 {
		t.Fatalf("date-only should fall back to noon: got %v ok=%v", got, ok)
	}
	// Blank → not ok.
	if _, ok := ParseDateTime("   "); ok {
		t.Fatalf("blank should be !ok")
	}
}
