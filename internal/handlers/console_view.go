package handlers

// console_view.go builds the view-models for the professional case console
// (mounted at /console). It is a pure presentation layer: every number comes from
// the same server-side math the tracker uses (computeStats, behindRoster,
// missedCheckInsRoster, ComputeCheckIns/PTRFees/GPS, GetEventsForClient).
// Nothing here reimplements a business rule — it only shapes those outputs into
// cards, chips, timelines, and tabs.

import (
	"encoding/json"
	"html/template"
	"sort"
	"strconv"
	"strings"
	"time"

	"pretrial-knoxc/internal/compute"
	"pretrial-knoxc/internal/models"
)

// pct formats num/den as a one-decimal percentage string ("—" when den<=0).
func pct(num, den float64) string {
	if den <= 0 {
		return "—"
	}
	return strconv.FormatFloat(num/den*100, 'f', 1, 64) + "%"
}

// ── status chip primitive (Wong palette + icon + text, never hue alone) ───────

// Chip is the core status primitive: a tone (maps to a Wong-safe color class), a
// glyph (✓ satisfied · ⚠ missed/violation · ◯ upcoming/none), and a text label.
// Meaning is always carried by the label + icon, not color alone (Brief 2.9).
type Chip struct {
	Tone  string // risk | warn | ok | info | neutral
	Icon  string // ✓ ⚠ ◯ · (or "")
	Label string
}

func levelChip(level int) Chip {
	switch level {
	case 1:
		return Chip{Tone: "ok", Label: "Level 1"}
	case 2:
		return Chip{Tone: "warn", Label: "Level 2"}
	case 3:
		return Chip{Tone: "risk", Label: "Level 3"}
	default:
		return Chip{Tone: "neutral", Icon: "◯", Label: "Level —"}
	}
}

func statusChip(status string) Chip {
	s := strings.ToLower(strings.TrimSpace(status))
	switch {
	case s == "":
		return Chip{Tone: "neutral", Label: "Unknown"}
	case reOpen.MatchString(s):
		return Chip{Tone: "info", Label: "Active"}
	case strings.HasPrefix(s, "closed"):
		return Chip{Tone: "neutral", Label: "Closed"}
	default:
		return Chip{Tone: "neutral", Label: titleCase(status)}
	}
}

// complianceChip summarizes a client's standing for the roster + record.
func complianceChip(behind, missedMonth bool, missedCount int, refOK bool) Chip {
	switch {
	case !refOK:
		return Chip{Tone: "neutral", Icon: "◯", Label: "No referral"}
	case behind:
		return Chip{Tone: "risk", Icon: "⚠", Label: "Behind on GPS"}
	case missedMonth:
		return Chip{Tone: "risk", Icon: "⚠", Label: "Missed check-in"}
	case missedCount > 0:
		return Chip{Tone: "warn", Icon: "⚠", Label: "Past missed"}
	default:
		return Chip{Tone: "ok", Icon: "✓", Label: "Compliant"}
	}
}

// ── Dashboard ("My Caseload") ─────────────────────────────────────────────────

// ConsoleKPIs are the five headline cards. Backed by the same roster math as the
// existing dashboard, so the numbers agree.
type ConsoleKPIs struct {
	ActiveClients   int
	DueToday        int
	CourtThisWeek   int
	OpenViolations  int
	OverdueCheckIns int // missed a required check-in this month (Stats.MissedMonth)
}

// ConsoleAlert is one row in the alert feed.
type ConsoleAlert struct {
	IDN     string
	Name    string
	Context string
	Chip    Chip
	Icon    string // severity glyph on the rail
	Mine    bool   // belongs to the signed-in officer's caseload
	sev     int    // sort key (higher = more urgent)
}

// ConsoleSched is one row in today's schedule.
type ConsoleSched struct {
	IDN   string
	Time  string
	Title string
	Sub   string
	Chip  Chip
	Mine  bool
}

// ConsoleDashboard is the whole "My Caseload" view-model.
type ConsoleDashboard struct {
	AsOf     string
	KPIs     ConsoleKPIs
	Alerts   []ConsoleAlert
	Schedule []ConsoleSched
}

