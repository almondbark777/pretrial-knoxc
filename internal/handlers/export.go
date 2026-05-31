package handlers

import (
	"encoding/csv"
	"net/http"
	"strconv"

	"pretrial-knoxc/internal/compute"
)

// CSV exports of the cross-client rosters and the cases grid — the dependency-free
// equivalent of the canonical tool's "Export to Excel" (Brief 2.8). Read-only GETs
// (no CSRF needed); they run off the same BuildClients data, so tombstones and
// overrides are already applied. Files are date-stamped for filing.

// writeCSV streams a CSV download with an attachment filename.
func writeCSV(w http.ResponseWriter, filename string, header []string, rows [][]string) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	cw := csv.NewWriter(w)
	_ = cw.Write(header)
	for _, row := range rows {
		_ = cw.Write(row)
	}
	cw.Flush()
}

// stamp returns today's ET date for filenames, e.g. "2026-05-31".
func stamp() string { return compute.TodayET().Format("2006-01-02") }

// ExportBehind streams the Behind-on-GPS roster as CSV.
func (s *Server) ExportBehind(w http.ResponseWriter, r *http.Request) {
	clients, err := s.clients()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	track := compute.TodayET()
	rows := [][]string{}
	for _, x := range behindRoster(clients, track) {
		rows = append(rows, []string{
			x.Name, x.IDN, x.Officer, levelLabel(x.Level),
			strconv.FormatFloat(x.Amount, 'f', 2, 64), x.Detail,
		})
	}
	writeCSV(w, "behind-gps-"+stamp()+".csv",
		[]string{"Name", "IDN", "Officer", "Level", "Surplus $ (negative = behind)", "Detail"}, rows)
}

// ExportMissed streams the Missed-Check-Ins-this-month roster as CSV.
func (s *Server) ExportMissed(w http.ResponseWriter, r *http.Request) {
	clients, err := s.clients()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	track := compute.TodayET()
	rows := [][]string{}
	for _, x := range missedCheckInsRoster(clients, track) {
		rows = append(rows, []string{x.Name, x.IDN, x.Officer, levelLabel(x.Level), x.Detail})
	}
	writeCSV(w, "missed-checkins-"+stamp()+".csv",
		[]string{"Name", "IDN", "Officer", "Level", "Detail"}, rows)
}

// ExportCases streams the full case-management grid as CSV.
func (s *Server) ExportCases(w http.ResponseWriter, r *http.Request) {
	clients, err := s.clients()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	track := compute.TodayET()
	rows := [][]string{}
	for _, d := range defendantRows(clients, track) {
		rows = append(rows, []string{
			d.Name, d.IDN, levelLabel(d.Level), d.Status, d.Officer, d.CaseNo,
			yesNo(d.GpsActive), d.GpsVendor, yesNo(d.BehindGPS),
			strconv.FormatFloat(d.PTRBalance, 'f', 2, 64),
			strconv.Itoa(d.MissedCount), yesNo(d.MissedMonth),
		})
	}
	writeCSV(w, "cases-"+stamp()+".csv", []string{
		"Name", "IDN", "Level", "Status", "Officer", "Cases",
		"GPS Active", "GPS Vendor", "Behind GPS", "PTR Balance", "Missed (total)", "Missed This Month",
	}, rows)
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
