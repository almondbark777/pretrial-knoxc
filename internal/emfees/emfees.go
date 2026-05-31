// Package emfees is a faithful Go port of the canonical "past-due-em-fees" skill
// (scripts/generate_memos.py — bundled for provenance in assets/SKILL_REFERENCE.md).
//
// It identifies Knox County Pre-Trial GPS clients who are 5+ days behind on their
// electronic-monitoring fees and produces the data for a filled-in past-due memo
// per person, split into Open and Closed lists. The methodology — daily rates,
// the 5-day threshold, the payment-type filter, inclusive day counting, mid-period
// device-switch dual billing, and the Closed-case start-date fallback chain — is
// the user's "already figured out" logic and is reproduced here EXACTLY. The memo
// itself is rendered from the user's own template (assets/memo_template.docx) by
// memo.go, so the letter format is reused, never recreated.
//
// Compute() is pure: it takes the three raw datasets as []map[string]string (the
// website feeds them straight from raw_gps_48_hours / raw_payments / raw_blue_book
// — see internal/db/emfees.go) and returns the Open/Closed records. The DB stores
// the SharePoint columns in snake_case, so the keys read here are snake_case; an
// uploaded-CSV adapter would pre-normalize headers to the same keys.
package emfees

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// dailyPaymentTypes are the payment types that count toward the daily monitoring
// fee (lower-cased, exact match). Install fees, PTR, and drug screens are excluded.
// Mirrors generate_memos.DAILY_PAYMENT_TYPES.
var dailyPaymentTypes = map[string]bool{
	"gps": true, "allied": true, "scram": true, "ptr, allied": true, "scram gps": true,
}

// rate returns the daily rate for a GPS type, and whether it is billable. ALLIED =
// $8/day, SCRAM = $15/day; anything else (IN CUSTODY, blank, "ALLIED (WORK RELEASE)")
// is not billed for daily monitoring and is skipped. Mirrors generate_memos.RATES.
func rate(gpsType string) (int, bool) {
	switch strings.ToUpper(strings.TrimSpace(gpsType)) {
	case "ALLIED":
		return 8, true
	case "SCRAM":
		return 15, true
	}
	return 0, false
}

// daysBehindThreshold: 5+ days behind triggers a memo.
const daysBehindThreshold = 5

var junkNameRe = regexp.MustCompile(`(?i)^!!!|\btest\b`)

func isJunkName(name string) bool { return junkNameRe.MatchString(name) }

// Rec is one past-due record (one memo). Fields mirror the Python rec dict.
type Rec struct {
	Name       string
	IDN        string
	Case       string
	Court      string
	Type       string // ALLIED | SCRAM
	Rate       int
	Start      time.Time // install date (Open) or billing-period start (Closed)
	End        time.Time // as-of (Open) or closed date (Closed)
	Days       int
	Owed       float64
	Paid       float64
	Behind     float64 // the arrearage printed on the memo (owed − paid)
	DaysBehind float64
	StartSrc   string // how the Closed start date was derived (audit trail)
	SwitchType string // ALLIED|SCRAM if a mid-period device switch was billed
	HasSwitch  bool
	Closed     bool // false = Open list, true = Closed list
}

// Result is the full computation: the Open and Closed record lists plus the as-of
// date and the skip counters (for the report's "if something looks off" notes).
type Result struct {
	AsOf          time.Time
	Open          []Rec
	Closed        []Rec
	SkippedJunk   int
	SkippedNoType int
}

// OpenTotal / ClosedTotal sum the arrearage across each list (Σ behind).
func (r Result) OpenTotal() float64   { return sumBehind(r.Open) }
func (r Result) ClosedTotal() float64 { return sumBehind(r.Closed) }

func sumBehind(recs []Rec) float64 {
	var t float64
	for _, x := range recs {
		t += x.Behind
	}
	return t
}

// ---- date / amount parsing (port of parse_date / parse_amount) ----

