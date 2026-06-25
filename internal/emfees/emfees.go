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

// reliefSwitchRe matches a "Switched To" value that means the client is no longer
// on a device — "No GPS", "off GPS", "GPS relieved", "removed". Identical to the
// canonical tracker's _isReliefSwitch (and compute.reRelief), so the EM-fee report
// freezes billing on relief the same way the client tracker does.
var reliefSwitchRe = regexp.MustCompile(`\bno\s*gps\b|\bgps\s*reliev|\boff\s*gps\b|\bgps\s*off\b|\bremov`)

func isReliefSwitch(switchedTo string) bool {
	return reliefSwitchRe.MatchString(strings.ToLower(strings.TrimSpace(switchedTo)))
}

// daysBehindThreshold: 5+ days behind triggers a memo.
const daysBehindThreshold = 5

var junkNameRe = regexp.MustCompile(`(?i)^!!!|\btest\b`)

func isJunkName(name string) bool { return junkNameRe.MatchString(name) }

// Rec is one past-due record (one memo). Fields mirror the Python rec dict.
type Rec struct {
	Name        string
	IDN         string
	Case        string
	Court       string
	Type        string // ALLIED | SCRAM
	Rate        int
	Start       time.Time // install date (Open) or billing-period start (Closed)
	End         time.Time // as-of (Open) or closed date (Closed)
	Days        int
	Owed        float64
	Paid        float64
	Behind      float64 // the arrearage printed on the memo (owed − paid)
	DaysBehind  float64
	StartSrc    string // how the Closed start date was derived (audit trail)
	SwitchType  string // ALLIED|SCRAM if a mid-period device switch was billed
	HasSwitch   bool
	CustodyDays int  // in-custody days excluded from this person's billing
	Closed      bool // false = Open list, true = Closed list
}

// CustodyRange is one raw in-custody span (date strings as stored). Only the full
// days strictly between Start and End are excluded from GPS billing; BOTH the
// take-off day (Start) and the "back on GPS" day (End) are billed (the vendor
// charges for the removal and reinstall days). Empty End = still in custody
// (excluded through the billing-period end). Parsed with the same parseDate the
// rest of the engine uses.
type CustodyRange struct{ Start, End string }

