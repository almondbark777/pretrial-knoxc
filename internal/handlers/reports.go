package handlers

import (
	"net/http"

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

// ReportBehind is the print-ready Behind-on-GPS coverage report.
func (s *Server) ReportBehind(w http.ResponseWriter, r *http.Request) {
	clients, err := s.clients()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	track := compute.TodayET()
	var rows [][]string
	for _, x := range behindRoster(clients, track) {
		rows = append(rows, []string{x.Name, x.IDN, x.Officer, levelLabel(x.Level), x.Detail})
	}
	s.renderReport(w, r, models.Report{
		Title:    "Behind on GPS Coverage",
		Subtitle: "Clients whose GPS payments are behind",
		Columns:  []string{"Name", "IDN", "Officer", "Level", "Behind by"},
		Rows:     rows,
		CSVPath:  "/export/behind.csv",
		Note:     "This is the GPS coverage-compliance roster. The show-cause letters (past-due EM-fee memos) are on the Past-Due EM Fees report at /reports/em-fees.",
	})
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
