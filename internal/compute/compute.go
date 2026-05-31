// Package compute is a faithful Go port of the canonical PTR Client Lookup
// v0.82 JS data layer (assets/8a6913e5-*.js): parsePretrialLevel, _parseDay,
// computeCheckIns, computePTRFees, computeGPS, and the GPS vendor/relief helpers.
//
// Per the Brief Part 0.2 (LOCKED), the Go rewrite re-implements this business
// math SERVER-SIDE and retires the embedded HTML tool. Correctness here is the
// whole point of the rewrite, so this file mirrors the JS line-for-line and is
// validated by compute_test.go against the PHASE_2 §4 golden values.
//
// All dates are normalized to NOON UTC, exactly like the JS
// `Date.UTC(y, m-1, d, 12, 0, 0)`, so day arithmetic is exact and DST-immune.
//
// This package depends only on the standard library so its parity tests run
// without fetching any module.
package compute

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ── Date helpers (mirror the JS noon-UTC convention) ───────────────────────

// Noon returns y/m/d at 12:00:00 UTC.
func Noon(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 12, 0, 0, 0, time.UTC)
}

var (
	reISO = regexp.MustCompile(`^(\d{4})-(\d{1,2})-(\d{1,2})`)
	reUS  = regexp.MustCompile(`^(\d{1,2})/(\d{1,2})/(\d{4})`)
	// Fallback layouts (the regexes above catch every real format in the data;
	// these only matter for oddballs, matching the JS `new Date(t)` fallback).
	fallbackLayouts = []string{
		"2006-01-02T15:04:05", "2006-01-02 15:04:05",
		"01/02/2006 15:04", "01/02/2006", "2006-01-02",
	}
)

// ParseDay ports _parseDayImpl. Returns (noon-UTC time, ok).
func ParseDay(s string) (time.Time, bool) {
	t := strings.TrimSpace(s)
	if t == "" {
		return time.Time{}, false
	}
	if m := reISO.FindStringSubmatch(t); m != nil {
		y, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		d, _ := strconv.Atoi(m[3])
		if y > 0 && mo >= 1 && mo <= 12 && d >= 1 && d <= 31 {
			return Noon(y, time.Month(mo), d), true
		}
	}
	if m := reUS.FindStringSubmatch(t); m != nil {
		mo, _ := strconv.Atoi(m[1])
		d, _ := strconv.Atoi(m[2])
		y, _ := strconv.Atoi(m[3])
		if y > 0 && mo >= 1 && mo <= 12 && d >= 1 && d <= 31 {
			return Noon(y, time.Month(mo), d), true
		}
	}
	for _, layout := range fallbackLayouts {
		if dt, err := time.Parse(layout, t); err == nil {
			return Noon(dt.Year(), dt.Month(), dt.Day()), true
		}
	}
	return time.Time{}, false
}

func addDays(d time.Time, n int) time.Time { return d.AddDate(0, 0, n) }

// mondayOfWeek mirrors JS _mondayOfWeek (getUTCDay: 0=Sun..6=Sat).
func mondayOfWeek(d time.Time) time.Time {
	wd := int(d.Weekday()) // Go: Sunday=0..Saturday=6 (same as JS getUTCDay)
	back := wd - 1
	if wd == 0 {
		back = 6
	}
	return addDays(d, -back)
}

func firstOfMonth(d time.Time) time.Time { return Noon(d.Year(), d.Month(), 1) }

func lastOfMonth(d time.Time) time.Time {
	// day 0 of next month == last day of this month
	return time.Date(d.Year(), d.Month()+1, 0, 12, 0, 0, 0, time.UTC)
}

func nextMonth(d time.Time) time.Time {
	return time.Date(d.Year(), d.Month()+1, 1, 12, 0, 0, 0, time.UTC)
}

// daysBetween mirrors JS Math.round((b-a)/86400000). Noon-UTC => exact.
func daysBetween(a, b time.Time) int {
	return int(math.Round(b.Sub(a).Hours() / 24.0))
}

