package handlers

import (
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"pretrial-knoxc/internal/compute"
	"pretrial-knoxc/internal/models"
)

var reOpen = regexp.MustCompile(`(?i)open`)

// openRep returns the representative case for an IDN: the first open-status row
// if any, else the first row. Mirrors the canonical roster dedup, which keeps
// one rep per IDN and prefers an open case over a closed one — so a multi-case
// defendant's roster membership and per-case fields no longer depend on which
// blue_book row happened to be last (PHASE_4 recheck finding G1). Returns nil
// for an empty slice.
func openRep(cases []*compute.Client) *compute.Client {
	if len(cases) == 0 {
		return nil
	}
	for _, c := range cases {
		if reOpen.MatchString(c.Status) {
			return c
		}
	}
	return cases[0]
}

// selectCase picks which case row to display for an IDN and the payment
// case-filter to apply. A specific case (e.g. "@1516438") anchors the check-in
// windows to that row and narrows PTR/GPS payments to it; otherwise the open
// rep is used with no narrowing (whole-client totals). Mirrors the canonical
// per-case dropdown (selectedCase + caseFilter).
func selectCase(cases []*compute.Client, caseQ string) (*compute.Client, string) {
	caseQ = strings.TrimSpace(caseQ)
	if caseQ != "" {
		for _, c := range cases {
			if compute.CaseMatches(c.CaseNo, caseQ) {
				return c, caseQ
			}
		}
		return openRep(cases), caseQ // no row matched; still narrow payments
	}
	return openRep(cases), ""
}

// caseOptions returns the distinct case tokens across all of an IDN's rows, in
// first-seen order — the option list for the profile's case-selector (mirrors
// the canonical getCasesForIdn). Empty/one-element results mean no selector.
func caseOptions(cases []*compute.Client) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range cases {
		for _, tok := range compute.CaseTokens(c.CaseNo) {
			if !seen[tok] {
				seen[tok] = true
				out = append(out, tok)
			}
		}
	}
	return out
}

// hasCheckInRecord reports whether a client has at least one parseable check-in
// on file. Clients with none have no digital history to judge, so the missed
// reports skip them (see CheckInDataFloor) rather than flag absent data.
func hasCheckInRecord(c *compute.Client) bool {
	for _, ci := range c.CheckIns {
		if ci.DOK {
			return true
		}
	}
	return false
}

// reportedMissed filters ComputeCheckIns' raw missed windows down to the ones the
// office should act on: only windows whose deadline is on/after CheckInDataFloor,
// and only for clients who actually have check-in records. This is a
// reporting-layer filter — the underlying compute (and the per-client profile
// view) still expose every window; only aggregate counts/rosters use this.
func reportedMissed(c *compute.Client, ci compute.CheckInResult) []compute.Window {
	if !hasCheckInRecord(c) {
		return nil
	}
	floor := compute.CheckInDataFloor()
	out := make([]compute.Window, 0, len(ci.Missed))
	for _, w := range ci.Missed {
		if !w.Deadline.Before(floor) {
			out = append(out, w)
		}
	}
	return out
}

// behindRoster mirrors the HTML BehindRoster default view but nets GPS across
// the IDN's cases (GPS is per-case — a person can have several GPS cases). One
// row per IDN, summing owed/paid/surplus over the IDN's DISTINCT GPS-active
// cases; included when the net surplus is negative and the open rep is open.
// Payments are split per case only when the IDN has more than one GPS case —
// single-GPS-case clients use the all-payments sum, identical to before. Sorted
// by name.
func behindRoster(clients map[string][]*compute.Client, track time.Time) []models.RosterRow {
	var rows []models.RosterRow
	for _, cases := range clients {
		// GPS-active cases (the canonical filters !gpsActive first).
		var gpsCases []*compute.Client
		for _, c := range cases {
			if c.GpsActive {
				gpsCases = append(gpsCases, c)
			}
		}
		rep := openRep(gpsCases)
		// Default 'open' filter: a closed-only IDN has a non-open rep -> excluded.
		if rep == nil || !reOpen.MatchString(rep.Status) {
			continue
		}
		// An officer reviewed this person and confirmed they're not actually behind
		// (problem report #12) — hold them off the roster.
		if rep.NotBehind {
			continue
		}
		n := netGPS(gpsCases, rep, track)
		if !n.HaveOwed {
			continue
		}
		if n.Surplus >= 0 {
			continue
		}
		lvl, _ := compute.ParseLevel(rep.Level)
		detail := "behind $" + ftoa(-n.Surplus)
		if n.Rate != nil && *n.Rate > 0 {
			if days := int(math.Ceil(-n.Surplus / float64(*n.Rate))); days > 0 {
				detail += " / " + itoa(days) + " days"
			}
		}
		rows = append(rows, models.RosterRow{
			IDN: rep.IDN, Name: rep.Name, Officer: rep.Officer, Level: lvl,
			Detail: detail, Amount: n.Surplus,
			Owed: n.Owed, Paid: n.Paid, Waived: compute.IsFeesWaived(rep.GpNotes),
		})
	}
	sortByName(rows)
	return rows
}

