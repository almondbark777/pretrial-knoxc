// console.go serves the professional case console at /console — the primary
// (and, since the classic interface was removed 2026-06-09, only) app UI.
//
// Every read view renders REAL data from the shared server-side math. Write
// actions POST to the shared CSRF-guarded /admin/* endpoints and come back here
// via `next`; a few not-yet-built actions remain demo-safe "coming soon" toasts.
package handlers

import (
	"database/sql"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"pretrial-knoxc/internal/auth"
	"pretrial-knoxc/internal/compute"
	"pretrial-knoxc/internal/db"
	"pretrial-knoxc/internal/models"
)

// trackFrom resolves the "as-of" date the whole console computes against. It
// reads the ptc_asof cookie (YYYY-MM-DD, set by the top-bar date control) and
// falls back to today EST — the same trackDate contract the tracker and every
// other view use (Build-Spec §3.3 / §8.2; no diverging version). The cached
// client set is as-of-independent (BuildClients ignores track), so changing the
// as-of only re-runs the compute layer, never a DB rebuild.
func (s *Server) trackFrom(r *http.Request) time.Time {
	if c, err := r.Cookie("ptc_asof"); err == nil {
		if d, ok := compute.ParseDay(c.Value); ok {
			return d
		}
	}
	return compute.TodayET()
}

// consoleBase returns the chrome keys every console page needs (user, role,
// active-nav highlight, as-of date). UserName is the display name for the
// "my caseload / all" scope toggle. AsOfInput/IsToday drive the top-bar as-of
// date control.
func (s *Server) consoleBase(w http.ResponseWriter, r *http.Request, active string, track time.Time) map[string]any {
	user := auth.User(r)
	data := map[string]any{
		"CSRF":         s.Auth.CSRF(w, r), // session token for the global "Report a problem" chrome form
		"User":         user,
		"UserName":     compute.FmtOfficer(user),
		"IsSupervisor": s.Auth.IsSupervisor(user),
		"IsAdmin":      s.Auth.IsAdmin(user),
		"ActiveNav":    active,
		"Today":        track.Format("January 2, 2006"),
		"AsOfInput":    track.Format("2006-01-02"),
		"IsToday":      sameDay(track, compute.TodayET()),
		"Epoch":        compute.StatsEpochLabel,  // go-live date for epoch-scoped stat labels
		"Msg":          r.URL.Query().Get("msg"), // flash toast after a write redirect
		"ChatRoster":   s.chatRoster(),           // every approved user for the chat presence list
		"MeEmail":      strings.ToLower(strings.TrimSpace(user)),
	}
	// Data freshness renders from the `dataFreshness` template func at both the
	// topbar and the sidebar foot (freshness.go is the one source of truth) — no
	// per-handler plumbing, and both stamps agree on wording + source.
	// Pending self-check-in count drives the nav badge on every console page, so
	// the approval queue is discoverable from anywhere. Cheap indexed COUNT;
	// errors degrade to no badge.
	if n, err := db.CountPendingCheckins(s.DB); err == nil {
		data["PendingCheckins"] = n
	}
	return data
}

// chatRosterEntry is one member in the chat presence list: display name + the
// lowercased email the SSE presence feed keys on.
type chatRosterEntry struct {
	Name  string
	Email string
}

// chatRoster returns every approved user (by display name + lowercased email),
// sorted by name — the full membership the chat panel renders with online dots.
func (s *Server) chatRoster() []chatRosterEntry {
	emails := s.Auth.AllowedEmails()
	out := make([]chatRosterEntry, 0, len(emails))
	seen := map[string]bool{}
	for _, e := range emails {
		le := strings.ToLower(strings.TrimSpace(e))
		if le == "" || seen[le] {
			continue
		}
		seen[le] = true
		out = append(out, chatRosterEntry{Name: compute.FmtOfficer(e), Email: le})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// renderConsole renders a console template, falling back to a graceful in-shell
// error page rather than a bare 500 (demo-safe: never a blank error screen).
func (s *Server) renderConsole(w http.ResponseWriter, name string, data map[string]any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.Tmpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), 500)
	}
}

// ── /console — dashboard ("My Caseload") ──────────────────────────────────────