// parseDate parses "5/8/2026" or "5/8/2026 14:30" (the SharePoint export format,
// as stored in the raw_* tables). Returns the date at UTC midnight so day-count
// subtraction is exact and DST-free. Returns ok=false on blank/invalid, exactly
// as the Python parse_date returns None.
func parseDate(value string) (time.Time, bool) {
	s := strings.TrimSpace(value)
	if s == "" {
		return time.Time{}, false
	}
	s = strings.SplitN(s, " ", 2)[0] // drop any trailing time-of-day
	t, err := time.ParseInLocation("1/2/2006", s, time.UTC)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// parseAmount parses "$1,234.00" → 1234.0; blank/invalid → 0.0.
func parseAmount(value string) float64 {
	s := strings.TrimSpace(value)
	s = strings.ReplaceAll(s, "$", "")
	s = strings.ReplaceAll(s, ",", "")
	if s == "" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return f
}

// daysBetween returns the whole-day difference (b − a). Both args are UTC-midnight,
// so the hour count is an exact multiple of 24. Mirrors Python (b - a).days.
func daysBetween(a, b time.Time) int { return int(b.Sub(a).Hours()) / 24 }

func get(m map[string]string, key string) string { return strings.TrimSpace(m[key]) }

// bbCase returns the blue-book "Case Number": warrant_case_num, falling back to a
// case_number column if one ever exists. Mirrors BuildClients' firstNonEmpty.
func bbCase(m map[string]string) string {
	if v := get(m, "warrant_case_num"); v != "" {
		return v
	}
	return get(m, "case_number")
}

// ---- payments aggregation (port of load_payments) ----

type paymentIndex struct {
	byCase     map[[2]string]float64 // (case_number, idn) → Σ daily-fee payments
	byIDN      map[string]float64    // idn → Σ daily-fee payments
	firstByIDN map[string]time.Time  // idn → earliest daily-fee payment date
}

func loadPayments(rows []map[string]string) paymentIndex {
	idx := paymentIndex{
		byCase:     map[[2]string]float64{},
		byIDN:      map[string]float64{},
		firstByIDN: map[string]time.Time{},
	}
	for _, r := range rows {
		ptype := strings.ToLower(get(r, "payment_type"))
		if !dailyPaymentTypes[ptype] {
			continue
		}
		amt := parseAmount(r["payment_amount"])
		caseNo := get(r, "case_number")
		idn := get(r, "idn")
		idx.byCase[[2]string{caseNo, idn}] += amt
		idx.byIDN[idn] += amt
		if d, ok := parseDate(r["payment_date"]); ok {
			if cur, seen := idx.firstByIDN[idn]; !seen || d.Before(cur) {
				idx.firstByIDN[idn] = d
			}
		}
	}
	return idx
}

// paidFor mirrors Python `paid_by_case.get((case, idn)) or paid_by_idn.get(idn)`:
// a zero (or missing) case-specific total falls back to the whole-IDN total.
func (idx paymentIndex) paidFor(caseNo, idn string) float64 {
	if v := idx.byCase[[2]string{caseNo, idn}]; v != 0 {
		return v
	}
	return idx.byIDN[idn]
}

// ---- blue-book grouping + lookups (port of load_blue_book + lookups) ----

func groupByIDN(rows []map[string]string) ([]string, map[string][]map[string]string) {
	order := []string{}
	by := map[string][]map[string]string{}
	for _, r := range rows {
		idn := get(r, "idn")
		if idn == "" {
			continue
		}
		if _, ok := by[idn]; !ok {
			order = append(order, idn)
		}
		by[idn] = append(by[idn], r)
	}
	return order, by
}

// lookupCourt pulls COURT from the blue book, preferring the row matching this
// case#, else any populated COURT. (raw_blue_book has no COURT column today, so
// this returns "" and the memo leaves Court blank for the officer to fill — the
// exact behavior the skill documents for closed cases.)
func lookupCourt(bb map[string][]map[string]string, idn, caseNo string) string {
	recs := bb[idn]
	if len(recs) == 0 {
		return ""
	}
	caseClean := strings.TrimSpace(strings.ReplaceAll(caseNo, "@", ""))
	if caseClean != "" {
		for _, r := range recs {
			bbc := strings.TrimSpace(strings.ReplaceAll(bbCase(r), "@", ""))
			if bbc != "" && strings.Contains(bbc, caseClean) {
				if c := get(r, "court"); c != "" {
					return c
				}
			}
		}
	}
	for _, r := range recs {
		if c := get(r, "court"); c != "" {
			return c
		}
	}
	return ""
}

// lookupGPSType finds a known ALLIED/SCRAM type in the blue book when the 48-hour
// row lacks one. Returns "" if none.
func lookupGPSType(bb map[string][]map[string]string, idn string) string {
	for _, r := range bb[idn] {
		if _, ok := rate(r["gps_type"]); ok {
			return strings.ToUpper(get(r, "gps_type"))
		}
	}
	return ""
}

// bestCaseNumber returns a real (non-placeholder) case# from the blue book; skips
// "needs…"/"test…" stand-ins so the memo doesn't print a placeholder.
func bestCaseNumber(bb map[string][]map[string]string, idn string) string {
	for _, r := range bb[idn] {
		c := bbCase(r)
		lc := strings.ToLower(c)
		if c != "" && !strings.Contains(lc, "needs") && !strings.Contains(lc, "test") {
			return c
		}
	}
	return ""
}

// ---- the billing math (port of compute_owed) ----

// computeOwed returns (days, owed) for the inclusive billing period start..end.
// Day counting is INCLUSIVE on both ends (the install day is day 1). If the client
// switched device mid-period, the switch day is charged at BOTH rates.
func computeOwed(start, end time.Time, r int, switchRate int, switchDate time.Time, hasSwitch bool) (int, float64) {
	valid := hasSwitch && !switchDate.Before(start) && !switchDate.After(end)
	if !valid {
		days := daysBetween(start, end) + 1
		return days, float64(days * r)
	}
	preDays := daysBetween(start, switchDate)
	postDays := daysBetween(switchDate, end)
	owed := float64(preDays*r) + float64(r+switchRate) + float64(postDays*switchRate)
	totalDays := preDays + 1 + postDays
	return totalDays, owed
}

// Compute runs the full analysis. Mirrors compute_open_and_closed: Pass 1 walks the
// GPS 48-hour rows (Open + any 48h Closed), Pass 2 supplements with blue-book-only
// closed cases that never appear in the 48-hour file.
func Compute(gps48, payments, blueBook []map[string]string, asOf time.Time) Result {
	asOf = time.Date(asOf.Year(), asOf.Month(), asOf.Day(), 0, 0, 0, 0, time.UTC)
	pay := loadPayments(payments)
	bbOrder, bb := groupByIDN(blueBook)

	res := Result{AsOf: asOf}
	seen := map[string]bool{}

	// === Pass 1: GPS 48 Hours file ===
	for _, r := range gps48 {
		install, ok := parseDate(r["gps_install_date"])
		if !ok {
			continue
		}
		name := get(r, "defendant")
		if isJunkName(name) {
			res.SkippedJunk++
			continue
		}
		idn := get(r, "idn")
		caseNo := get(r, "case_number")
		status := strings.ToUpper(get(r, "case_status"))

		gpsType := strings.ToUpper(get(r, "gps_type"))
		rt, ok := rate(gpsType)
		if !ok {
			gpsType = lookupGPSType(bb, idn)
			rt, ok = rate(gpsType)
			if !ok {
				res.SkippedNoType++
				continue
			}
		}

		var end time.Time
		if status == "CLOSED" {
			end, ok = parseDate(r["closed_date"])
			if !ok {
				continue
			}
		} else {
			end = asOf
		}
		if daysBetween(install, end) < 0 {
			continue
		}

		// Mid-period device switch (only honor real ALLIED/SCRAM switches).
		switchType := strings.ToUpper(get(r, "switched_to"))
		switchRate, hasSwitch := rate(switchType)
		switchDate, sdOK := parseDate(r["switched_gps_date"])
		hasSwitch = hasSwitch && sdOK

		days, owed := computeOwed(install, end, rt, switchRate, switchDate, hasSwitch)
		if days <= 0 {
			continue
		}

		effRate := rt
		if hasSwitch {
			effRate = switchRate
		}
		paid := pay.paidFor(caseNo, idn)
		behind := owed - paid
		if behind/float64(effRate) < daysBehindThreshold {
			continue
		}

		rec := Rec{
			Name: name, IDN: idn, Case: caseNo, Type: gpsType, Rate: rt,
			Start: install, End: end, Days: days,
			Owed: owed, Paid: paid, Behind: behind, DaysBehind: behind / float64(effRate),
			Court: lookupCourt(bb, idn, caseNo), StartSrc: "Install (48hr)",
			SwitchType: switchType, HasSwitch: hasSwitch, Closed: status == "CLOSED",
		}
		seen[idn] = true
		if rec.Closed {
			res.Closed = append(res.Closed, rec)
		} else {
			res.Open = append(res.Open, rec)
		}
	}

	// === Pass 2: blue-book-only closed cases (had GPS, no 48-hour row) ===
	for _, idn := range bbOrder {
		if seen[idn] {
			continue
		}
		recs := bb[idn]
		statuses := map[string]bool{}
		for _, r := range recs {
			statuses[strings.ToUpper(get(r, "case_status"))] = true
		}
		if statuses["OPEN"] {
			continue
		}
		if !statuses["CLOSED"] && !statuses["CCLOSED"] {
			continue
		}
		hadGPS := false
		for _, r := range recs {
			if strings.ToLower(get(r, "gps")) == "true" {
				hadGPS = true
				break
			}
			if _, ok := rate(r["gps_type"]); ok {
				hadGPS = true
				break
			}
		}
		if !hadGPS {
			continue
		}

		// Representative row: first with a billable GPS type, else the first row.
		rep := recs[0]
		for _, r := range recs {
			if _, ok := rate(r["gps_type"]); ok {
				rep = r
				break
			}
		}
		name := get(rep, "defendant")
		if isJunkName(name) {
			continue
		}
		gpsType := strings.ToUpper(get(rep, "gps_type"))
		rt, ok := rate(gpsType)
		if !ok {
			continue
		}

		// Start-date fallback chain: first payment → Released to Hilltop → Referral.
		var start time.Time
		startSrc := ""
		if d, ok := pay.firstByIDN[idn]; ok {
			start, startSrc = d, "First payment"
		}
		if startSrc == "" {
			if d, ok := parseDate(rep["released_to_hilltop_date"]); ok {
				start, startSrc = d, "Released to Hilltop"
			}
		}
		if startSrc == "" {
			if d, ok := parseDate(rep["referral_date"]); ok {
				start, startSrc = d, "Referral"
			}
		}
		if startSrc == "" {
			continue
		}

		var end time.Time
		hasEnd := false
		for _, r := range recs {
			if d, ok := parseDate(r["closed_date"]); ok {
				if !hasEnd || d.After(end) {
					end, hasEnd = d, true
				}
			}
		}
		if !hasEnd {
			continue
		}
		if daysBetween(start, end) < 0 {
			continue
		}

		days, owed := computeOwed(start, end, rt, 0, time.Time{}, false)
		if days <= 0 {
			continue
		}
		paid := pay.byIDN[idn]
		behind := owed - paid
		if behind/float64(rt) < daysBehindThreshold {
			continue
		}
		caseNo := bestCaseNumber(bb, idn)
		res.Closed = append(res.Closed, Rec{
			Name: name, IDN: idn, Case: caseNo, Type: gpsType, Rate: rt,
			Start: start, End: end, Days: days,
			Owed: owed, Paid: paid, Behind: behind, DaysBehind: behind / float64(rt),
			Court: lookupCourt(bb, idn, caseNo), StartSrc: startSrc, Closed: true,
		})
	}

	sortByName(res.Open)
	sortByName(res.Closed)
	return res
}

// sortByName orders records by defendant name (then IDN) for stable memo output.
func sortByName(recs []Rec) {
	sort.SliceStable(recs, func(i, j int) bool {
		ni, nj := strings.ToUpper(recs[i].Name), strings.ToUpper(recs[j].Name)
		if ni != nj {
			return ni < nj
		}
		return recs[i].IDN < recs[j].IDN
	})
}
