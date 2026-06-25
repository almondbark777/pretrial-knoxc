package handlers

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"pretrial-knoxc/internal/auth"
	"pretrial-knoxc/internal/compute"
	"pretrial-knoxc/internal/db"
	"pretrial-knoxc/internal/models"
)

// Printable reports. Each report view renders the same generic report.html
// (a clean table) with a Print button; print CSS flips it to a black-on-white
// document. The matching CSV export is one click away. The show-cause letters
// are their own report (Past-Due EM Fees — see emfees.go), not this one: this
// "Behind on GPS Coverage" view is the check-in/coverage compliance roster.

// Reports renders the reports hub.
func (s *Server) Reports(w http.ResponseWriter, r *http.Request) {
	user := auth.User(r)
	s.render(w, "reports.html", map[string]any{
		"User": user, "IsSupervisor": s.Auth.IsSupervisor(user), "ActiveNav": "reports",
	})
}

// renderReport stamps the as-of date and renders the generic report template.
func (s *Server) renderReport(w http.ResponseWriter, r *http.Request, rep models.Report) {
	rep.AsOf = compute.TodayET().Format("January 2, 2006")
	user := auth.User(r)
	s.render(w, "report.html", map[string]any{
		"User": user, "IsSupervisor": s.Auth.IsSupervisor(user), "R": rep,
	})
}

// ReportBehind is the print-ready Behind-on-GPS coverage report. The default
// is the flat roster; ?by=officer renders the per-officer split (one section
// per supervising officer with a count + behind-$ subtotal) — the STATUS
// nice-to-have "per-officer split on the Behind report".
func (s *Server) ReportBehind(w http.ResponseWriter, r *http.Request) {
	clients, err := s.clients()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	track := compute.TodayET()
	roster := behindRoster(clients, track)
	var rows [][]string
	for _, x := range roster {
		rows = append(rows, []string{x.Name, x.IDN, x.Officer, levelLabel(x.Level), x.Detail})
	}
	rep := models.Report{
		Title:    "Behind on GPS Coverage",
		Subtitle: "Clients whose GPS payments are behind",
		Columns:  []string{"Name", "IDN", "Officer", "Level", "Behind by"},
		Rows:     rows,
		CSVPath:  "/export/behind.csv",
		Note:     "This is the GPS coverage-compliance roster. The show-cause letters (past-due EM-fee memos) are on the Past-Due EM Fees report at /reports/em-fees.",
		AltLabel: "Group by officer",
		AltURL:   "/reports/behind?by=officer",
	}
	if r.URL.Query().Get("by") == "officer" {
		rep.Title = "Behind on GPS Coverage — by Officer"
		rep.Subtitle = "Clients whose GPS payments are behind, per supervising officer"
		rep.Columns = []string{"Name", "IDN", "Level", "Behind by"} // officer is the section header
		rep.Groups = groupBehindByOfficer(roster)
		rep.AltLabel = "Flat list"
		rep.AltURL = "/reports/behind"
	}
	s.renderReport(w, r, rep)
}

// groupBehindByOfficer splits the Behind roster into one section per
// supervising officer (alphabetical, blank officer last as "Unassigned"),
// each with a count + total-behind-dollars subtotal. Pure function so the
// split is unit-testable without rendering.
func groupBehindByOfficer(roster []models.RosterRow) []models.ReportGroup {
	byOff := map[string][]models.RosterRow{}
	for _, x := range roster {
		off := strings.TrimSpace(x.Officer)
		byOff[off] = append(byOff[off], x)
	}
	keys := make([]string, 0, len(byOff))
	for k := range byOff {
		if k != "" {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return strings.ToUpper(keys[i]) < strings.ToUpper(keys[j]) })
	if _, ok := byOff[""]; ok {
		keys = append(keys, "") // Unassigned last
	}
	groups := make([]models.ReportGroup, 0, len(keys))
	for _, k := range keys {
		g := models.ReportGroup{Label: k}
		if k == "" {
			g.Label = "Unassigned"
		}
		var behind float64
		for _, x := range byOff[k] {
			// roster rows are already name-sorted (behindRoster sorts)
			g.Rows = append(g.Rows, []string{x.Name, x.IDN, levelLabel(x.Level), x.Detail})
			behind += -x.Amount // Amount is the (negative) surplus
		}
		plural := "clients"
		if len(g.Rows) == 1 {
			plural = "client"
		}
		g.Subtotal = fmt.Sprintf("%d %s · $%.2f behind", len(g.Rows), plural, behind)
		groups = append(groups, g)
	}
	return groups
}