func (s *Server) Console(w http.ResponseWriter, r *http.Request) {
	clients, err := s.clients()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	track := s.trackFrom(r)
	courtDates, _ := db.ListAllCourtDatesLive(s.DB)
	violations, _ := db.ListAllViolationsLive(s.DB)
	violations = violationsSinceEpoch(violations) // aggregate tallies count from go-live
	scheds, _ := db.ListAllScheduledCheckInsLive(s.DB)

	reopened, _ := db.ReopenedSince(s.DB, track.AddDate(0, 0, -2)) // recently reopened → new-referrals feed
	data := s.consoleBase(w, r, "dashboard", track)
	data["D"] = consoleDashboard(clients, track, courtDates, violations, scheds, compute.FmtOfficer(auth.User(r)), reopened)
	if pins, _ := db.PinnedIDNs(s.DB, auth.User(r)); len(pins) > 0 {
		data["Pinned"] = pinnedRows(clients, pins)
	}
	s.renderConsole(w, "console_dashboard.html", data)
}

// ── /console/clients — roster table ───────────────────────────────────────────

func (s *Server) ConsoleClients(w http.ResponseWriter, r *http.Request) {
	clients, err := s.clients()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	track := s.trackFrom(r)
	courtByIDN := nextCourtByIDN(s.DB, track)

	data := s.consoleBase(w, r, "clients", track)
	// consoleClientRows builds the behind/missed sets internally; reuse them to
	// assemble Stats without a second full-roster pass (#11 dedup).
	rows, behind, missed := consoleClientRows(clients, track, courtByIDN)
	// Decorate with manual client-flag severity (one query) so the roster shows a
	// flag chip next to flagged clients.
	if flagged := db.ActiveFlagSeverity(s.DB); len(flagged) > 0 {
		for i := range rows {
			rows[i].Flag = flagged[rows[i].IDN]
		}
	}
	data["RowsJSON"] = rosterRowsJSON(rows) // client-side windowing: only the visible page hits the DOM
	data["RowCount"] = len(rows)
	st := rosterStateCounts(clients)
	st.BehindGPS = len(behind)
	st.MissedMonth = len(missed)
	data["Stats"] = st
	data["Officers"] = distinctOfficers(clients) // officer filter: pick any officer's caseload
	// Initial filter state from URL params (shareable/bookmarkable — Build-Spec
	// §5.2). The client-side filter applies these on load; KPI/alert cards and
	// the compliance page deep-link here with these set.
	q := r.URL.Query()
	data["Fq"] = q.Get("q")
	data["Fstatus"] = q.Get("status")
	data["Flevel"] = q.Get("level")
	data["Fofficer"] = q.Get("officer")
	data["Fcomp"] = q.Get("comp")
	data["Fgps"] = q.Get("gps")
	data["Fdue"] = q.Get("due")      // "today" / "overdue" — the Due-Today KPI deep-links with this set
	data["CSRF"] = s.Auth.CSRF(w, r) // for the row quick-action (log check-in)
	if views, _ := db.ListSavedViews(s.DB, auth.User(r), "/console/clients"); len(views) > 0 {
		data["Views"] = viewChips(views)
	}
	s.renderConsole(w, "console_clients.html", data)
}

// ── /console/referrals — every client, most-recently-referred first ───────────

// ConsoleReferrals lists EVERY client (bulk SharePoint-imported + app-entered),
// sorted from the most recently referred to the oldest, as a compact, windowed
// worklist (only the visible page hits the DOM, so the full ~3,300-client list
// stays light on weak office PCs). The full per-client field dump is one click
// away via Export CSV (/export/referrals.csv).
func (s *Server) ConsoleReferrals(w http.ResponseWriter, r *http.Request) {
	clients, err := s.clients()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	track := s.trackFrom(r)
	rows := referralWorklist(clients)
	data := s.consoleBase(w, r, "referrals", track)
	data["RowsJSON"] = referralWorklistJSON(rows) // client-side windowing
	data["Count"] = len(rows)
	data["Officers"] = distinctOfficers(clients) // officer filter
	data["Fq"] = r.URL.Query().Get("q")          // shareable ?q= search seed
	s.renderConsole(w, "console_referrals.html", data)
}