// Date comparison helpers (all inputs are noon-UTC, so equality is exact).
func le(a, b time.Time) bool { return !a.After(b) }
func ge(a, b time.Time) bool { return !a.Before(b) }
func lt(a, b time.Time) bool { return a.Before(b) }
func gt(a, b time.Time) bool { return a.After(b) }

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// ── Pretrial level (port of parsePretrialLevel) ────────────────────────────

var (
	reLvl1  = regexp.MustCompile(`^(L?1|LEVEL\s*1|LEVEL\s*ONE|I)$`)
	reLvl2  = regexp.MustCompile(`^(L?2|LEVEL\s*2|LEVEL\s*TWO|II)$`)
	reLvl3  = regexp.MustCompile(`^(L?3|LEVEL\s*3|LEVEL\s*THREE|III)$`)
	reDigit = regexp.MustCompile(`(\d)`)
)

// ParseLevel returns (level, known). known=false models the JS `null`.
// Callers compare the int directly; 0 (unknown) behaves like the JS null does
// in the branch logic (falls into the weekly/else branch; PTR "applies=false"
// unless GPS-active).
func ParseLevel(raw string) (int, bool) {
	s := strings.ToUpper(strings.TrimSpace(raw))
	if s == "" {
		return 0, false
	}
	switch {
	case reLvl1.MatchString(s):
		return 1, true
	case reLvl2.MatchString(s):
		return 2, true
	case reLvl3.MatchString(s):
		return 3, true
	}
	if m := reDigit.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n, true
	}
	return 0, false
}

// ── Client / result models ─────────────────────────────────────────────────

// CheckIn is one check-in event with its pre-parsed date.
type CheckIn struct {
	D    time.Time
	DOK  bool
	Type string
}

// Payment is one payment event with its pre-parsed date and amount.
type Payment struct {
	D    time.Time
	DOK  bool
	Amt  float64
	Type string
	Case string // case number(s) on this payment row, for per-case narrowing
}

// Client mirrors the joined client object from buildClients (the fields the
// three compute functions read). Dates are pre-parsed (RefD/ClosedD) exactly as
// buildClients pre-parses c._refD / c._closedD.
type Client struct {
	IDN            string
	Name           string
	Level          string
	Status         string
	Officer        string
	CaseNo         string
	GpsActive      bool
	GpsType        string
	DayAdj         float64
	GpInstall      string
	GpSwitchedTo   string
	GpSwitchedDate string
	GpNotes        string

	// Imported case-info fields — display only, not read by the math. Shown on
	// the profile's Case Info panel (blank non-critical fields are hidden).
	ChargeType      string
	BondAmount      string
	SupervisionType string
	OrderFrom       string
	DMA             string
	Birthdate       string

	RefD     time.Time
	RefOK    bool
	ClosedD  time.Time
	ClosedOK bool

	CheckIns []CheckIn
	Payments []Payment

	// Overrides records which imported fields were corrected by a supervisor
	// app-override (raw-column key -> new value), so the UI can flag them
	// "override (app)". Populated by BuildClients after splicing the override
	// into the raw row, so all downstream compute already sees the fixed value.
	Overrides map[string]string
}

// Window is one required check-in window.
type Window struct {
	Type      string    `json:"type"` // initial | month | week
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
	Deadline  time.Time `json:"deadline"`
	Satisfied bool      `json:"satisfied"`
	Missed    bool      `json:"missed"`
	Label     string    `json:"label"`
}

// CheckInResult mirrors computeCheckIns' return shape.
type CheckInResult struct {
	Level       int        `json:"level"` // 0 == unknown/null
	RefDate     *time.Time `json:"refDate"`
	Today       time.Time  `json:"today"` // effective end
	Windows     []Window   `json:"windows"`
	Missed      []Window   `json:"missed"`
	LastCheckIn *time.Time `json:"lastCheckIn"`
	NextDue     *Window    `json:"nextDue"`
	Error       string     `json:"error,omitempty"`
}