// netGPSResult is the NET GPS owed/paid/surplus summed across an IDN's DISTINCT
// GPS-active cases. Cases is the distinct GPS-case count (>1 = a multi-GPS-case
// client, whose per-case card diverges from this net). HaveOwed is false when no
// case yields an owed figure (e.g. vendor unset) — callers treat that as
// "uncomputable" and fall back to the per-case view.
type netGPSResult struct {
	Owed, Adj, Paid, Surplus float64
	Rate                     *int
	Cases                    int
	HaveOwed                 bool
}

// netGPS nets GPS owed/paid/surplus across an IDN's DISTINCT GPS-active cases:
// OWED summed per case (each case's own window), PAID counted ONCE via a union
// caseFilter. A payment tagged with several case #s must not be credited once
// per case — that double-counts (audit 2026-06-27: 5 idns, $6,375 over-credited).
// A single GPS case uses the all-payments sum (filter="") so a payment with a
// blank/odd case_number still credits — identical to the pre-per-case behavior.
// This is the shared net math behind both the Behind-on-GPS roster and the
// record's net GPS card, so the two never diverge. rep is the IDN's
// open-preferred GPS case (its payment list backs the union sum); pass
// openRep(gpsCases).
func netGPS(gpsCases []*compute.Client, rep *compute.Client, track time.Time) netGPSResult {
	// One entry per distinct case (by case-token set) so two blue-book rows for
	// the same case aren't double counted.
	distinct := map[string]*compute.Client{}
	var order []string
	for _, c := range gpsCases {
		k := caseKey(c.CaseNo)
		if _, ok := distinct[k]; !ok {
			distinct[k] = c
			order = append(order, k)
		}
	}

	var n netGPSResult
	n.Cases = len(order)
	switch {
	case len(order) == 0 || rep == nil:
		return n
	case len(order) == 1:
		g := compute.ComputeGPS(*distinct[order[0]], track, nil, "")
		if g.TotalOwedDollars == nil {
			return n
		}
		n.Owed, n.Adj, n.Paid, n.Rate = *g.TotalOwedDollars, g.AdjDollars, g.TotalGpsPaid, g.DailyRate
		n.HaveOwed = true
	default:
		var union []string
		for _, k := range order {
			c := distinct[k]
			g := compute.ComputeGPS(*c, track, nil, c.CaseNo)
			if g.TotalOwedDollars == nil {
				continue
			}
			n.HaveOwed = true
			n.Owed += *g.TotalOwedDollars
			n.Adj += g.AdjDollars
			if n.Rate == nil {
				n.Rate = g.DailyRate
			}
			if strings.TrimSpace(c.CaseNo) != "" {
				union = append(union, c.CaseNo)
			}
		}
		n.Paid = compute.ComputeGPS(*rep, track, nil, strings.Join(union, ", ")).TotalGpsPaid
	}
	n.Surplus = (n.Paid + n.Adj) - n.Owed
	return n
}