// ── /console/help — quick reference ───────────────────────────────────────────

// ConsoleHelp renders the static in-app guide: the daily workflow, what the
// chips mean, the check-in cadence rules (including the both-types rule),
// fees/billing basics, roles, and keyboard shortcuts. Content lives entirely
// in templates/console_help.html.
func (s *Server) ConsoleHelp(w http.ResponseWriter, r *http.Request) {
	s.renderConsole(w, "console_help.html", s.consoleBase(w, r, "help", s.trackFrom(r)))
}

// ConsoleProblems lists submitted "Report a problem" feedback (supervisor only).
func (s *Server) ConsoleProblems(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireSupervisor(w, r); !ok {
		return
	}
	reports, _ := db.ListProblemReports(s.DB, 200)
	data := s.consoleBase(w, r, "admin", s.trackFrom(r))
	data["Problems"] = reports
	s.renderConsole(w, "console_problems.html", data)
}

// ── /console/clients/new — intake wizard (demo-safe) ──────────────────────────

// ConsoleIntake renders the multi-step new-client intake wizard (Build-Spec §5.4).
// This pass it is demo-safe: client-side step validation + draft, and submit
// confirms with a toast rather than writing (the app-is-system-of-record native
// create lands in a later milestone). Registered before /console/clients/{idn}
// so the static "new" segment wins over the idn param.
func (s *Server) ConsoleIntake(w http.ResponseWriter, r *http.Request) {
	clients, err := s.clients()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	track := s.trackFrom(r)
	data := s.consoleBase(w, r, "clients", track)
	data["Officers"] = s.officerChoices(clients)
	data["CSRF"] = s.Auth.CSRF(w, r) // final step creates a real client via /admin/add_defendant
	s.renderConsole(w, "console_intake.html", data)
}

// officerChoices returns the assignable supervising officers for the intake
// "assign" step: every staff member on the email allow-list (shown by name),
// unioned with any officer already supervising someone in the data. This lets a
// referral be assigned to a specific person by name even before that officer owns
// any open client — every authorized officer is pickable (one name per person).
func (s *Server) officerChoices(clients map[string][]*compute.Client) []string {
	// Dedup case-insensitively so a data variant ("Carla kidwell") doesn't show
	// alongside the allow-list's canonical casing ("Carla Kidwell"). Allow-list is
	// iterated first, so its properly-cased name wins.
	seen := map[string]bool{}
	var out []string
	add := func(name string) {
		key := strings.ToLower(strings.TrimSpace(name))
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, name)
	}
	for _, email := range s.Auth.AllowedEmails() {
		add(compute.FmtOfficer(email))
	}
	for _, name := range distinctOfficers(clients) {
		add(name)
	}
	sort.Strings(out)
	return out
}

// distinctOfficers returns the sorted set of supervising-officer display names
// across the open roster (for the intake "assign officer" step).
func distinctOfficers(clients map[string][]*compute.Client) []string {
	seen := map[string]bool{}
	var out []string
	for _, cases := range clients {
		c := openRep(cases)
		if c == nil || c.Officer == "" || seen[c.Officer] {
			continue
		}
		seen[c.Officer] = true
		out = append(out, c.Officer)
	}
	sort.Strings(out)
	return out
}

// ── /console/clients/{idn} — client record ────────────────────────────────────

