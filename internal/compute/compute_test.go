package compute

import (
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
	if len(r.Missed) != 3 {
		t.Fatalf("L2 missed=%d want 3 (initial,Feb,Mar)", len(r.Missed))
	}
	if r.LastCheckIn == nil || !r.LastCheckIn.Equal(Noon(2026, 4, 7)) {
		t.Fatalf("L2 lastCheckIn=%v want 2026-04-07", r.LastCheckIn)
	}
	// April satisfied, May not missed (current month).
	if !r.Windows[3].Satisfied || r.Windows[4].Missed {
		t.Fatalf("L2 Apr.satisfied=%v May.missed=%v want true/false",
			r.Windows[3].Satisfied, r.Windows[4].Missed)
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