// consoleDashboard assembles the dashboard. It leans on the existing roster
// functions (so Behind/Missed counts match the tracker exactly) and does one
// extra O(n) pass for "due today" / "next court". courtDates + violations are the
// app's extension data (may be empty on a fresh DB — that's a real zero).
func consoleDashboard(clients map[string][]*compute.Client, track time.Time,
	courtDates []models.CourtDate, violations []models.Violation,
	scheds []models.ScheduledCheckIn, officer string) ConsoleDashboard {

	d := ConsoleDashboard{AsOf: track.Format("Monday, January 2, 2006")}
	officerLC := strings.ToLower(strings.TrimSpace(officer))
	mine := func(o string) bool {
		return officerLC != "" && strings.ToLower(strings.TrimSpace(o)) == officerLC
	}

	// Compute the two heavy rosters ONCE and reuse them for both the KPI counts
	// and the alert feed (they were previously computed twice per dashboard load:
	// once inside computeStats, once for the alerts). rosterStateCounts covers the
	// cheap state tallies without another roster pass.
	behind := behindRoster(clients, track)
	missed := missedCheckInsRoster(clients, track)
	d.KPIs.ActiveClients = rosterStateCounts(clients).Open
	d.KPIs.OverdueCheckIns = len(missed)

	// Due today: a client whose next required check-in window's deadline is today.
	for _, cases := range clients {
		c := openRep(cases)
		if c == nil || !reOpen.MatchString(c.Status) {
			continue
		}
		ci := compute.ComputeCheckIns(*c, track)
		if ci.NextDue != nil && sameDay(ci.NextDue.Deadline, track) {
			d.KPIs.DueToday++
			d.Schedule = append(d.Schedule, ConsoleSched{
				IDN: c.IDN, Time: "Check-in", Title: c.Name,
				Sub:  "Check-in due today · " + ci.NextDue.Label,
				Chip: Chip{Tone: "warn", Icon: "◯", Label: "Due"}, Mine: mine(c.Officer),
			})
		}
	}

	// Court dates this week / today (from the extension table). "This week" is the
	// rolling 7-day window [track, track+6].
	weekEnd := track.AddDate(0, 0, 6)
	for _, cd := range courtDates {
		dt, ok := compute.ParseDay(cd.CourtDate)
		if !ok {
			continue
		}
		if !dt.Before(track) && !dt.After(weekEnd) {
			d.KPIs.CourtThisWeek++
		}
		if sameDay(dt, track) {
			d.Schedule = append(d.Schedule, ConsoleSched{
				IDN: cd.IDN, Time: "Court", Title: nameFor(clients, cd.IDN),
				Sub:  "Court appearance" + appendIf(" · ", cd.Court),
				Chip: Chip{Tone: "info", Icon: "·", Label: "Court"},
				// Attribute to the client's supervising officer so the court
				// appearance shows up under "My caseload" (not hidden as Mine=false).
				Mine: mine(officerForIDN(clients, cd.IDN)),
			})
		}
	}

	// Booked check-in appointments falling due today (scheduled_check_ins).
	// Distinct from the computed "Check-in due today" rows above: those are
	// compliance-window deadlines, these are appointments an officer made.
	for _, sc := range scheds {
		dt, ok := compute.ParseDay(sc.For)
		if !ok || !sameDay(dt, track) {
			continue
		}
		d.Schedule = append(d.Schedule, ConsoleSched{
			IDN: sc.IDN, Time: "Check-in", Title: nameFor(clients, sc.IDN),
			Sub:  "Scheduled check-in" + appendIf(" · ", sc.Type),
			Chip: Chip{Tone: "info", Icon: "·", Label: "Scheduled"},
			// Attribute to the supervising officer so it survives "My caseload".
			Mine: mine(officerForIDN(clients, sc.IDN)),
		})
	}

	d.KPIs.OpenViolations = len(violations)

	// ── Alert feed ── behind-on-GPS + missed-check-in rosters drive it (real data),
	// plus any recorded violations. Sorted by severity, then name.
	for _, r := range behind {
		d.Alerts = append(d.Alerts, ConsoleAlert{
			IDN: r.IDN, Name: r.Name, Context: titleCase(r.Detail) + " · " + r.Officer,
			Chip: Chip{Tone: "risk", Icon: "⚠", Label: "Behind on GPS"}, Icon: "⚠",
			Mine: mine(r.Officer), sev: 30,
		})
	}
	for _, r := range missed {
		d.Alerts = append(d.Alerts, ConsoleAlert{
			IDN: r.IDN, Name: r.Name, Context: titleCase(r.Detail) + " · " + r.Officer,
			Chip: Chip{Tone: "risk", Icon: "⚠", Label: "Missed check-in"}, Icon: "⚠",
			Mine: mine(r.Officer), sev: 20,
		})
	}
	for _, v := range violations {
		nm := nameFor(clients, v.IDN)
		ctx := strings.TrimSpace(v.Category + " " + v.Description)
		if ctx == "" {
			ctx = "Violation recorded"
		}
		d.Alerts = append(d.Alerts, ConsoleAlert{
			IDN: v.IDN, Name: nm, Context: clipText(ctx, 90),
			Chip: Chip{Tone: "risk", Icon: "⚠", Label: "Violation"}, Icon: "⚠",
			Mine: mine(officerForIDN(clients, v.IDN)), // attribute to the caseload (survives "My caseload")
			sev:  40,
		})
	}
	sort.SliceStable(d.Alerts, func(i, j int) bool {
		if d.Alerts[i].sev != d.Alerts[j].sev {
			return d.Alerts[i].sev > d.Alerts[j].sev
		}
		return strings.ToUpper(d.Alerts[i].Name) < strings.ToUpper(d.Alerts[j].Name)
	})
	if len(d.Alerts) > 40 {
		d.Alerts = d.Alerts[:40]
	}
	sort.SliceStable(d.Schedule, func(i, j int) bool {
		return strings.ToUpper(d.Schedule[i].Title) < strings.ToUpper(d.Schedule[j].Title)
	})
	return d
}

// ── Clients roster table ──────────────────────────────────────────────────────

// ConsoleClientRow is one row of the filterable roster table.
type ConsoleClientRow struct {
	IDN             string
	Name            string
	Initials        string
	CaseNo          string
	Level           int
	LevelChip       Chip
	StatusChip      Chip
	Officer         string
	NextCourt       string
	NextCourtSort   string // ISO key so the column sorts chronologically
	NextCheckIn     string
	NextCheckInSort string // ISO key so the column sorts chronologically
	CheckInOverdue  bool
	Compliance      Chip
	GpsActive       bool
	// lowercase blobs for client-side filtering
	Search string
}