func (s *Server) ConsoleRecordPage(w http.ResponseWriter, r *http.Request) {
	idn := strings.TrimSpace(chi.URLParam(r, "idn"))
	clients, err := s.clients()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	track := s.trackFrom(r)
	data := s.consoleBase(w, r, "clients", track)

	cases := clients[idn]
	if len(cases) == 0 {
		// Graceful in-shell "not found" — never a 404 in front of an audience.
		data["NotFound"] = idn
		s.renderConsole(w, "console_record.html", data)
		return
	}
	c, caseFilter := selectCase(cases, r.URL.Query().Get("case"))
	ci := compute.ComputeCheckIns(*c, track)
	ptr := compute.ComputePTRFees(*c, track, caseFilter)
	gps := compute.ComputeGPS(*c, track, nil, caseFilter)
	extras, err := db.LoadExtras(s.DB, idn)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	ledger, err := db.ClientLedger(s.DB, idn) // full imported + app check-in/payment history
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	extraDates, _ := db.ListClientDates(s.DB, idn) // app-entered additional profile dates
	data["R"] = consoleRecord(c, cases, track, ci, ptr, gps, extras, ledger, extraDates, caseFilter)
	data["CSRF"] = s.Auth.CSRF(w, r)
	data["OverridableFields"] = db.OverridableFields() // for the supervisor "Correct field" modal
	data["Officers"] = s.officerChoices(clients)       // for the "Edit case info" officer select
	data["Pinned"] = db.IsPinned(s.DB, auth.User(r), idn)
	data["AppWaiver"] = db.HasFeeWaiver(s.DB, idn)        // Waive-fees vs Remove-waiver on the ⋯ menu
	data["Flags"], _ = db.ListActiveFlags(s.DB, idn)      // manual alert banner at the top of the record
	data["Photos"], _ = db.ListDefendantPhotos(s.DB, idn) // defendant/victim photos (report #10)
	// Self-service (QR) check-ins for this client, decorated like the approval
	// queue, so the record's Check-ins tab shows them with status + telemetry.
	if selfCheckins, err := db.ListCheckinsForIDN(s.DB, idn); err == nil && len(selfCheckins) > 0 {
		rows := make([]checkinRow, 0, len(selfCheckins))
		for _, c := range selfCheckins {
			rows = append(rows, decorateCheckin(c))
		}
		data["SelfCheckins"] = rows
	}
	s.renderConsole(w, "console_record.html", data)
}

// APIClientByID returns one client's full computed bundle as JSON, addressed by
// path (GET /api/clients/{idn}) per the Build-Spec §4.1. The query-param variant
// (APIClient) stays for back-compat. Demonstrates the server-side math is the
// single source of truth for the new app too.
func (s *Server) APIClientByID(w http.ResponseWriter, r *http.Request) {
	idn := strings.TrimSpace(chi.URLParam(r, "idn"))
	clients, err := s.clients()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	track := compute.TodayET() // JSON API stays anchored to today (stable, cookie-independent)
	cases := clients[idn]
	if len(cases) == 0 {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
		return
	}
	c, caseFilter := selectCase(cases, r.URL.Query().Get("case"))
	extras, _ := db.LoadExtras(s.DB, idn)
	writeJSON(w, http.StatusOK, map[string]any{
		"idn":        c.IDN,
		"name":       c.Name,
		"level":      c.Level,
		"status":     c.Status,
		"officer":    c.Officer,
		"caseNo":     c.CaseNo,
		"cases":      caseOptions(cases),
		"checkIns":   compute.ComputeCheckIns(*c, track),
		"ptr":        compute.ComputePTRFees(*c, track, caseFilter),
		"gps":        compute.ComputeGPS(*c, track, nil, caseFilter),
		"notes":      extras.Notes,
		"courtDates": extras.CourtDates,
		"violations": extras.Violations,
	})
}

// ── /console/calendar ─────────────────────────────────────────────────────────

func (s *Server) ConsoleCalendar(w http.ResponseWriter, r *http.Request) {
	clients, err := s.clients()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	track := s.trackFrom(r)
	year, month := track.Year(), track.Month()
	if mp := r.URL.Query().Get("month"); mp != "" {
		if t, e := time.Parse("2006-01", mp); e == nil {
			year, month = t.Year(), t.Month()
		}
	}
	cur := compute.Noon(year, month, 1)
	data := s.consoleBase(w, r, "calendar", track)
	data["PrevMonth"] = cur.AddDate(0, -1, 0).Format("2006-01")
	data["NextMonth"] = cur.AddDate(0, 1, 0).Format("2006-01")

	idn := strings.TrimSpace(r.URL.Query().Get("idn"))
	if idn != "" {
		if cases := clients[idn]; len(cases) > 0 {
			c, _ := selectCase(cases, r.URL.Query().Get("case"))
			courtDates, _ := db.ListCourtDates(s.DB, idn) // merge app-entered court dates
			title, days := calendarMonth(c, courtDates, track, year, month)
			data["Client"] = c
			data["Title"] = title
			data["Days"] = days
			s.renderConsole(w, "console_calendar.html", data)
			return
		}
	}
	// Roster (team-standup) view — optionally scoped to one officer's caseload so
	// each officer can pull up just their own people. The officer picker lists
	// every supervising officer; an empty selection is the division-wide view.
	officer := strings.TrimSpace(r.URL.Query().Get("officer"))
	data["Officers"] = distinctOfficers(clients)
	data["Fofficer"] = officer
	data["FofficerQ"] = url.QueryEscape(officer)
	rc := rosterCalendarMonth(clientsForOfficer(clients, officer), courtDatesByIDN(s.DB), track, year, month)
	data["Roster"] = true
	data["RC"] = rc
	data["Title"] = rc.Title
	data["MonthISO"] = cur.Format("2006-01") // template builds day-drill links: ?date=MonthISO-DD
	s.renderConsole(w, "console_calendar.html", data)
}