// reviewedNotBehindRoster lists clients an officer has marked "reviewed — not
// behind" (problem report #12), so the compliance page can show them with an
// Undo. One row per IDN.
func reviewedNotBehindRoster(clients map[string][]*compute.Client) []models.RosterRow {
	var rows []models.RosterRow
	for _, cases := range clients {
		rep := openRep(cases)
		if rep == nil || !rep.NotBehind {
			continue
		}
		lvl, _ := compute.ParseLevel(rep.Level)
		rows = append(rows, models.RosterRow{
			IDN: rep.IDN, Name: rep.Name, Officer: rep.Officer, Level: lvl,
		})
	}
	sortByName(rows)
	return rows
}

// caseKey is a stable identity for a case from its case-number tokens (sorted),
// so two blue-book rows for the same case map to the same key.
func caseKey(caseNo string) string {
	toks := compute.CaseTokens(caseNo)
	sort.Strings(toks)
	return strings.Join(toks, ",")
}

// missedCheckInsRoster mirrors the HTML MissedCheckInsRoster: one open rep per
// IDN who has not checked in during the current calendar month, EXCLUDING L1,
// honoring the 3-day grace (still in grace AND grace ends after the month start
// -> skip). The "checked this month" test spans the full calendar month
// (monthStart..monthEnd), matching the spec — not capped at trackDate (G4).
func missedCheckInsRoster(clients map[string][]*compute.Client, track time.Time) []models.RosterRow {
	monthStart := compute.Noon(track.Year(), track.Month(), 1)
	monthEnd := monthStart.AddDate(0, 1, -1) // last day of the month, noon UTC
	var rows []models.RosterRow
	// The evaluated month predates digital check-in capture: nothing to report
	// (won't happen at today's date, but keeps the data-floor rule explicit).
	if monthEnd.Before(compute.CheckInDataFloor()) {
		return rows
	}
	for _, cases := range clients {
		c := openRep(cases)
		if c == nil || !reOpen.MatchString(c.Status) {
			continue // open cases only
		}
		// No digital check-in history at all -> we can't judge compliance, so don't
		// flood the roster with defendants whose absence reflects missing data
		// rather than a missed visit (see compute.CheckInDataFloor).
		if !hasCheckInRecord(c) {
			continue
		}
		lvl, _ := compute.ParseLevel(c.Level)
		if lvl == 1 {
			continue // L1 excluded from the monthly roster (v0.74)
		}
		if !c.RefOK {
			continue
		}
		// Any check-in (in-person OR phone) satisfies the periodic windows, but an
		// IN-PERSON visit is required each calendar month (policy revised 2026-06-25):
		// a client who checks in every week is not flagged just for alternating
		// types, while one who only ever phones is still caught. Flag when there's no
		// in-person visit this month.
		hasIP, hasPh := false, false
		for _, ci := range c.CheckIns {
			if ci.DOK && !ci.D.Before(monthStart) && !ci.D.After(monthEnd) {
				if ip, ph := compute.CheckInKind(ci.Type); ip {
					hasIP = true
				} else if ph {
					hasPh = true
				}
			}
		}
		if hasIP {
			continue // in-person requirement met for the month
		}
		// 3-day grace: if still inside grace and grace ends at/after month start, skip.
		graceEnd := c.RefD.AddDate(0, 0, 3)
		if !graceEnd.Before(track) && !graceEnd.Before(monthStart) {
			continue
		}
		mo := track.Format("January 2006")
		detail := "no in-person check-in in " + mo
		if !hasPh {
			detail = "no check-in at all in " + mo
		}
		rows = append(rows, models.RosterRow{
			IDN: c.IDN, Name: c.Name, Officer: c.Officer, Level: lvl,
			Detail: detail,
		})
	}
	sortByName(rows)
	return rows
}

// violationsSinceEpoch keeps only violations dated on/after the stats go-live
// epoch (compute.StatsEpoch). The console's aggregate violation count + alert feed
// reflect the production era, not migrated/pre-go-live history. Undated rows are
// excluded (can't be confirmed in-period). Per-client records use the unfiltered
// list, so an individual's full history is unaffected.
func violationsSinceEpoch(vs []models.Violation) []models.Violation {
	epoch := compute.StatsEpoch()
	out := make([]models.Violation, 0, len(vs))
	for _, v := range vs {
		if d, ok := compute.ParseDay(v.ViolationDate); ok && !d.Before(epoch) {
			out = append(out, v)
		}
	}
	return out
}

