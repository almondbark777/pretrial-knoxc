package handlers

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"pretrial-knoxc/internal/auth"
	"pretrial-knoxc/internal/compute"
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