// courtDatesByIDN groups every live (non-tombstoned) court date by client IDN,
// for the roster calendar's per-day court counts.
func courtDatesByIDN(d *sql.DB) map[string][]models.CourtDate {
	all, _ := db.ListAllCourtDatesLive(d)
	out := make(map[string][]models.CourtDate, len(all))
	for _, cd := range all {
		out[cd.IDN] = append(out[cd.IDN], cd)
	}
	return out
}

// ── /console/calendar/day — drill-down for one day ────────────────────────────

// calDayItem is one client's event on the drilled-into day.
type calDayItem struct {
	IDN     string
	Name    string
	Officer string
	Kind    string // court | checkin-* | payment | ptr-fee | missed | due | referral | gps-* | closed
	Label   string
}

// calDaySection is a titled group of same-category items for the day page.
type calDaySection struct {
	Title string
	Tone  string // chip tone: info | risk | warn | ok | neutral
	Items []calDayItem
}

// calDayBuckets accumulates day items into fixed, priority-ordered categories.
type calDayBuckets struct {
	court, missed, due, checkin, payment, other []calDayItem
}

func (b *calDayBuckets) add(kind string, it calDayItem) {
	switch {
	case kind == "court":
		b.court = append(b.court, it)
	case kind == "missed":
		b.missed = append(b.missed, it)
	case kind == "due":
		b.due = append(b.due, it)
	case strings.HasPrefix(kind, "checkin"):
		b.checkin = append(b.checkin, it)
	case kind == "payment" || kind == "ptr-fee":
		b.payment = append(b.payment, it)
	default:
		b.other = append(b.other, it)
	}
}

func (b *calDayBuckets) total() int {
	return len(b.court) + len(b.missed) + len(b.due) + len(b.checkin) + len(b.payment) + len(b.other)
}

// sections returns the non-empty categories in display order, items sorted by name.
func (b *calDayBuckets) sections() []calDaySection {
	defs := []calDaySection{
		{Title: "Court appearances", Tone: "info", Items: b.court},
		{Title: "Missed check-ins", Tone: "risk", Items: b.missed},
		{Title: "Check-ins due", Tone: "warn", Items: b.due},
		{Title: "Check-ins logged", Tone: "ok", Items: b.checkin},
		{Title: "Payments", Tone: "neutral", Items: b.payment},
		{Title: "Other", Tone: "neutral", Items: b.other},
	}
	out := make([]calDaySection, 0, len(defs))
	for _, sec := range defs {
		if len(sec.Items) == 0 {
			continue
		}
		sort.Slice(sec.Items, func(i, j int) bool { return sec.Items[i].Name < sec.Items[j].Name })
		out = append(out, sec)
	}
	return out
}