// rosterStateCounts tallies the cheap, current-state roster sizes with NO
// per-client compute: distinct IDNs, open/closed by status, and GPS-active among
// OPEN cases only (people currently wearing, not closed/old referrals).
// BehindGPS/MissedMonth are left zero — those require full roster passes,
// so callers that already hold the behind/missed rosters set the lengths
// themselves instead of recomputing (see consoleDashboard).
func rosterStateCounts(clients map[string][]*compute.Client) models.Stats {
	s := models.Stats{Total: len(clients)} // distinct IDNs
	for _, cases := range clients {
		c := openRep(cases)
		if c == nil {
			continue
		}
		if reOpen.MatchString(c.Status) {
			s.Open++
		} else if strings.HasPrefix(strings.ToLower(c.Status), "closed") {
			s.Closed++
		}
		// GPS-active = currently wearing: count only people with an OPEN case on
		// GPS, not closed/old GPS referrals (the KPI was over-reporting total
		// referrals — supervisor feedback, 2026-06).
		if reOpen.MatchString(c.Status) {
			for _, cc := range cases {
				if cc.GpsActive {
					s.GPSActive++
					break
				}
			}
		}
	}
	return s
}

func computeStats(clients map[string][]*compute.Client, track time.Time) models.Stats {
	s := rosterStateCounts(clients)
	s.BehindGPS = len(behindRoster(clients, track))
	s.MissedMonth = len(missedCheckInsRoster(clients, track))
	return s
}

// behindMissedSets builds IDN→true lookup sets for the Behind-on-GPS and
// Missed-check-ins rosters, from the same roster functions both views use (so
// the flags never diverge). Shared by consoleClientRows and defendantRows.
func behindMissedSets(clients map[string][]*compute.Client, track time.Time) (behind, missed map[string]bool) {
	behind = map[string]bool{}
	for _, r := range behindRoster(clients, track) {
		behind[r.IDN] = true
	}
	missed = map[string]bool{}
	for _, r := range missedCheckInsRoster(clients, track) {
		missed[r.IDN] = true
	}
	return
}