// MonthOwed is one $20 PTR-fee month.
type MonthOwed struct {
	Label  string `json:"label"`
	Amount int    `json:"amount"`
}

// PTRResult mirrors computePTRFees' return shape.
type PTRResult struct {
	Level      int         `json:"level"`
	MonthsOwed []MonthOwed `json:"monthsOwed"`
	TotalOwed  int         `json:"totalOwed"`
	TotalPaid  float64     `json:"totalPaid"`
	Balance    float64     `json:"balance"`
	Applies    bool        `json:"applies"`
}

// GPSResult mirrors computeGPS' return shape (nil pointers == JS null).
type GPSResult struct {
	Vendor           string   `json:"vendor"`
	DailyRate        *int     `json:"dailyRate"`
	Vendor2          string   `json:"vendor2"`
	DailyRate2       *int     `json:"dailyRate2"`
	HasSwitch        bool     `json:"hasSwitch"`
	ReliefSwitch     bool     `json:"reliefSwitch"`
	TotalOwedDollars *float64 `json:"totalOwedDollars"`
	TotalGpsPaid     float64  `json:"totalGpsPaid"`
	DaysActive       *int     `json:"daysActive"`
	Adj              float64  `json:"adj"`
	AdjDollars       float64  `json:"adjDollars"`
	SurplusDollars   *float64 `json:"surplusDollars"`
	SurplusDays      *int     `json:"surplusDays"`
	Covered          *bool    `json:"covered"`
}

// ── computeCheckIns ─────────────────────────────────────────────────────────

func ComputeCheckIns(c Client, track time.Time) CheckInResult {
	level, _ := ParseLevel(c.Level)
	if !c.RefOK {
		return CheckInResult{Level: level, RefDate: nil, Today: track, Windows: []Window{},
			Missed: []Window{}, Error: "No referral date"}
	}
	ref := c.RefD
	effEnd := track
	if c.ClosedOK && lt(c.ClosedD, track) {
		effEnd = c.ClosedD
	}

	// All valid check-in dates, ascending.
	var allCi []time.Time
	for _, ci := range c.CheckIns {
		if ci.DOK {
			allCi = append(allCi, ci.D)
		}
	}
	sortTimes(allCi)
	var lastCheckIn *time.Time
	if len(allCi) > 0 {
		lc := allCi[len(allCi)-1]
		lastCheckIn = &lc
	}

	initialDeadline := addDays(ref, 3)
	initialMade := anyInRange(allCi, ref, initialDeadline)
	initialMissed := !initialMade && gt(effEnd, initialDeadline)

	windows := []Window{{
		Type: "initial", Start: ref, End: initialDeadline, Deadline: initialDeadline,
		Satisfied: initialMade, Missed: initialMissed, Label: "Initial (3-day)",
	}}

	refCopy := ref
	if level == 1 {
		return CheckInResult{Level: level, RefDate: &refCopy, Today: effEnd, Windows: windows,
			Missed: missedOf(windows), LastCheckIn: lastCheckIn, NextDue: nextDue(windows, effEnd)}
	}

	if level == 2 {
		cur := nextMonth(firstOfMonth(initialDeadline))
		for le(cur, effEnd) {
			monthEnd := lastOfMonth(cur)
			windowEnd := minTime(monthEnd, effEnd)
			hit := anyInRange(allCi, cur, windowEnd)
			monthClosed := ge(effEnd, monthEnd) || (c.ClosedOK && le(c.ClosedD, monthEnd))
			isFuture := gt(cur, effEnd)
			windows = append(windows, Window{
				Type: "month", Start: cur, End: monthEnd, Deadline: monthEnd,
				Satisfied: hit, Missed: !hit && monthClosed && !isFuture,
				Label: cur.Format("January 2006"),
			})
			cur = nextMonth(cur)
		}
		return CheckInResult{Level: level, RefDate: &refCopy, Today: effEnd, Windows: windows,
			Missed: missedOf(windows), LastCheckIn: lastCheckIn, NextDue: nextDue(windows, effEnd)}
	}

	// Level 3 (or GPS-as-L3, or unknown level — exactly like the JS else branch).
	weekMon := addDays(mondayOfWeek(initialDeadline), 7)
	guard := 0
	for le(weekMon, effEnd) && guard < 400 {
		guard++
		weekFri := addDays(weekMon, 4)
		windowEnd := minTime(weekFri, effEnd)
		hit := anyInRange(allCi, weekMon, windowEnd)
		weekClosed := ge(effEnd, weekFri)
		isFuture := gt(weekMon, effEnd)
		windows = append(windows, Window{
			Type: "week", Start: weekMon, End: weekFri, Deadline: weekFri,
			Satisfied: hit, Missed: !hit && weekClosed && !isFuture,
			Label: "Week of " + weekMon.Format("Jan 02"),
		})
		weekMon = addDays(weekMon, 7)
	}
	outLevel := level
	if outLevel == 0 && c.GpsActive {
		outLevel = 3
	}
	return CheckInResult{Level: outLevel, RefDate: &refCopy, Today: effEnd, Windows: windows,
		Missed: missedOf(windows), LastCheckIn: lastCheckIn, NextDue: nextDue(windows, effEnd)}
}