// ConsoleCalendarDay lists everything happening on one day across the (optionally
// officer-scoped) caseload — the click-into-a-day drill-down from the calendar:
// court appearances, missed/due check-ins, logged check-ins, payments, and the
// rest, each linking to the client's record.
func (s *Server) ConsoleCalendarDay(w http.ResponseWriter, r *http.Request) {
	clients, err := s.clients()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	track := s.trackFrom(r)

	// Target day (default today).
	day := compute.Noon(track.Year(), track.Month(), track.Day())
	if dp := strings.TrimSpace(r.URL.Query().Get("date")); dp != "" {
		if dt, ok := compute.ParseDay(dp); ok {
			day = dt
		}
	}
	officer := strings.TrimSpace(r.URL.Query().Get("officer"))
	courtByIDN := courtDatesByIDN(s.DB)
	floor := compute.CheckInDataFloor()

	buckets := &calDayBuckets{}
	for idn, cases := range clientsForOfficer(clients, officer) {
		c := openRep(cases)
		if c == nil {
			continue
		}
		name, off := dash(c.Name), compute.FmtOfficer(c.Officer)
		hasRec := hasCheckInRecord(c)
		for _, ev := range compute.GetEventsForClient(*c, track) {
			if !sameDay(ev.Date, day) {
				continue
			}
			if ev.Kind == "missed" && (!hasRec || ev.Date.Before(floor)) {
				continue // same gating as the rosters/roster calendar
			}
			buckets.add(ev.Kind, calDayItem{IDN: idn, Name: name, Officer: off, Kind: ev.Kind, Label: ev.Label})
		}
		for _, cd := range courtByIDN[idn] {
			if dt, ok := compute.ParseDay(cd.CourtDate); ok && sameDay(dt, day) {
				buckets.add("court", calDayItem{IDN: idn, Name: name, Officer: off, Kind: "court", Label: courtLabel(cd.Court, false)})
			}
			if dt, ok := compute.ParseDay(cd.NextDate); ok && sameDay(dt, day) {
				buckets.add("court", calDayItem{IDN: idn, Name: name, Officer: off, Kind: "court", Label: courtLabel(cd.Court, true)})
			}
		}
	}

	data := s.consoleBase(w, r, "calendar", track)
	data["DayTitle"] = day.Format("Monday, January 2, 2006")
	data["PrevDay"] = day.AddDate(0, 0, -1).Format("2006-01-02")
	data["NextDay"] = day.AddDate(0, 0, 1).Format("2006-01-02")
	data["BackMonth"] = day.Format("2006-01")
	data["Fofficer"] = officer
	data["FofficerQ"] = url.QueryEscape(officer)
	data["Sections"] = buckets.sections()
	data["Total"] = buckets.total()
	s.renderConsole(w, "console_calendar_day.html", data)
}

// clientsForOfficer narrows the roster to clients whose open-preferred case is
// supervised by the named officer (case-insensitive). An empty name returns the
// full set unchanged, so the division-wide calendar is the default.
func clientsForOfficer(clients map[string][]*compute.Client, officer string) map[string][]*compute.Client {
	officer = strings.TrimSpace(officer)
	if officer == "" {
		return clients
	}
	out := make(map[string][]*compute.Client, len(clients))
	for idn, cases := range clients {
		if c := openRep(cases); c != nil && strings.EqualFold(strings.TrimSpace(c.Officer), officer) {
			out[idn] = cases
		}
	}
	return out
}

// ── /console/compliance — Behind on GPS + Missed Check-Ins ────────────────────

func (s *Server) ConsoleCompliance(w http.ResponseWriter, r *http.Request) {
	clients, err := s.clients()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	track := s.trackFrom(r)
	now := compute.NowET()
	data := s.consoleBase(w, r, "compliance", track)
	data["Behind"] = behindRoster(clients, track)
	data["Reviewed"] = reviewedNotBehindRoster(clients)
	data["Missed"] = missedCheckInsRoster(clients, track)
	violations, _ := db.ListAllViolations(s.DB)
	// Scope to the stats epoch so the roster count matches the dashboard's
	// "Open Violations since {epoch}" KPI that links here.
	data["Violations"] = violationRoster(clients, violationsSinceEpoch(violations))
	data["MatchTime"] = now.Format("3:04 PM")
	s.renderConsole(w, "console_compliance.html", data)
}

// ── /console/reports ──────────────────────────────────────────────────────────

func (s *Server) ConsoleReports(w http.ResponseWriter, r *http.Request) {
	clients, err := s.clients()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	track := s.trackFrom(r)
	a := analyticsData(clients, track)
	data := s.consoleBase(w, r, "reports", track)
	data["A"] = a
	// Derived rates (all from the existing math) for the headline strip.
	data["GpsCollectRate"] = pct(a.TotalGpsPaid, a.TotalGpsOwed)
	data["PtrCollectRate"] = pct(a.TotalPtrPaid, float64(a.TotalPtrOwed))
	data["CheckinComplianceRate"] = pct(float64(a.Stats.Open-a.Stats.MissedMonth), float64(a.Stats.Open))
	data["GpsComplianceRate"] = pct(float64(a.Stats.GPSActive-a.Stats.BehindGPS), float64(a.Stats.GPSActive))
	s.renderConsole(w, "console_reports.html", data)
}