// custodyDaysInWindow returns how many days of [winStart, winEnd] (inclusive) fall
// in a custody span and so must not be billed. Each span excludes the OPEN interval
// (Start, End) — both the take-off and reinstall days are billed; an empty End runs
// through winEnd. Overlaps are merged.
func custodyDaysInWindow(ranges []CustodyRange, winStart, winEnd time.Time) int {
	if len(ranges) == 0 || winEnd.Before(winStart) {
		return 0
	}
	winEndExcl := winEnd.AddDate(0, 0, 1)
	type span struct{ a, b time.Time }
	var spans []span
	for _, r := range ranges {
		s, ok := parseDate(r.Start)
		if !ok {
			continue
		}
		// Exclude the OPEN interval (Start, End): the take-off day is billed (the
		// vendor charges for the removal day), so exclusion begins the day AFTER it.
		s = s.AddDate(0, 0, 1)
		if s.Before(winStart) {
			s = winStart
		}
		e := winEndExcl
		if ed, ok := parseDate(r.End); ok {
			e = ed
		}
		if e.After(winEndExcl) {
			e = winEndExcl
		}
		if e.After(s) {
			spans = append(spans, span{s, e})
		}
	}
	if len(spans) == 0 {
		return 0
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].a.Before(spans[j].a) })
	total, curA, curB := 0, spans[0].a, spans[0].b
	for _, sp := range spans[1:] {
		if !sp.a.After(curB) {
			if sp.b.After(curB) {
				curB = sp.b
			}
			continue
		}
		total += daysBetween(curA, curB)
		curA, curB = sp.a, sp.b
	}
	total += daysBetween(curA, curB)
	return total
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
	// Drop any trailing time-of-day, whether space- or 'T'-separated
	// ("5/11/2026 1:00", "2026-05-11T00:00:00Z").
	if i := strings.IndexAny(s, " T"); i > 0 {
		s = s[:i]
	}
	// Accept both the US export format and canonical ISO. The daily importer keeps
	// US ("5/11/2026"); the reconcile tool canonicalizes to ISO ("2026-05-11"), so
	// the report must read both or it would silently skip ISO-stored rows.
	for _, layout := range []string{"1/2/2006", "2006-1-2"} {
		if t, err := time.ParseInLocation(layout, s, time.UTC); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
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

// lookupName returns the defendant name from the blue book for this IDN, used when
// the GPS 48-hour row's Defendant column is blank. The live SharePoint export of the
// 48-hour file ships an empty Defendant column for every row, so without this fall
// back every Open show-cause letter would print with no name. The blue book carries
// a name for effectively every IDN, so this fills the gap. Returns "" if none.
func lookupName(bb map[string][]map[string]string, idn string) string {
	for _, r := range bb[idn] {
		if n := get(r, "defendant"); n != "" {
			return n
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
	return ComputeWithCustody(gps48, payments, blueBook, nil, asOf)
}

// ComputeWithCustody is Compute plus per-IDN in-custody spans, whose days are
// excluded from each person's GPS billing (and so can drop someone below the
// 5-day threshold entirely). custody is keyed by IDN; nil means "no custody data"
// and behaves exactly like Compute.
func ComputeWithCustody(gps48, payments, blueBook []map[string]string, custody map[string][]CustodyRange, asOf time.Time) Result {
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
		idn := get(r, "idn")
		name := get(r, "defendant")
		if name == "" {
			name = lookupName(bb, idn) // 48-hour Defendant column is blank in the live export
		}
		if isJunkName(name) {
			res.SkippedJunk++
			continue
		}
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
		// GPS relieved: when the row records the client is off the device
		// ("Switched To" = "No GPS" / "off GPS" / "removed") with a valid switch
		// date inside the billing window, billing FREEZES at that date — days stop
		// counting after it. Mirrors the canonical computeGPS relief-switch rule so
		// the report agrees with the client tracker (e.g. a client moved to plea
		// SCRAM stops accruing EM fees). A relief row carries no billable rate, so
		// it only shortens the window; real ALLIED/SCRAM device switches below still
		// dual-bill across their switch date as before.
		if reliefDate, rok := parseDate(r["switched_gps_date"]); rok &&
			isReliefSwitch(get(r, "switched_to")) &&
			!reliefDate.Before(install) && reliefDate.Before(end) {
			end = reliefDate
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
		// In-custody days aren't billed (the "back on GPS" day is). Subtract them
		// before the threshold test so custody can clear someone off the list. When a
		// real device switch dual-bills across switchDate, the per-day credit must use
		// the rate that day was BILLED at (computeOwed boundaries), or a $15-billed day
		// is credited at $8 and the owed is overstated (#4, mirror of compute #3):
		//   pre-switch  [install, switchDate-1] @ rt
		//   switch day  switchDate              @ rt+switchRate (billed at both)
		//   post-switch [switchDate+1, end]     @ switchRate
		custodyDays := custodyDaysInWindow(custody[idn], install, end)
		if custodyDays > 0 {
			var credit int
			if hasSwitch && !switchDate.Before(install) && !switchDate.After(end) {
				preDays := custodyDaysInWindow(custody[idn], install, switchDate.AddDate(0, 0, -1))
				switchDay := custodyDaysInWindow(custody[idn], switchDate, switchDate)
				postDays := custodyDaysInWindow(custody[idn], switchDate.AddDate(0, 0, 1), end)
				// Segments partition [install,end] at switchDate, so they sum to the
				// flat custody count — guard the invariant the recipe requires.
				if preDays+switchDay+postDays != custodyDays {
					panic("emfees: custody switch segments must sum to custodyDays")
				}
				credit = preDays*rt + switchDay*(rt+switchRate) + postDays*switchRate
			} else {
				credit = custodyDays * effRate
			}
			days -= custodyDays
			if days < 0 {
				days = 0
			}
			owed -= float64(credit)
			if owed < 0 {
				owed = 0
			}
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
			SwitchType: switchType, HasSwitch: hasSwitch, CustodyDays: custodyDays, Closed: status == "CLOSED",
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
		custodyDays := custodyDaysInWindow(custody[idn], start, end)
		if custodyDays > 0 {
			days -= custodyDays
			if days < 0 {
				days = 0
			}
			owed -= float64(custodyDays * rt)
			if owed < 0 {
				owed = 0
			}
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
			Court: lookupCourt(bb, idn, caseNo), StartSrc: startSrc, CustodyDays: custodyDays, Closed: true,
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
