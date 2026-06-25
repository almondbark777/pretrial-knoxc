package handlers

import (
	"archive/zip"
	"encoding/csv"
	"log"
	"net/http"
	"strconv"

	"pretrial-knoxc/internal/compute"
	"pretrial-knoxc/internal/db"
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

// ExportReferrals streams EVERY client (most-recently-referred first) as CSV with
// the full per-client field set — the "full data" companion to the compact
// on-screen Referrals worklist. Runs off BuildClients, so tombstones/overrides are
// already applied. The header must stay aligned with referralExportRows' cells.
func (s *Server) ExportReferrals(w http.ResponseWriter, r *http.Request) {
	clients, err := s.clients()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	header := []string{
		"Name", "IDN", "Case #(s)", "Level", "Status", "Officer",
		"Referred", "Closed", "GPS Active", "GPS Type", "GPS Install",
		"GPS Switched To", "GPS Switched Date", "Charge Type", "Bond Amount",
		"Supervision Type", "Order From", "DMA", "Birthdate", "GPS Notes",
	}
	writeCSV(w, "referrals-"+stamp()+".csv", header, referralExportRows(clients))
}

// ExportLetters streams the cross-client show-cause-letter history as CSV — the
// download companion to the /reports/letters page, same rows and header.
func (s *Server) ExportLetters(w http.ResponseWriter, r *http.Request) {
	rows, err := s.letterHistory(2000)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeCSV(w, "show-cause-letters-"+stamp()+".csv", letterReportColumns, rows)
}

// ExportBehind streams the Behind-on-GPS roster as CSV.
func (s *Server) ExportBehind(w http.ResponseWriter, r *http.Request) {
	clients, err := s.clients()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	track := s.trackFrom(r) // honor the console's as-of date so the file matches the on-screen roster
	rows := [][]string{}
	for _, x := range behindRoster(clients, track) {
		rows = append(rows, []string{
			x.Name, x.IDN, x.Officer, levelLabel(x.Level),
			strconv.FormatFloat(x.Amount, 'f', 2, 64), x.Detail,
		})
	}
	writeCSV(w, "behind-gps-"+track.Format("2006-01-02")+".csv",
		[]string{"Name", "IDN", "Officer", "Level", "Surplus $ (negative = behind)", "Detail"}, rows)
}

// ExportMissed streams the Missed-Check-Ins-this-month roster as CSV.
func (s *Server) ExportMissed(w http.ResponseWriter, r *http.Request) {
	clients, err := s.clients()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	track := s.trackFrom(r) // honor the console's as-of date so the file matches the on-screen roster
	rows := [][]string{}
	for _, x := range missedCheckInsRoster(clients, track) {
		rows = append(rows, []string{x.Name, x.IDN, x.Officer, levelLabel(x.Level), x.Detail})
	}
	writeCSV(w, "missed-checkins-"+track.Format("2006-01-02")+".csv",
		[]string{"Name", "IDN", "Officer", "Level", "Detail"}, rows)
}

// ExportViolations streams the recorded-violations roster (since the stats epoch)
// as CSV — the download companion to the compliance page's Violations panel, using
// the same epoch filter so the file matches the on-screen roster row-for-row.
func (s *Server) ExportViolations(w http.ResponseWriter, r *http.Request) {
	clients, err := s.clients()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	track := s.trackFrom(r) // used only to date-stamp the filename (the roster is epoch-scoped, not as-of-scoped)
	violations, _ := db.ListAllViolations(s.DB)
	rows := [][]string{}
	for _, x := range violationRoster(clients, violationsSinceEpoch(violations)) {
		rows = append(rows, []string{x.Name, x.IDN, x.Officer, levelLabel(x.Level), x.Date, x.Detail})
	}
	writeCSV(w, "violations-"+track.Format("2006-01-02")+".csv",
		[]string{"Name", "IDN", "Officer", "Level", "Date", "Detail"}, rows)
}

// ExportCases streams the full case-management grid as CSV.
func (s *Server) ExportCases(w http.ResponseWriter, r *http.Request) {
	clients, err := s.clients()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	track := s.trackFrom(r) // honor the console's as-of date so the file matches the on-screen roster
	rows := [][]string{}
	for _, d := range defendantRows(clients, track) {
		rows = append(rows, []string{
			d.Name, d.IDN, levelLabel(d.Level), d.Status, d.Officer, d.CaseNo,
			yesNo(d.GpsActive), d.GpsVendor, yesNo(d.BehindGPS),
			strconv.FormatFloat(d.PTRBalance, 'f', 2, 64),
			strconv.Itoa(d.MissedCount), yesNo(d.MissedMonth),
		})
	}
	writeCSV(w, "cases-"+track.Format("2006-01-02")+".csv", []string{
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

// ExportAllData streams every data table as its own CSV inside a single ZIP — the
// supervisor's "see all the data like the SharePoint lists" full dump (blue book,
// check-ins, payments, GPS, plus the app's own tables). Supervisor-gated because
// it's the complete dataset (PII); a read-only GET, so no CSRF.
func (s *Server) ExportAllData(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireSupervisor(w, r); !ok {
		return
	}
	tables, err := db.ListUserTables(s.DB)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="ptr-all-data-`+stamp()+`.zip"`)
	zw := zip.NewWriter(w)
	defer func() {
		if err := zw.Close(); err != nil {
			log.Printf("ExportAllData: zip close error: %v", err)
		}
	}()
	for _, t := range tables {
		cols, rows, derr := db.DumpTable(s.DB, t)
		if derr != nil {
			continue // skip a problematic table rather than abort the whole dump
		}
		f, ferr := zw.Create(t + ".csv")
		if ferr != nil {
			log.Printf("ExportAllData: zip.Create(%q): %v", t, ferr)
			continue // log and skip rather than abandoning remaining tables
		}
		cw := csv.NewWriter(f)
		_ = cw.Write(cols)
		for _, row := range rows {
			_ = cw.Write(row)
		}
		cw.Flush()
		if err := cw.Error(); err != nil {
			log.Printf("ExportAllData: csv flush error for table %q: %v", t, err)
		}
	}
}