// defendantRows builds one row per IDN (open-preferred rep) with computed
// compliance, for the case-management grid + /api/defendants. Behind/Missed
// flags are taken from the SAME roster functions (no divergence).
func defendantRows(clients map[string][]*compute.Client, track time.Time) []models.DefendantRow {
	behind, missed := behindMissedSets(clients, track)
	rows := make([]models.DefendantRow, 0, len(clients))
	for idn, cases := range clients {
		c := openRep(cases)
		if c == nil {
			continue
		}
		lvl, _ := compute.ParseLevel(c.Level)
		gps := compute.ComputeGPS(*c, track, nil, "")
		ptr := compute.ComputePTRFees(*c, track, "")
		ci := compute.ComputeCheckIns(*c, track)
		rows = append(rows, models.DefendantRow{
			IDN: idn, Name: c.Name, Level: lvl, Status: c.Status, Officer: c.Officer,
			CaseNo: c.CaseNo, GpsActive: c.GpsActive, GpsVendor: gps.Vendor,
			GpsSurplus: gps.SurplusDollars, BehindGPS: behind[idn],
			PTRBalance: ptr.Balance, MissedCount: len(reportedMissed(c, ci)), MissedMonth: missed[idn],
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		return strings.ToUpper(rows[i].Name) < strings.ToUpper(rows[j].Name)
	})
	return rows
}

func levelLabel(lvl int) string {
	if lvl >= 1 && lvl <= 3 {
		return "L" + strconv.Itoa(lvl)
	}
	return "Unknown"
}

func vendorLabel(v string) string {
	if v == "" {
		return "Unknown / MISSING"
	}
	return v
}

// analyticsData summarizes the ACTIVE (open) roster — distributions by level,
// GPS vendor, and officer caseload, plus owed/paid totals. Closed cases are
// frozen and excluded (consistent with the open-only rosters).
// Behind/missed sets are built once here so Stats can be assembled from
// rosterStateCounts+len() without a second full-roster pass (#14 dedup).
func analyticsData(clients map[string][]*compute.Client, track time.Time) models.Analytics {
	behind, missed := behindMissedSets(clients, track)
	st := rosterStateCounts(clients)
	st.BehindGPS = len(behind)
	st.MissedMonth = len(missed)
	a := models.Analytics{Stats: st}
	lvlCount := map[int]int{}
	vendorCount := map[string]int{}
	officerCount := map[string]int{}
	for _, cases := range clients {
		c := openRep(cases)
		if c == nil || !reOpen.MatchString(c.Status) {
			continue // active roster only
		}
		lvl, _ := compute.ParseLevel(c.Level)
		lvlCount[lvl]++
		if c.Officer != "" {
			officerCount[c.Officer]++
		}
		if c.GpsActive {
			g := compute.ComputeGPS(*c, track, nil, "")
			vendorCount[vendorLabel(g.Vendor)]++
			if g.TotalOwedDollars != nil {
				a.TotalGpsOwed += *g.TotalOwedDollars
			}
			a.TotalGpsPaid += g.TotalGpsPaid
		}
		ptr := compute.ComputePTRFees(*c, track, "")
		a.TotalPtrOwed += ptr.TotalOwed
		a.TotalPtrPaid += ptr.TotalPaid
	}
	// Level bars in a fixed, meaningful order.
	for _, lvl := range []int{1, 2, 3, 0} {
		if n := lvlCount[lvl]; n > 0 {
			a.ByLevel = append(a.ByLevel, models.Bar{Label: levelLabel(lvl), Count: n})
		}
	}
	for _, v := range []string{"SCRAM", "ALLIED", "IC", "Unknown / MISSING"} {
		if n := vendorCount[v]; n > 0 {
			a.ByVendor = append(a.ByVendor, models.Bar{Label: v, Count: n})
		}
	}
	a.TopOfficers = topBars(officerCount, 12)
	scaleBars(a.ByLevel)
	scaleBars(a.ByVendor)
	scaleBars(a.TopOfficers)
	return a
}

// scaleBars fills Bar.Pct relative to the largest count in the group (0..100).
func scaleBars(bars []models.Bar) {
	max := 0
	for _, b := range bars {
		if b.Count > max {
			max = b.Count
		}
	}
	if max == 0 {
		return
	}
	for i := range bars {
		bars[i].Pct = bars[i].Count * 100 / max
	}
}

// topBars returns the N highest-count entries, descending (ties broken by label).
func topBars(m map[string]int, n int) []models.Bar {
	bars := make([]models.Bar, 0, len(m))
	for k, v := range m {
		bars = append(bars, models.Bar{Label: k, Count: v})
	}
	sort.Slice(bars, func(i, j int) bool {
		if bars[i].Count != bars[j].Count {
			return bars[i].Count > bars[j].Count
		}
		return bars[i].Label < bars[j].Label
	})
	if len(bars) > n {
		bars = bars[:n]
	}
	return bars
}

// calendarMonth builds the rendered month grid for one client: the leading
// padding cells (Sunday-started weeks) + one cell per day, each carrying the
// events that fall on it. Returns the month title and the day cells.
func calendarMonth(c *compute.Client, courtDates []models.CourtDate, track time.Time, year int, month time.Month) (string, []models.CalDay) {
	first := compute.Noon(year, month, 1)
	title := first.Format("January 2006")
	daysIn := first.AddDate(0, 1, -1).Day()

	// Bucket this client's events by day-of-month within the rendered month.
	byDay := map[int][]models.CalEvent{}
	for _, ev := range compute.GetEventsForClient(*c, track) {
		if ev.Date.Year() == year && ev.Date.Month() == month {
			d := ev.Date.Day()
			byDay[d] = append(byDay[d], models.CalEvent{Day: d, Kind: ev.Kind, Label: ev.Label})
		}
	}
	// Court dates live in the app extension table, not the computed Client, so
	// merge them in here. Plot the scheduled date and, when a hearing was logged
	// with a reschedule, the next date too (matching the case-summary logic).
	for _, cd := range courtDates {
		addCourtCell(byDay, cd.CourtDate, courtLabel(cd.Court, false), year, month)
		addCourtCell(byDay, cd.NextDate, courtLabel(cd.Court, true), year, month)
	}
	var days []models.CalDay
	for i := 0; i < int(first.Weekday()); i++ { // Sunday=0 leading pad
		days = append(days, models.CalDay{Day: 0})
	}
	for d := 1; d <= daysIn; d++ {
		days = append(days, models.CalDay{Day: d, Events: byDay[d]})
	}
	return title, days
}

// addCourtCell appends a "court" event for dateStr to the right day bucket when
// it parses and falls inside the rendered month.
func addCourtCell(byDay map[int][]models.CalEvent, dateStr, label string, year int, month time.Month) {
	dt, ok := compute.ParseDay(dateStr)
	if !ok || dt.Year() != year || dt.Month() != month {
		return
	}
	d := dt.Day()
	byDay[d] = append(byDay[d], models.CalEvent{Day: d, Kind: "court", Label: label})
}

// courtLabel builds the calendar label for a court appearance, naming the court
// when known and flagging a rescheduled (post-outcome) date.
func courtLabel(court string, rescheduled bool) string {
	label := "Court"
	if rescheduled {
		label = "Court (rescheduled)"
	}
	if c := strings.TrimSpace(court); c != "" {
		label += " — " + c
	}
	return label
}

// countCourtDates returns how many court appearances (scheduled + any logged
// reschedule) fall on each day-of-month within the rendered month, for the
// roster aggregate.
func countCourtDates(courtDates []models.CourtDate, year int, month time.Month) map[int]int {
	out := map[int]int{}
	bump := func(dateStr string) {
		if dt, ok := compute.ParseDay(dateStr); ok && dt.Year() == year && dt.Month() == month {
			out[dt.Day()]++
		}
	}
	for _, cd := range courtDates {
		bump(cd.CourtDate)
		bump(cd.NextDate)
	}
	return out
}

// rosterCalendarMonth aggregates events across ALL clients into per-day counts
// for the roster-mode (team standup) calendar — Brief 2.9's second calendar mode.
// One representative per IDN (open-preferred) is used so a multi-case person's
// shared check-ins/payments aren't double-counted. Categories: check-ins,
// payments (GPS + PTR), court appearances, missed windows, and upcoming due
// windows. courtByIDN supplies the app-entered court dates per client.
func rosterCalendarMonth(clients map[string][]*compute.Client, courtByIDN map[string][]models.CourtDate, track time.Time, year int, month time.Month) models.RosterCalendar {
	first := compute.Noon(year, month, 1)
	daysIn := first.AddDate(0, 1, -1).Day()
	byDay := map[int]*models.RosterDay{}
	rc := models.RosterCalendar{Title: first.Format("January 2006")}
	cell := func(d int) *models.RosterDay {
		rd := byDay[d]
		if rd == nil {
			rd = &models.RosterDay{Day: d}
			byDay[d] = rd
		}
		return rd
	}

	floor := compute.CheckInDataFloor()
	for idn, cases := range clients {
		c := openRep(cases)
		if c == nil {
			continue
		}
		hasRec := hasCheckInRecord(c) // gate "missed" marks the same way the rosters do
		for _, ev := range compute.GetEventsForClient(*c, track) {
			if ev.Date.Year() != year || ev.Date.Month() != month {
				continue
			}
			rd := cell(ev.Date.Day())
			switch {
			case strings.HasPrefix(ev.Kind, "checkin"):
				rd.CheckIns++
				rc.TotCheckIns++
			case ev.Kind == "payment" || ev.Kind == "ptr-fee":
				rd.Payments++
				rc.TotPayments++
			case ev.Kind == "missed":
				// Don't mark missed for clients with no digital check-in history,
				// or for windows predating digital capture (CheckInDataFloor).
				if !hasRec || ev.Date.Before(floor) {
					continue
				}
				rd.Missed++
				rc.TotMissed++
			case ev.Kind == "due":
				rd.Due++
				rc.TotDue++
			}
		}
		// Court appearances for this client (scheduled + any reschedule).
		for d, n := range countCourtDates(courtByIDN[idn], year, month) {
			cell(d).Court += n
			rc.TotCourt += n
		}
	}

	var days []models.RosterDay
	for i := 0; i < int(first.Weekday()); i++ { // Sunday=0 leading pad
		days = append(days, models.RosterDay{Day: 0})
	}
	for d := 1; d <= daysIn; d++ {
		if rd := byDay[d]; rd != nil {
			days = append(days, *rd)
		} else {
			days = append(days, models.RosterDay{Day: d})
		}
	}
	rc.Days = days

	// Week rows (with week totals) + per-weekday column totals — the
	// "roster-calendar weekly/column totals" nice-to-have. Same numbers as the
	// day cells, just re-aggregated; the grand totals above stay authoritative.
	// rc.Days stays exactly leading-pad + month days (JSON/test contract); only
	// this week grouping gets trailing padding so every row has 7 cells.
	padded := days
	for len(padded)%7 != 0 {
		padded = append(padded, models.RosterDay{Day: 0})
	}
	rc.ColTotals = make([]models.RosterTotals, 7)
	for w := 0; w < len(padded); w += 7 {
		week := models.RosterWeek{Days: padded[w : w+7]}
		for i, rd := range week.Days {
			week.Tot.CheckIns += rd.CheckIns
			week.Tot.Payments += rd.Payments
			week.Tot.Court += rd.Court
			week.Tot.Missed += rd.Missed
			week.Tot.Due += rd.Due
			rc.ColTotals[i].CheckIns += rd.CheckIns
			rc.ColTotals[i].Payments += rd.Payments
			rc.ColTotals[i].Court += rd.Court
			rc.ColTotals[i].Missed += rd.Missed
			rc.ColTotals[i].Due += rd.Due
		}
		rc.Weeks = append(rc.Weeks, week)
	}
	rc.Month = models.RosterTotals{
		CheckIns: rc.TotCheckIns, Payments: rc.TotPayments,
		Court: rc.TotCourt, Missed: rc.TotMissed, Due: rc.TotDue,
	}
	return rc
}

// ViolationRow is one recorded violation resolved to its client, for the
// compliance page's violations roster. It mirrors the Behind/Missed rosters'
// client + officer + level shape, plus the violation's (display-formatted) date
// and a category/description detail.
type ViolationRow struct {
	IDN     string
	Name    string
	Officer string
	Level   int
	Date    string // display-formatted (shortStamp); a dash when undated
	Detail  string // category + description
}

// violationRoster resolves each recorded violation to its client and returns the
// list sorted newest-first (undated rows last, then by name). Callers pass
// violations already scoped to the stats epoch (violationsSinceEpoch) so the row
// count matches the dashboard's "Open Violations" KPI.
func violationRoster(clients map[string][]*compute.Client, violations []models.Violation) []ViolationRow {
	rows := make([]ViolationRow, 0, len(violations))
	for _, v := range violations {
		lvl := 0
		if c := openRep(clients[v.IDN]); c != nil {
			lvl, _ = compute.ParseLevel(c.Level)
		}
		detail := strings.TrimSpace(v.Category)
		if d := strings.TrimSpace(v.Description); d != "" {
			if detail != "" {
				detail += " — " + d
			} else {
				detail = d
			}
		}
		if detail == "" {
			detail = "Violation recorded"
		}
		rows = append(rows, ViolationRow{
			IDN:     v.IDN,
			Name:    nameFor(clients, v.IDN),
			Officer: officerForIDN(clients, v.IDN),
			Level:   lvl,
			Date:    v.ViolationDate, // raw for sorting; display-formatted below
			Detail:  clipText(detail, 120),
		})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		di, oki := compute.ParseDay(rows[i].Date)
		dj, okj := compute.ParseDay(rows[j].Date)
		if oki != okj {
			return oki // dated rows before undated
		}
		if oki && okj && !di.Equal(dj) {
			return di.After(dj) // newest first
		}
		return strings.ToUpper(rows[i].Name) < strings.ToUpper(rows[j].Name)
	})
	for i := range rows {
		rows[i].Date = shortStamp(rows[i].Date)
	}
	return rows
}

func sortByName(rows []models.RosterRow) {
	sort.Slice(rows, func(i, j int) bool {
		return strings.ToUpper(rows[i].Name) < strings.ToUpper(rows[j].Name)
	})
}

func ftoa(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }
func itoa(i int) string     { return strconv.Itoa(i) }
