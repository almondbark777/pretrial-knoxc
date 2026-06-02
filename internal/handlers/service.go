package handlers

import (
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

// behindRoster mirrors the HTML BehindRoster default view: one rep per IDN
// (open-preferred among the IDN's GPS-active cases), GPS surplusDollars < 0, and
// the rep is open (the component defaults to its 'open' filter). Sorted by name.
func behindRoster(clients map[string][]*compute.Client, track time.Time) []models.RosterRow {
	var rows []models.RosterRow
	for _, cases := range clients {
		// Dedup among GPS-active cases (the canonical filters !gpsActive first).
		var gpsCases []*compute.Client
		for _, c := range cases {
			if c.GpsActive {
				gpsCases = append(gpsCases, c)
			}
		}
		c := openRep(gpsCases)
		if c == nil {
			continue
		}
		g := compute.ComputeGPS(*c, track, nil, "")
		if g.SurplusDollars == nil || *g.SurplusDollars >= 0 {
			continue
		}
		// Default 'open' filter: a closed-only IDN has a non-open rep -> excluded.
		if !reOpen.MatchString(c.Status) {
			continue
		}
		lvl, _ := compute.ParseLevel(c.Level)
		detail := "behind $" + ftoa(-*g.SurplusDollars)
		if g.SurplusDays != nil {
			detail += " / " + itoa(-*g.SurplusDays) + " days"
		}
		owed := 0.0
		if g.TotalOwedDollars != nil {
			owed = *g.TotalOwedDollars
		}
		rows = append(rows, models.RosterRow{
			IDN: c.IDN, Name: c.Name, Officer: c.Officer, Level: lvl,
			Detail: detail, Amount: *g.SurplusDollars,
			Owed: owed, Paid: g.TotalGpsPaid, Waived: compute.IsFeesWaived(c.GpNotes),
		})
	}
	sortByName(rows)
	return rows
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
	for _, cases := range clients {
		c := openRep(cases)
		if c == nil || !reOpen.MatchString(c.Status) {
			continue // open cases only
		}
		lvl, _ := compute.ParseLevel(c.Level)
		if lvl == 1 {
			continue // L1 excluded from the monthly roster (v0.74)
		}
		if !c.RefOK {
			continue
		}
		// Both an in-person AND a phone check-in are required this calendar month
		// (office policy: clients must do both at their level's cadence). A phone
		// call alone no longer counts as "checked in".
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
		if hasIP && hasPh {
			continue
		}
		// 3-day grace: if still inside grace and grace ends at/after month start, skip.
		graceEnd := c.RefD.AddDate(0, 0, 3)
		if !graceEnd.Before(track) && !graceEnd.Before(monthStart) {
			continue
		}
		mo := track.Format("January 2006")
		detail := "no in-person or phone check-in in " + mo
		switch {
		case hasIP && !hasPh:
			detail = "no phone check-in in " + mo
		case !hasIP && hasPh:
			detail = "no in-person check-in in " + mo
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
// per-client compute: distinct IDNs, open/closed by status, and GPS-active (any
// case). BehindGPS/MissedMonth are left zero — those require full roster passes,
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
		for _, cc := range cases { // GPS-active if ANY case is
			if cc.GpsActive {
				s.GPSActive++
				break
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

// defendantRows builds one row per IDN (open-preferred rep) with computed
// compliance, for the case-management grid + /api/defendants. Behind/Missed
// flags are taken from the SAME roster functions (no divergence).
func defendantRows(clients map[string][]*compute.Client, track time.Time) []models.DefendantRow {
	behind := map[string]bool{}
	for _, r := range behindRoster(clients, track) {
		behind[r.IDN] = true
	}
	missed := map[string]bool{}
	for _, r := range missedCheckInsRoster(clients, track) {
		missed[r.IDN] = true
	}
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
			PTRBalance: ptr.Balance, MissedCount: len(ci.Missed), MissedMonth: missed[idn],
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
func analyticsData(clients map[string][]*compute.Client, track time.Time) models.Analytics {
	a := models.Analytics{Stats: computeStats(clients, track)}
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
func calendarMonth(c *compute.Client, track time.Time, year int, month time.Month) (string, []models.CalDay) {
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
	var days []models.CalDay
	for i := 0; i < int(first.Weekday()); i++ { // Sunday=0 leading pad
		days = append(days, models.CalDay{Day: 0})
	}
	for d := 1; d <= daysIn; d++ {
		days = append(days, models.CalDay{Day: d, Events: byDay[d]})
	}
	return title, days
}

// myDay builds the logged-in officer's personal worklist: among the clients THEY
// supervise (open rep's Officer == their display name), the ones behind on GPS,
// who missed a check-in this month, and whose next check-in window falls due
// within 7 days. Reuses the roster fns (filtered by officer) so there's no
// divergence. A user who supervises no one (e.g. an admin) gets an empty list.
func myDay(clients map[string][]*compute.Client, track time.Time, officer string) models.MyDay {
	md := models.MyDay{Officer: officer}
	officerLC := strings.ToLower(strings.TrimSpace(officer))
	mine := map[string]bool{}
	for _, cases := range clients {
		c := openRep(cases)
		if c != nil && strings.ToLower(strings.TrimSpace(c.Officer)) == officerLC && officerLC != "" {
			mine[c.IDN] = true
			md.Caseload++
		}
	}
	for _, x := range behindRoster(clients, track) {
		if mine[x.IDN] {
			md.Behind = append(md.Behind, x)
		}
	}
	for _, x := range missedCheckInsRoster(clients, track) {
		if mine[x.IDN] {
			md.Missed = append(md.Missed, x)
		}
	}
	weekEnd := track.AddDate(0, 0, 7)
	for _, cases := range clients {
		c := openRep(cases)
		if c == nil || !mine[c.IDN] {
			continue
		}
		ci := compute.ComputeCheckIns(*c, track)
		if ci.NextDue != nil && !ci.NextDue.Deadline.After(weekEnd) {
			lvl, _ := compute.ParseLevel(c.Level)
			md.DueSoon = append(md.DueSoon, models.RosterRow{
				IDN: c.IDN, Name: c.Name, Officer: c.Officer, Level: lvl,
				Detail: "due " + ci.NextDue.Deadline.Format("Mon Jan 2") + " · " + ci.NextDue.Label,
			})
		}
	}
	sortByName(md.DueSoon)
	sortByName(md.Behind)
	sortByName(md.Missed)
	return md
}

// rosterCalendarMonth aggregates events across ALL clients into per-day counts
// for the roster-mode (team standup) calendar — Brief 2.9's second calendar mode.
// One representative per IDN (open-preferred) is used so a multi-case person's
// shared check-ins/payments aren't double-counted. Categories: check-ins,
// payments (GPS + PTR), missed windows, and upcoming due windows.
func rosterCalendarMonth(clients map[string][]*compute.Client, track time.Time, year int, month time.Month) models.RosterCalendar {
	first := compute.Noon(year, month, 1)
	daysIn := first.AddDate(0, 1, -1).Day()
	byDay := map[int]*models.RosterDay{}
	rc := models.RosterCalendar{Title: first.Format("January 2006")}

	for _, cases := range clients {
		c := openRep(cases)
		if c == nil {
			continue
		}
		for _, ev := range compute.GetEventsForClient(*c, track) {
			if ev.Date.Year() != year || ev.Date.Month() != month {
				continue
			}
			d := ev.Date.Day()
			rd := byDay[d]
			if rd == nil {
				rd = &models.RosterDay{Day: d}
				byDay[d] = rd
			}
			switch {
			case strings.HasPrefix(ev.Kind, "checkin"):
				rd.CheckIns++
				rc.TotCheckIns++
			case ev.Kind == "payment" || ev.Kind == "ptr-fee":
				rd.Payments++
				rc.TotPayments++
			case ev.Kind == "missed":
				rd.Missed++
				rc.TotMissed++
			case ev.Kind == "due":
				rd.Due++
				rc.TotDue++
			}
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
	return rc
}

func sortByName(rows []models.RosterRow) {
	sort.Slice(rows, func(i, j int) bool {
		return strings.ToUpper(rows[i].Name) < strings.ToUpper(rows[j].Name)
	})
}

func ftoa(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }
func itoa(i int) string     { return strconv.Itoa(i) }