// blankDateSort is the ISO sort key for a missing date ("—"): a far-future
// sentinel so rows with no date sort to the bottom when ascending.
const blankDateSort = "9999-12-31"

// consoleClientRows turns the shared defendantRows() output into rich rows with
// chips + next-court/next-check-in, reusing the canonical compute per rep.
func consoleClientRows(clients map[string][]*compute.Client, track time.Time, courtByIDN map[string]courtCell) []ConsoleClientRow {
	// The roster needs only the Behind/Missed flags + next check-in — NOT the GPS
	// vendor/surplus or PTR balance that defendantRows also computes. So build rows
	// directly: behind/missed once each, then a single ComputeCheckIns per client
	// (defendantRows would compute GPS + PTR + a second ComputeCheckIns per client,
	// all discarded here — wasteful on a ~3,300-row caseload).
	behind, missed := behindMissedSets(clients, track)

	rows := make([]ConsoleClientRow, 0, len(clients))
	for idn, cases := range clients {
		c := openRep(cases)
		if c == nil {
			continue
		}
		lvl, _ := compute.ParseLevel(c.Level)
		ci := compute.ComputeCheckIns(*c, track)
		nextCI, nextCISort := "—", blankDateSort
		overdue := false
		if ci.NextDue != nil {
			nextCI = ci.NextDue.Deadline.Format("Jan 2")
			nextCISort = ci.NextDue.Deadline.Format("2006-01-02")
			overdue = ci.NextDue.Deadline.Before(track)
		}
		nextCourt, nextCourtSort := courtByIDN[idn].Display, courtByIDN[idn].Sort
		if nextCourt == "" {
			nextCourt, nextCourtSort = "—", blankDateSort
		}
		row := ConsoleClientRow{
			IDN: idn, Name: c.Name, Initials: Initials(c.Name), CaseNo: dash(c.CaseNo),
			Level: lvl, LevelChip: levelChip(lvl), StatusChip: statusChip(c.Status),
			Officer: dash(c.Officer), NextCourt: nextCourt, NextCourtSort: nextCourtSort,
			NextCheckIn: nextCI, NextCheckInSort: nextCISort,
			CheckInOverdue: overdue,
			Compliance:     complianceChip(behind[idn], missed[idn], len(reportedMissed(c, ci)), true),
			GpsActive:      c.GpsActive,
		}
		row.Search = strings.ToLower(c.Name + " " + idn + " " + c.CaseNo + " " + c.Officer)
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		return strings.ToUpper(rows[i].Name) < strings.ToUpper(rows[j].Name)
	})
	return rows
}

// rosterJSONRow is the compact, short-keyed encoding of one roster row for
// client-side windowing. Keeping keys short trims the payload across thousands of
// rows; chips are rebuilt in JS from L/St/Cmp so no markup is duplicated here.
type rosterJSONRow struct {
	I   string `json:"i"`   // idn
	N   string `json:"n"`   // name
	A   string `json:"a"`   // initials (avatar)
	C   string `json:"c"`   // case no
	L   int    `json:"l"`   // level
	St  string `json:"st"`  // status chip label
	Nc  string `json:"nc"`  // next court (display)
	Ncs string `json:"ncs"` // next court (ISO sort key)
	Ci  string `json:"ci"`  // next check-in (display)
	Cis string `json:"cis"` // next check-in (ISO sort key)
	Ov  bool   `json:"ov"`  // check-in overdue
	Cm  string `json:"cm"`  // compliance chip label
	G   bool   `json:"g"`   // gps active
	O   string `json:"o"`   // officer (display)
	S   string `json:"s"`   // lowercase search blob
}

// rosterRowsJSON marshals the roster as a compact JSON array embedded in the page
// for client-side filter/sort/paging. Only the visible page is rendered into the
// DOM (true windowing), so a multi-thousand-row roster stays light on low-end
// office PCs. Go's json.Marshal escapes <,>,& so the blob is safe inside <script>.
func rosterRowsJSON(rows []ConsoleClientRow) template.JS {
	out := make([]rosterJSONRow, len(rows))
	for i, r := range rows {
		out[i] = rosterJSONRow{
			I: r.IDN, N: r.Name, A: r.Initials, C: r.CaseNo, L: r.Level,
			St: r.StatusChip.Label, Nc: r.NextCourt, Ncs: r.NextCourtSort,
			Ci: r.NextCheckIn, Cis: r.NextCheckInSort, Ov: r.CheckInOverdue,
			Cm: r.Compliance.Label, G: r.GpsActive, O: r.Officer, S: r.Search,
		}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return template.JS("[]")
	}
	return template.JS(b)
}

// ── Client record ─────────────────────────────────────────────────────────────

// ConsoleField is one labelled cell in the Case Summary grid.
type ConsoleField struct {
	K       string
	V       string
	Missing bool
	Tone    string // optional value tone (e.g. "risk" for an overdue date)
}

// ConsoleCondition is one supervision condition with its own status chip.
type ConsoleCondition struct {
	Name   string
	Detail string
	Chip   Chip
}

// ConsoleTLItem is one entry on a reverse-chron timeline.
type ConsoleTLItem struct {
	Date   string
	Title  string
	Detail string
	Tone   string
	Icon   string
}

// ConsoleCourtRow is one court event row. Outcome is logged after the hearing
// (§5.6); HasOutcome gates the "Log outcome" action.
type ConsoleCourtRow struct {
	ID         int64
	Event      string
	Date       string
	Notes      string
	Outcome    Chip
	HasOutcome bool
	NextDate   string
	Reminder   Chip
}