// ── /console/admin ────────────────────────────────────────────────────────────

// adminUserRow is one row of the Users & Roles table: the app_users record plus UI
// flags — Permanent (break-glass admin → controls disabled) and IsSelf (the current
// admin → no self-remove).
type adminUserRow struct {
	Email, Role, AddedBy string
	Permanent, IsSelf    bool
}

func (s *Server) ConsoleAdmin(w http.ResponseWriter, r *http.Request) {
	track := s.trackFrom(r)
	data := s.consoleBase(w, r, "admin", track)
	// Tombstones + recent audit (real); users/conditions/templates are demo-safe
	// placeholders rendered by the template until those config tables exist.
	tomb, _ := db.ListTombstones(s.DB)
	audit, _ := db.ListAudit(s.DB, "", 25)
	data["Tombstones"] = tomb
	data["Audit"] = audit
	data["Fields"] = db.OverridableFields()
	// Users & roles roster (admins edit it here; supervisors see it read-only).
	appUsers, _ := db.ListAppUsers(s.DB)
	me := strings.ToLower(strings.TrimSpace(auth.User(r)))
	userRows := make([]adminUserRow, 0, len(appUsers))
	for _, u := range appUsers {
		userRows = append(userRows, adminUserRow{
			Email: u.Email, Role: u.Role, AddedBy: u.AddedBy,
			Permanent: s.Auth.IsBreakGlassAdmin(u.Email),
			IsSelf:    u.Email == me,
		})
	}
	data["Users"] = userRows
	data["Roles"] = []string{"officer", "supervisor", "admin"}
	// Caseload-by-last-name grid (officers × A–Z). Officer rows = everyone on the
	// email allow-list, shown by name (FmtOfficer), so every authorized user can be
	// assigned a letter regardless of whether they currently supervise any open
	// client. Unioned with any officer that already owns a letter (so an existing
	// assignment always renders a row and survives a save even if that email is no
	// longer on the list).
	caseload, _ := db.LoadCaseloadLetters(s.DB)
	var officers []string
	seen := map[string]bool{}
	for _, email := range s.Auth.AllowedEmails() {
		name := compute.FmtOfficer(email)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		officers = append(officers, name)
	}
	for _, o := range caseload {
		if !seen[o] {
			officers = append(officers, o)
			seen[o] = true
		}
	}
	sort.Strings(officers)
	data["Officers"] = officers
	data["Caseload"] = caseload
	data["Letters"] = db.Letters
	data["CSRF"] = s.Auth.CSRF(w, r) // for the one-click "Undo last delete" form + caseload save
	s.renderConsole(w, "console_admin.html", data)
}

// courtCell carries both the human label ("Jan 2") and an ISO sort key
// ("2006-01-02") for the roster's Next Court column. The display string alone
// would sort alphabetically by month name (Apr, Aug, Dec…); the ISO key makes
// the column sort chronologically.
type courtCell struct{ Display, Sort string }

// nextCourtByIDN maps each IDN to its soonest upcoming court date (for the
// roster table's "Next Court" column). Empty when there are no court dates. Court
// dates are app-entered extension data (often empty on a fresh DB).
func nextCourtByIDN(d *sql.DB, track time.Time) map[string]courtCell {
	cds, err := db.ListAllCourtDates(d)
	if err != nil {
		return nil
	}
	out := map[string]courtCell{} // first future date wins (ListAllCourtDates is date-sorted)
	for _, cd := range cds {
		if _, seen := out[cd.IDN]; seen {
			continue
		}
		if dt, ok := compute.ParseDay(cd.CourtDate); ok && !dt.Before(track) {
			out[cd.IDN] = courtCell{Display: dt.Format("Jan 2"), Sort: dt.Format("2006-01-02")}
		}
	}
	return out
}