// ReportMissed is the print-ready Missed-Check-Ins-this-month report.
func (s *Server) ReportMissed(w http.ResponseWriter, r *http.Request) {
	clients, err := s.clients()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	track := compute.TodayET()
	var rows [][]string
	for _, x := range missedCheckInsRoster(clients, track) {
		rows = append(rows, []string{x.Name, x.IDN, x.Officer, levelLabel(x.Level), x.Detail})
	}
	s.renderReport(w, r, models.Report{
		Title:    "Missed Check-Ins — This Month",
		Subtitle: "Open clients with no check-in in the current calendar month (L1 excluded)",
		Columns:  []string{"Name", "IDN", "Officer", "Level", "Detail"},
		Rows:     rows,
		CSVPath:  "/export/missed.csv",
	})
}

// letterReportColumns is the shared header for the show-cause-letter history —
// the printable report and the CSV export use the same shape so the two never
// drift. Kept here next to letterReportRows.
var letterReportColumns = []string{"Generated (ET)", "Client", "IDN", "Case", "Type", "Detail", "By"}

// letterTypeLabel turns the stored letter_type token into a human label. Only
// "em_fees" exists today; the default keeps any future type readable.
func letterTypeLabel(t string) string {
	switch strings.TrimSpace(t) {
	case "em_fees":
		return "Past-due EM fee"
	case "":
		return "—"
	default:
		return t
	}
}

// clientNames maps each IDN to its display name from the computed roster, so a
// letter-history row can show a name instead of a bare IDN. A letter for a
// since-removed/closed client just isn't in the active roster — callers fall
// back to the IDN for those.
func clientNames(clients map[string][]*compute.Client) map[string]string {
	out := make(map[string]string, len(clients))
	for idn, cs := range clients {
		for _, c := range cs {
			if n := strings.TrimSpace(c.Name); n != "" {
				out[idn] = n
				break
			}
		}
	}
	return out
}

// letterReportRows formats letter_log entries into table/CSV rows, resolving
// each IDN to a name where the roster knows it. Aligned with
// letterReportColumns.
func letterReportRows(entries []models.LetterLogEntry, names map[string]string) [][]string {
	rows := make([][]string, 0, len(entries))
	for _, e := range entries {
		name := names[e.IDN]
		if name == "" {
			name = e.IDN
		}
		rows = append(rows, []string{
			e.CreatedAt, name, e.IDN, e.Case, letterTypeLabel(e.Type), e.Detail,
			compute.FmtOfficer(e.GeneratedBy),
		})
	}
	return rows
}

// letterHistory loads the recent letter-generation feed and resolves client
// names — the shared body of the report and the CSV export. The roster build is
// skipped entirely when no letters exist (the common pre-go-live case).
func (s *Server) letterHistory(limit int) ([][]string, error) {
	entries, err := db.ListRecentLetters(s.DB, limit)
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	if len(entries) > 0 {
		if clients, err := s.clients(); err == nil {
			names = clientNames(clients)
		}
	}
	return letterReportRows(entries, names), nil
}

// ReportLetters is the print-ready cross-client show-cause-letter history: every
// past-due EM-fee memo the site has generated (single download or batch zip),
// newest first. The per-client view lives on the record's Activity timeline and
// the EM-fees report's "Last letter" column; this is the office-wide log of who
// got a letter and when — so a second officer doesn't re-send what was just sent.
func (s *Server) ReportLetters(w http.ResponseWriter, r *http.Request) {
	rows, err := s.letterHistory(500)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.renderReport(w, r, models.Report{
		Title:    "Show-Cause Letters — Recent",
		Subtitle: "Past-due EM-fee memos generated from this site, newest first",
		Columns:  letterReportColumns,
		Rows:     rows,
		CSVPath:  "/export/letters.csv",
		Note:     "Logged automatically when a memo is downloaded — single downloads and batch zips alike. The per-client history also shows on each record's Activity tab and as the “Last letter” column on the Past-Due EM Fees report.",
	})
}