// ConsolePTRMonth is one $20 PTR-fee month row.
type ConsolePTRMonth struct {
	Label  string
	Amount int
}

// ConsoleLoggedCI is one app-entered check-in shown on the record with its
// optional per-check-in note (e.g. GPS fitment details). ID drives the
// per-row remove form (only app-entered rows are removable — raw imported
// check-ins have no row here).
type ConsoleLoggedCI struct {
	ID     int64
	Date   string
	Type   string
	Note   string
	Author string
}

// ConsoleLoggedPayment is one app-entered payment shown on the record so an
// officer can confirm (and remove) what they just recorded.
type ConsoleLoggedPayment struct {
	ID     int64
	Date   string
	Type   string
	Amount string
	Case   string
	Author string
}

// ConsoleDrugScreen is one drug-screen log row on the record's Drug Screens tab.
type ConsoleDrugScreen struct {
	ID         int64
	Date       string
	Test       string
	Result     Chip
	Substances string
	Notes      string
	Author     string
}

// ConsoleViolationRow is one recorded violation listed on the record's
// Conditions tab. ID drives the per-row remove form (violations are app-entered
// extension rows, so every one is removable by the officer who can record one).
type ConsoleViolationRow struct {
	ID          int64
	Date        string
	Category    string
	Severity    Chip
	Description string
	ActionTaken string
	Author      string
}

// ConsoleReminderRow is one logged court reminder on the record's Court tab
// (v1 reminders are log-only — recorded, marked "not sent"). ID drives the
// per-row remove form.
type ConsoleReminderRow struct {
	ID     int64
	Logged string
	Due    string // "" when no due date was set
	Body   string
	Author string
}

// ConsoleRecord is the full client-record view-model.
type ConsoleRecord struct {
	IDN        string
	Name       string
	Initials   string
	CaseNo     string
	Cases      []string
	DOB        string
	Officer    string
	LevelChip  Chip
	StatusChip Chip
	Badges     []Chip
	Tags       []models.Tag
	Closed     bool
	ClosedDate string
	Missing    []string

	Summary        []ConsoleField
	Conditions     []ConsoleCondition
	CheckIns       []ConsoleTLItem
	Court          []ConsoleCourtRow
	LoggedCheckIns []ConsoleLoggedCI
	Scheduled      []ConsoleSchedCI
	LoggedPayments []ConsoleLoggedPayment
	DrugScreens    []ConsoleDrugScreen
	Violations     []ConsoleViolationRow
	Reminders      []ConsoleReminderRow
	PTRMonths      []ConsolePTRMonth
	PTR            compute.PTRResult
	GPS            compute.GPSResult
	GpsWaived      bool
	Notes          []models.Note
	Activity       []ConsoleTLItem

	CI   compute.CheckInResult
	AsOf string
}

// ConsoleSchedCI is one booked check-in appointment on the record's Check-ins
// tab. Done/Missed are derived at read time: Done when a real check-in exists
// on the booked day, Missed when the day has passed without one.
type ConsoleSchedCI struct {
	ID     int64
	Date   string
	Type   string
	By     string
	Done   bool
	Missed bool
}