func anyInRange(dates []time.Time, start, end time.Time) bool {
	for _, d := range dates {
		if ge(d, start) && le(d, end) {
			return true
		}
	}
	return false
}

func missedOf(ws []Window) []Window {
	out := []Window{}
	for _, w := range ws {
		if w.Missed {
			out = append(out, w)
		}
	}
	return out
}

func nextDue(ws []Window, effEnd time.Time) *Window {
	for i := range ws {
		if !ws[i].Satisfied && !ws[i].Missed && le(ws[i].Start, effEnd) {
			return &ws[i]
		}
	}
	for i := range ws {
		if !ws[i].Satisfied && gt(ws[i].Start, effEnd) {
			return &ws[i]
		}
	}
	return nil
}

func sortTimes(ts []time.Time) {
	// small slices; insertion sort keeps it dependency-free and stable
	for i := 1; i < len(ts); i++ {
		for j := i; j > 0 && ts[j].Before(ts[j-1]); j-- {
			ts[j], ts[j-1] = ts[j-1], ts[j]
		}
	}
}

// ── case-token matching (port of the canonical _matchCase, split on /[,\s]+/) ──

var reCaseSplit = regexp.MustCompile(`[,\s]+`)

// caseTokens lowercases, trims, and splits a case string on commas/whitespace,
// dropping empties — exactly like the JS `split(/[,\s]+/).map(trim).filter(Boolean)`.
func caseTokens(s string) []string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return nil
	}
	var out []string
	for _, t := range reCaseSplit.Split(s, -1) {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// matchCase reports whether a payment row's case value intersects the filter
// tokens. An empty filter means "no narrowing" (always true), mirroring the
// canonical _matchCase. A non-empty filter against an empty row case is false.
func matchCase(rowCase string, filter []string) bool {
	if len(filter) == 0 {
		return true
	}
	rowToks := caseTokens(rowCase)
	if len(rowToks) == 0 {
		return false
	}
	for _, rt := range rowToks {
		for _, ft := range filter {
			if rt == ft {
				return true
			}
		}
	}
	return false
}

// CaseTokens exposes the canonical /[,\s]+/ case tokenizer (lowercased, trimmed,
// empties dropped) for callers that need the distinct cases on a row — e.g. the
// profile's case-selector dropdown.
func CaseTokens(s string) []string { return caseTokens(s) }

// CaseMatches reports whether a case `value` shares any token with `filter`
// (both tokenized on /[,\s]+/). An empty filter returns false — callers use that
// to mean "no specific case selected". Exported for the handlers' case picker.
func CaseMatches(value, filter string) bool {
	ft := caseTokens(filter)
	if len(ft) == 0 {
		return false
	}
	return matchCase(value, ft)
}

// ── computePTRFees ──────────────────────────────────────────────────────────

var rePTR = regexp.MustCompile(`(?i)\bptr\b`)

// ComputePTRFees ports computePTRFees(c, todayStr, caseFilter). caseFilter ""
// narrows nothing (whole-client view); a case token narrows the PTR payments to
// that case, exactly as the per-case dropdown does in the canonical tool.
func ComputePTRFees(c Client, track time.Time, caseFilter string) PTRResult {
	level, _ := ParseLevel(c.Level)
	caseToks := caseTokens(caseFilter)

	var totalPaid float64
	for _, p := range c.Payments {
		if !matchCase(p.Case, caseToks) {
			continue
		}
		if rePTR.MatchString(strings.ToLower(strings.TrimSpace(p.Type))) {
			totalPaid += p.Amt
		}
	}

	if !c.RefOK {
		return PTRResult{Level: level, MonthsOwed: []MonthOwed{}, TotalOwed: 0,
			TotalPaid: totalPaid, Balance: totalPaid, Applies: false}
	}
	effEnd := track
	if c.ClosedOK && lt(c.ClosedD, track) {
		effEnd = c.ClosedD
	}

	if level == 1 {
		return PTRResult{Level: level,
			MonthsOwed: []MonthOwed{{Label: "One-time L1 fee", Amount: 20}},
			TotalOwed:  20, TotalPaid: totalPaid, Balance: totalPaid - 20, Applies: true}
	}
	if level != 2 && level != 3 && !c.GpsActive {
		return PTRResult{Level: level, MonthsOwed: []MonthOwed{}, TotalOwed: 0,
			TotalPaid: totalPaid, Balance: totalPaid, Applies: false}
	}

	months := []MonthOwed{}
	cur := firstOfMonth(c.RefD)
	endCur := firstOfMonth(effEnd)
	guard := 0
	for le(cur, endCur) && guard < 600 {
		guard++
		months = append(months, MonthOwed{Label: cur.Format("Jan 2006"), Amount: 20})
		cur = nextMonth(cur)
	}
	totalOwed := len(months) * 20
	return PTRResult{Level: level, MonthsOwed: months, TotalOwed: totalOwed,
		TotalPaid: totalPaid, Balance: totalPaid - float64(totalOwed), Applies: true}
}

// ── computeGPS ──────────────────────────────────────────────────────────────

var reRelief = regexp.MustCompile(`\bno\s*gps\b|\bgps\s*reliev|\boff\s*gps\b|\bgps\s*off\b|\bremov`)

func vendorOf(t string) string {
	u := strings.ToUpper(t)
	switch {
	case strings.Contains(u, "SCRAM"):
		return "SCRAM"
	case strings.Contains(u, "ALLIED"):
		return "ALLIED"
	case strings.Contains(u, "IC"):
		return "IC"
	}
	return ""
}

func rateOf(v string) *int {
	switch v {
	case "SCRAM":
		return iptr(15)
	case "ALLIED":
		return iptr(8)
	case "IC":
		return iptr(0)
	}
	return nil
}

func isReliefSwitch(t string) bool {
	u := strings.ToLower(strings.TrimSpace(t))
	if u == "" {
		return false
	}
	return reRelief.MatchString(u)
}

var gpsPayTypes = map[string]bool{"gps": true, "allied": true, "scram": true}

// ComputeGPS ports computeGPS(c, trackDateStr, sessionAdj, caseFilter).
// sessionAdj==nil uses the Blue Book day adjustment. caseFilter "" sums all of
// the IDN's GPS payments; a case token narrows the paid side to that case (the
// owed/days side is unchanged — it's the single per-IDN install record).
func ComputeGPS(c Client, track time.Time, sessionAdj *float64, caseFilter string) GPSResult {
	caseToks := caseTokens(caseFilter)
	vendor := vendorOf(strings.ToUpper(c.GpsType))
	dailyRate := rateOf(vendor)
	vendor2 := vendorOf(c.GpSwitchedTo)
	dailyRate2 := rateOf(vendor2)

	var totalGpsPaid float64
	for _, p := range c.Payments {
		if !matchCase(p.Case, caseToks) {
			continue
		}
		if gpsPayTypes[strings.ToLower(strings.TrimSpace(p.Type))] {
			totalGpsPaid += p.Amt
		}
	}

	bbAdj := c.DayAdj
	adj := bbAdj
	if sessionAdj != nil {
		adj = *sessionAdj
	}

	var daysActive *int
	var start, end time.Time
	var startOK, endOK bool
	if c.GpInstall != "" {
		if s, ok := ParseDay(c.GpInstall); ok {
			start, startOK = s, true
			end = track
			if c.ClosedOK && lt(c.ClosedD, track) {
				end = c.ClosedD
			}
			if isReliefSwitch(c.GpSwitchedTo) {
				if rd, ok := ParseDay(c.GpSwitchedDate); ok && ge(rd, start) && lt(rd, end) {
					end = rd
				}
			}
			endOK = true
			da := daysBetween(start, end) + 1
			if da < 0 {
				da = 0
			}
			daysActive = &da
		}
	}

	switchD, switchOK := ParseDay(c.GpSwitchedDate)
	hasSwitch := c.GpSwitchedTo != "" && switchOK && dailyRate2 != nil &&
		startOK && endOK && ge(switchD, start) && le(switchD, end)

	var totalOwed *float64
	if dailyRate != nil && startOK && endOK {
		if hasSwitch {
			dBefore := daysBetween(start, switchD)
			if dBefore < 0 {
				dBefore = 0
			}
			dAfter := daysBetween(switchD, end)
			if dAfter < 0 {
				dAfter = 0
			}
			v := float64(dBefore*(*dailyRate)) + 23 + float64(dAfter*(*dailyRate2))
			totalOwed = &v
		} else if daysActive != nil {
			v := float64(*daysActive * *dailyRate)
			totalOwed = &v
		}
	}

	// Adjustment converted to dollars at the rate in force at window end.
	var adjRate *int
	if hasSwitch && dailyRate2 != nil {
		adjRate = dailyRate2
	} else {
		adjRate = dailyRate
	}
	adjDollars := 0.0
	if adjRate != nil {
		adjDollars = adj * float64(*adjRate)
	}

	var surplusDollars *float64
	if totalOwed != nil {
		v := (totalGpsPaid + adjDollars) - *totalOwed
		surplusDollars = &v
	}

	var surplusDays *int
	if surplusDollars != nil && adjRate != nil && *adjRate > 0 {
		var sd int
		if *surplusDollars >= 0 {
			sd = int(math.Ceil(*surplusDollars / float64(*adjRate)))
		} else {
			sd = -int(math.Ceil(math.Abs(*surplusDollars) / float64(*adjRate)))
		}
		surplusDays = &sd
	}

	var covered *bool
	if surplusDollars != nil {
		cv := *surplusDollars >= 0
		covered = &cv
	}

	return GPSResult{
		Vendor: vendor, DailyRate: dailyRate, Vendor2: vendor2, DailyRate2: dailyRate2,
		HasSwitch: hasSwitch, ReliefSwitch: isReliefSwitch(c.GpSwitchedTo),
		TotalOwedDollars: totalOwed, TotalGpsPaid: totalGpsPaid, DaysActive: daysActive,
		Adj: adj, AdjDollars: adjDollars, SurplusDollars: surplusDollars,
		SurplusDays: surplusDays, Covered: covered,
	}
}

func iptr(i int) *int { return &i }