func consoleRecord(c *compute.Client, allCases []*compute.Client, track time.Time,
	ci compute.CheckInResult, ptr compute.PTRResult, gps compute.GPSResult,
	extras models.DefendantExtras) ConsoleRecord {

	level, _ := compute.ParseLevel(c.Level)
	rec := ConsoleRecord{
		IDN: c.IDN, Name: c.Name, Initials: Initials(c.Name), CaseNo: dash(c.CaseNo),
		Cases: caseOptions(allCases), DOB: dash(c.Birthdate), Officer: dash(c.Officer),
		LevelChip: levelChip(level), StatusChip: statusChip(c.Status),
		GpsWaived: compute.IsFeesWaived(c.GpNotes), CI: ci, PTR: ptr, GPS: gps,
		Notes: extras.Notes, Tags: extras.Tags, AsOf: track.Format("January 2, 2006"),
	}
	if c.ClosedOK {
		rec.Closed = true
		rec.ClosedDate = c.ClosedD.Format("Jan 2, 2006")
	}

	// Badges: GPS condition + any open violation.
	if c.GpsActive {
		rec.Badges = append(rec.Badges, Chip{Tone: "info", Label: "GPS Monitored"})
	}
	if len(extras.Violations) > 0 {
		rec.Badges = append(rec.Badges, Chip{Tone: "risk", Icon: "⚠", Label: "Open Violation"})
	}

	// Missing-critical-info (Brief 2.7): Level, Referral date, and — when GPS-active —
	// vendor + install date.
	if level == 0 {
		rec.Missing = append(rec.Missing, "Pretrial Level")
	}
	if !c.RefOK {
		rec.Missing = append(rec.Missing, "Referral Date")
	}
	if c.GpsActive {
		if gps.Vendor == "" {
			rec.Missing = append(rec.Missing, "GPS Vendor")
		}
		if c.GpInstall == "" {
			rec.Missing = append(rec.Missing, "GPS Install Date")
		}
	}

	// Next court (soonest future) from the extension court dates.
	nextCourt := "—"
	for _, cd := range extras.CourtDates {
		if dt, ok := compute.ParseDay(cd.CourtDate); ok && !dt.Before(track) {
			nextCourt = dt.Format("Jan 2, 2006") + appendIf(" — ", cd.Court)
			break
		}
	}

	// Next check-in (with overdue tone).
	nextCI, nextTone := "—", ""
	if ci.NextDue != nil {
		nextCI = ci.NextDue.Deadline.Format("Jan 2, 2006") + " · " + ci.NextDue.Label
		if ci.NextDue.Deadline.Before(track) {
			nextCI += " (OVERDUE)"
			nextTone = "risk"
		}
	} else if ci.Error != "" {
		nextCI = ci.Error
	}

	// Last in-person / phone contact (officers track these separately — a string
	// of phone calls no longer hides a missing in-person visit).
	lastStr := func(t *time.Time) string {
		if t == nil {
			return "— none on record"
		}
		return t.Format("Jan 2, 2006")
	}

	// Most recent drug screen (ListDrugScreens is screen_date DESC).
	lastScreen, lastScreenTone := "— none on record", ""
	if len(extras.DrugScreens) > 0 {
		ds := extras.DrugScreens[0]
		chip := drugScreenChip(ds.Result)
		lastScreen = shortStamp(ds.ScreenDate) + " · " + chip.Label
		if chip.Tone == "risk" {
			lastScreenTone = "risk"
		}
	}

	// Case Summary grid.
	rec.Summary = []ConsoleField{
		{K: "Charges", V: dash(c.ChargeType)},
		{K: "Bond", V: dash(c.BondAmount)},
		{K: "Pretrial Level", V: levelLabel(level), Missing: level == 0},
		{K: "Supervision Type", V: dash(c.SupervisionType)},
		{K: "Referral Date", V: orDash(c.RefOK, c.RefD.Format("Jan 2, 2006")), Missing: !c.RefOK},
		{K: "Next Court Date", V: nextCourt},
		{K: "Next Check-in", V: nextCI, Tone: nextTone},
		{K: "Last In-Person", V: lastStr(ci.LastInPerson), Tone: toneIf(ci.LastInPerson == nil, "risk")},
		{K: "Last Phone", V: lastStr(ci.LastPhone)},
		{K: "Last Drug Screen", V: lastScreen, Tone: lastScreenTone},
		{K: "GPS Status", V: gpsSummary(c, gps), Missing: c.GpsActive && gps.Vendor == ""},
		{K: "PTR Fee Balance", V: ptrBalanceText(ptr), Tone: toneForBalance(ptr.Balance, ptr.Applies)},
		{K: "Order From", V: dash(c.OrderFrom)},
	}

	// Conditions (derived from the same math).
	rec.Conditions = consoleConditions(c, level, ci, ptr, gps)

	// Check-ins timeline (reverse-chron): the events of kind check-in/missed/due.
	for _, ev := range reversed(compute.GetEventsForClient(*c, track)) {
		if strings.HasPrefix(ev.Kind, "checkin") {
			rec.CheckIns = append(rec.CheckIns, ConsoleTLItem{
				Date: ev.Date.Format("Jan 2, 2006"), Title: ev.Label,
				Tone: "ok", Icon: "✓",
			})
		} else if ev.Kind == "missed" {
			rec.CheckIns = append(rec.CheckIns, ConsoleTLItem{
				Date: ev.Date.Format("Jan 2, 2006"), Title: ev.Label,
				Tone: "risk", Icon: "⚠",
			})
		} else if ev.Kind == "due" {
			rec.CheckIns = append(rec.CheckIns, ConsoleTLItem{
				Date: ev.Date.Format("Jan 2, 2006"), Title: ev.Label,
				Tone: "warn", Icon: "◯",
			})
		}
	}

	// App-logged check-ins with their per-check-in notes (fitment details, etc.).
	// Newest first (ListAddedCheckIns is add_id DESC).
	for _, a := range extras.AddedCheckIns {
		rec.LoggedCheckIns = append(rec.LoggedCheckIns, ConsoleLoggedCI{
			ID: a.ID, Date: shortStamp(a.Date), Type: dash(a.Type), Note: a.Note,
			Author: compute.FmtOfficer(a.Author),
		})
	}

	// Booked appointments (soonest first from ListScheduledCheckIns). Done =
	// a real check-in (raw or app-entered — both are in c.CheckIns) exists on
	// the booked day; Missed = the day passed without one.
	for _, sc := range extras.ScheduledCheckIns {
		row := ConsoleSchedCI{ID: sc.ID, Date: sc.For, Type: sc.Type, By: compute.FmtOfficer(sc.CreatedBy)}
		if dt, ok := compute.ParseDay(sc.For); ok {
			row.Date = dt.Format("Jan 2, 2006")
			for _, k := range c.CheckIns {
				if k.DOK && sameDay(k.D, dt) {
					row.Done = true
					break
				}
			}
			row.Missed = !row.Done && dt.Before(track) && !sameDay(dt, track)
		}
		rec.Scheduled = append(rec.Scheduled, row)
	}

	// Court tab.
	for _, cd := range extras.CourtDates {
		dt, ok := compute.ParseDay(cd.CourtDate)
		dateStr := cd.CourtDate
		outcome := Chip{Tone: "neutral", Icon: "◯", Label: "Upcoming"}
		if ok {
			dateStr = dt.Format("Jan 2, 2006")
			if dt.Before(track) {
				outcome = Chip{Tone: "neutral", Label: "Past"}
			}
		}
		if cd.Outcome != "" { // logged after the hearing
			outcome = Chip{Tone: "ok", Icon: "✓", Label: cd.Outcome}
		}
		nextDate := ""
		if nd, ok := compute.ParseDay(cd.NextDate); ok {
			nextDate = nd.Format("Jan 2, 2006")
		}
		rec.Court = append(rec.Court, ConsoleCourtRow{
			ID: cd.ID, Event: orDash(cd.Court != "", cd.Court), Date: dateStr,
			Notes: dash(cd.Notes), Outcome: outcome, HasOutcome: cd.Outcome != "", NextDate: nextDate,
			Reminder: Chip{Tone: "warn", Icon: "◯", Label: "Logged (not sent)"},
		})
	}

	// Recorded violations (newest first from ListViolations) — listed with a
	// per-row remove on the Conditions tab; the Activity merge below shows the
	// same rows in timeline form.
	for _, v := range extras.Violations {
		rec.Violations = append(rec.Violations, ConsoleViolationRow{
			ID: v.ID, Date: shortStamp(v.ViolationDate), Category: dash(v.Category),
			Severity: severityChip(v.Severity), Description: v.Description,
			ActionTaken: v.ActionTaken, Author: compute.FmtOfficer(v.Officer),
		})
	}

	// Logged reminders (ListReminders: incomplete first, then by due date) —
	// listed with a per-row remove on the Court tab next to the court dates
	// they're reminders for.
	for _, rm := range extras.Reminders {
		due := ""
		if strings.TrimSpace(rm.DueDate) != "" {
			due = shortStamp(rm.DueDate)
		}
		rec.Reminders = append(rec.Reminders, ConsoleReminderRow{
			ID: rm.ID, Logged: shortStamp(rm.CreatedAt), Due: due,
			Body: rm.Body, Author: compute.FmtOfficer(rm.CreatedBy),
		})
	}

	// App-entered payments (newest first; ListAddedPayments is add_id DESC) so an
	// officer can confirm/remove what they recorded. Imported payments stay in the
	// fee totals above, not here.
	for _, p := range extras.AddedPayments {
		rec.LoggedPayments = append(rec.LoggedPayments, ConsoleLoggedPayment{
			ID: p.ID, Date: shortStamp(p.PaymentDate), Type: dash(p.PaymentType),
			Amount: p.PaymentAmount, Case: p.CaseNumber, Author: compute.FmtOfficer(p.Author),
		})
	}

	// Payments / Fees — PTR months.
	for _, m := range ptr.MonthsOwed {
		rec.PTRMonths = append(rec.PTRMonths, ConsolePTRMonth{Label: m.Label, Amount: m.Amount})
	}

	// Drug-screen log (newest first from ListDrugScreens).
	for _, ds := range extras.DrugScreens {
		rec.DrugScreens = append(rec.DrugScreens, ConsoleDrugScreen{
			ID: ds.ID, Date: shortStamp(ds.ScreenDate), Test: dash(ds.TestType),
			Result: drugScreenChip(ds.Result), Substances: ds.Substances,
			Notes: ds.Notes, Author: compute.FmtOfficer(ds.Officer),
		})
	}

	// Activity timeline — every dated item (events incl. app-added check-ins /
	// payments, plus notes / violations / court dates), newest first.
	type act struct {
		t    time.Time
		item ConsoleTLItem
	}
	var acts []act
	for _, ev := range compute.GetEventsForClient(*c, track) {
		acts = append(acts, act{ev.Date, ConsoleTLItem{
			Date: ev.Date.Format("Jan 2, 2006"), Title: ev.Label,
			Tone: toneForKind(ev.Kind), Icon: iconForKind(ev.Kind)}})
	}
	for _, n := range extras.Notes {
		t, _ := compute.ParseDay(n.CreatedAt)
		acts = append(acts, act{t, ConsoleTLItem{
			Date: shortStamp(n.CreatedAt), Title: "Note — " + dash(compute.FmtOfficer(n.Author)),
			Detail: n.Body, Tone: "info", Icon: "·"}})
	}
	for _, v := range extras.Violations {
		t, _ := compute.ParseDay(v.ViolationDate)
		title := "Violation"
		if v.Category != "" {
			title += " — " + v.Category
		}
		acts = append(acts, act{t, ConsoleTLItem{
			Date: shortStamp(v.ViolationDate), Title: title,
			Detail: strings.TrimSpace(v.Severity + " " + v.Description), Tone: "risk", Icon: "⚠"}})
	}
	for _, cd := range extras.CourtDates {
		t, _ := compute.ParseDay(cd.CourtDate)
		acts = append(acts, act{t, ConsoleTLItem{
			Date: shortStamp(cd.CourtDate), Title: "Court date" + appendIf(" — ", cd.Court),
			Detail: cd.Notes, Tone: "info", Icon: "·"}})
	}
	for _, rm := range extras.Reminders {
		t, _ := compute.ParseDay(rm.CreatedAt)
		acts = append(acts, act{t, ConsoleTLItem{
			Date: shortStamp(rm.CreatedAt), Title: "Reminder logged (not sent)",
			Detail: rm.Body, Tone: "warn", Icon: "◯"}})
	}
	for _, ds := range extras.DrugScreens {
		t, _ := compute.ParseDay(ds.ScreenDate)
		chip := drugScreenChip(ds.Result)
		var parts []string
		for _, p := range []string{ds.TestType, ds.Substances, ds.Notes} {
			if strings.TrimSpace(p) != "" {
				parts = append(parts, strings.TrimSpace(p))
			}
		}
		acts = append(acts, act{t, ConsoleTLItem{
			Date: shortStamp(ds.ScreenDate), Title: "Drug screen — " + chip.Label,
			Detail: strings.Join(parts, " · "), Tone: chip.Tone, Icon: chip.Icon}})
	}
	sort.SliceStable(acts, func(i, j int) bool { return acts[i].t.After(acts[j].t) })
	for _, a := range acts {
		rec.Activity = append(rec.Activity, a.item)
	}
	return rec
}

// ViewChip is one saved-view chip on the roster page. URL is pre-built from
// the stored query — safe as template.URL because SaveView sanitizes to known
// filter keys and re-encodes via url.Values before storing.
type ViewChip struct {
	ID   int64
	Name string
	URL  template.URL
}

func viewChips(views []models.SavedView) []ViewChip {
	out := make([]ViewChip, 0, len(views))
	for _, v := range views {
		u := "/console/clients"
		if v.Query != "" {
			u += "?" + v.Query
		}
		out = append(out, ViewChip{ID: v.ID, Name: v.Name, URL: template.URL(u)})
	}
	return out
}

// PinnedRow is one entry in the dashboard's "Pinned clients" quick list.
type PinnedRow struct {
	IDN      string
	Name     string
	Initials string
	Detail   string
}

// pinnedRows resolves the user's pinned IDNs against the live client set,
// newest pin first. Pins whose client no longer exists (deleted/tombstoned)
// are skipped silently — the pin row stays in the table and simply reappears
// if the client is restored.
func pinnedRows(clients map[string][]*compute.Client, idns []string) []PinnedRow {
	var out []PinnedRow
	for _, idn := range idns {
		c := openRep(clients[idn])
		if c == nil {
			continue
		}
		var parts []string
		if lvl, _ := compute.ParseLevel(c.Level); lvl > 0 {
			parts = append(parts, "L"+itoa(lvl))
		}
		if strings.TrimSpace(c.Officer) != "" {
			parts = append(parts, c.Officer)
		}
		out = append(out, PinnedRow{
			IDN: c.IDN, Name: c.Name, Initials: Initials(c.Name),
			Detail: strings.Join(parts, " · "),
		})
	}
	return out
}

// drugScreenChip maps a screen result onto the shared chip tones: positive and
// refused read as risk, diluted as warn, negative as ok, anything else neutral.
func drugScreenChip(result string) Chip {
	switch strings.ToLower(strings.TrimSpace(result)) {
	case "positive":
		return Chip{Tone: "risk", Icon: "⚠", Label: "Positive"}
	case "refused":
		return Chip{Tone: "risk", Icon: "⚠", Label: "Refused"}
	case "diluted":
		return Chip{Tone: "warn", Icon: "◯", Label: "Diluted"}
	case "negative":
		return Chip{Tone: "ok", Icon: "✓", Label: "Negative"}
	case "pending":
		return Chip{Tone: "neutral", Icon: "◯", Label: "Pending"}
	case "":
		return Chip{Tone: "neutral", Label: "—"}
	default:
		return Chip{Tone: "neutral", Label: titleCase(result)}
	}
}

// severityChip maps a violation severity onto the shared chip tones: High reads
// as risk, Medium as warn, Low as info, anything else neutral.
func severityChip(sev string) Chip {
	switch strings.ToLower(strings.TrimSpace(sev)) {
	case "high":
		return Chip{Tone: "risk", Icon: "⚠", Label: "High"}
	case "medium":
		return Chip{Tone: "warn", Icon: "⚠", Label: "Medium"}
	case "low":
		return Chip{Tone: "info", Label: "Low"}
	case "":
		return Chip{Tone: "neutral", Label: "—"}
	default:
		return Chip{Tone: "neutral", Label: titleCase(sev)}
	}
}

func consoleConditions(c *compute.Client, level int, ci compute.CheckInResult,
	ptr compute.PTRResult, gps compute.GPSResult) []ConsoleCondition {
	var out []ConsoleCondition

	// 1) Check-in cadence — tracked as TWO conditions (in-person + phone), each
	// satisfied only by its own type, at the level's cadence.
	cadence := "Weekly"
	switch level {
	case 1:
		cadence = "Initial (3-day)"
	case 2:
		cadence = "Monthly"
	}
	// A type is "behind" if any closed window is missing that type.
	ipBehind, phBehind := false, false
	for _, w := range ci.Windows {
		if w.Missed {
			if !w.SatisfiedInPerson {
				ipBehind = true
			}
			if !w.SatisfiedPhone {
				phBehind = true
			}
		}
	}
	typeChip := func(behind bool) Chip {
		switch {
		case ci.Error != "":
			return Chip{Tone: "neutral", Icon: "◯", Label: "No referral"}
		case behind:
			return Chip{Tone: "risk", Icon: "⚠", Label: "Behind"}
		default:
			return Chip{Tone: "ok", Icon: "✓", Label: "Current"}
		}
	}
	lastDetail := func(t *time.Time) string {
		if t == nil {
			return "no visit on record"
		}
		return "last " + t.Format("Jan 2, 2006")
	}
	out = append(out,
		ConsoleCondition{Name: cadence + " in-person check-in", Detail: lastDetail(ci.LastInPerson), Chip: typeChip(ipBehind)},
		ConsoleCondition{Name: cadence + " phone check-in", Detail: lastDetail(ci.LastPhone), Chip: typeChip(phBehind)},
	)

	// 2) GPS electronic monitoring (only when active).
	if c.GpsActive {
		detail := "Electronic monitoring"
		if gps.Vendor != "" {
			detail = gps.Vendor + " monitoring"
			if gps.DailyRate != nil {
				detail += " · $" + itoa(*gps.DailyRate) + "/day"
			}
		}
		var chip Chip
		switch {
		case rec_isWaived(c):
			chip = Chip{Tone: "neutral", Label: "Waived"}
		case gps.Vendor == "":
			chip = Chip{Tone: "warn", Icon: "⚠", Label: "Vendor MISSING"}
		case gps.Covered != nil && !*gps.Covered:
			chip = Chip{Tone: "risk", Icon: "⚠", Label: "Behind"}
		default:
			chip = Chip{Tone: "ok", Icon: "✓", Label: "Active"}
		}
		out = append(out, ConsoleCondition{Name: "GPS electronic monitoring", Detail: detail, Chip: chip})
	}

	// 3) PTR supervision fee.
	if ptr.Applies {
		chip := Chip{Tone: "ok", Icon: "✓", Label: "Current"}
		if ptr.Balance < 0 {
			chip = Chip{Tone: "warn", Icon: "⚠", Label: "Owes $" + ftoa(-ptr.Balance)}
		}
		out = append(out, ConsoleCondition{Name: "PTR supervision fee", Detail: "$20 / month", Chip: chip})
	}
	return out
}

func rec_isWaived(c *compute.Client) bool { return compute.IsFeesWaived(c.GpNotes) }

// ── small formatting helpers (display only) ───────────────────────────────────

// Initials renders an avatar monogram from a display name ("Alex Bentley" → "AB").
// Exported so the template FuncMap (cmd/server) reuses the same logic.
func Initials(name string) string {
	parts := strings.Fields(strings.TrimSpace(name))
	if len(parts) == 0 {
		return "?"
	}
	out := string([]rune(parts[0])[:1])
	if len(parts) > 1 {
		last := parts[len(parts)-1]
		out += string([]rune(last)[:1])
	}
	return strings.ToUpper(out)
}

func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func orDash(ok bool, s string) string {
	if !ok || strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func appendIf(sep, s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	return sep + s
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = []rune(strings.ToUpper(string(r[0])))[0]
	return string(r)
}

func clipText(s string, n int) string {
	s = strings.TrimSpace(s)
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

func shortStamp(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 10 {
		if dt, ok := compute.ParseDay(s[:10]); ok {
			return dt.Format("Jan 2, 2006")
		}
	}
	return dash(s)
}

func sameDay(a, b time.Time) bool {
	return a.Year() == b.Year() && a.Month() == b.Month() && a.Day() == b.Day()
}

func reversed(ev []compute.Event) []compute.Event {
	out := make([]compute.Event, len(ev))
	for i, e := range ev {
		out[len(ev)-1-i] = e
	}
	return out
}

func nameFor(clients map[string][]*compute.Client, idn string) string {
	if c := openRep(clients[idn]); c != nil && c.Name != "" {
		return c.Name
	}
	return "IDN " + idn
}

// officerForIDN returns the supervising-officer display name for a client (open-
// preferred rep), or "" if unknown — used to attribute court appearances to a
// caseload.
func officerForIDN(clients map[string][]*compute.Client, idn string) string {
	if c := openRep(clients[idn]); c != nil {
		return c.Officer
	}
	return ""
}

// toneIf returns tone when cond is true, else "" (no tone).
func toneIf(cond bool, tone string) string {
	if cond {
		return tone
	}
	return ""
}

func toneForBalance(bal float64, applies bool) string {
	if !applies {
		return ""
	}
	if bal < 0 {
		return "warn"
	}
	return "ok"
}

func ptrBalanceText(ptr compute.PTRResult) string {
	if !ptr.Applies {
		return "No PTR fee"
	}
	if ptr.Balance < 0 {
		return "Owes $" + ftoa(-ptr.Balance)
	}
	if ptr.Balance > 0 {
		return "Paid ahead $" + ftoa(ptr.Balance)
	}
	return "Paid in full"
}

func gpsSummary(c *compute.Client, gps compute.GPSResult) string {
	if !c.GpsActive {
		return "Not monitored"
	}
	if gps.Vendor == "" {
		return "Active — vendor MISSING"
	}
	s := gps.Vendor
	if gps.Covered != nil {
		if *gps.Covered {
			s += " · current"
		} else if gps.SurplusDollars != nil {
			s += " · behind $" + ftoa(-*gps.SurplusDollars)
		}
	}
	return s
}

func toneForKind(kind string) string {
	switch {
	case kind == "missed":
		return "risk"
	case kind == "due", kind == "gps-install", kind == "gps-switch":
		return "warn"
	case strings.HasPrefix(kind, "checkin"), kind == "payment", kind == "ptr-fee":
		return "ok"
	case kind == "referral":
		return "info"
	default:
		return "neutral"
	}
}

func iconForKind(kind string) string {
	switch {
	case kind == "missed":
		return "⚠"
	case strings.HasPrefix(kind, "checkin"), kind == "payment", kind == "ptr-fee":
		return "✓"
	case kind == "due":
		return "◯"
	default:
		return "·"
	}
}
