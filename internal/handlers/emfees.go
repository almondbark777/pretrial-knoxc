package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"pretrial-knoxc/internal/auth"
	"pretrial-knoxc/internal/compute"
	"pretrial-knoxc/internal/db"
	"pretrial-knoxc/internal/emfees"
)

// Past-Due EM Fees — the show-cause-letter report. This is a faithful port of the
// canonical "past-due-em-fees" skill (internal/emfees), wired to the live data:
// it lists every client 5+ days behind on GPS monitoring fees (Open + Closed) and
// generates the user's own past-due memo (.docx) per person — one at a time or the
// whole batch as a zip with Open/ and Closed/ folders, exactly like the skill.

// emFeeAsOf is today's ET date at UTC midnight — the billing-period end for Open
// cases and the date printed on the memos. Matches the skill's midnight as-of.
func emFeeAsOf() time.Time {
	t := compute.TodayET()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// emFeeRow is one display row (pre-formatted, so the template stays dumb).
type emFeeRow struct {
	Name, IDN, Case, Court, Type      string
	Rate                              string
	Start, End                        string
	Days                              int
	Owed, Paid, Behind, DaysBehind    string
	StartSrc, Kind, RowClass, SwitchT string
}

func emFeeRows(recs []emfees.Rec, kind string) []emFeeRow {
	out := make([]emFeeRow, 0, len(recs))
	for _, r := range recs {
		row := emFeeRow{
			Name: r.Name, IDN: r.IDN, Case: r.Case, Court: r.Court, Type: r.Type,
			Rate:  "$" + strconv.Itoa(r.Rate),
			Start: r.Start.Format("1/2/2006"), Days: r.Days,
			Owed: emfees.Money(r.Owed), Paid: emfees.Money(r.Paid), Behind: emfees.Money(r.Behind),
			DaysBehind: strconv.FormatFloat(r.DaysBehind, 'f', 1, 64),
			StartSrc:   r.StartSrc, Kind: kind, RowClass: emFeeRowClass(r),
		}
		if !r.End.IsZero() {
			row.End = r.End.Format("1/2/2006")
		}
		if r.HasSwitch {
			row.SwitchT = r.SwitchType
		}
		out = append(out, row)
	}
	return out
}

// emFeeRowClass mirrors the skill's spreadsheet highlighting so the same rows that
// need a manual spot-check stand out here too. Closed: pink = 2+ year span, yellow
// = Referral-only start. Open: pink = 30+ days behind, yellow = 14+.
func emFeeRowClass(r emfees.Rec) string {
	if r.Closed {
		switch {
		case r.Days > 730:
			return "row-flag-high"
		case r.StartSrc == "Referral":
			return "row-flag-mid"
		}
		return ""
	}
	switch {
	case r.DaysBehind >= 30:
		return "row-flag-high"
	case r.DaysBehind >= 14:
		return "row-flag-mid"
	}
	return ""
}

// ReportEMFees renders the Past-Due EM Fees report page.
func (s *Server) ReportEMFees(w http.ResponseWriter, r *http.Request) {
	res, err := db.EMFees(s.DB, emFeeAsOf())
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	user := auth.User(r)
	s.render(w, "report_emfees.html", map[string]any{
		"User":          user,
		"IsSupervisor":  s.Auth.IsSupervisor(user),
		"ActiveNav":     "reports",
		"AsOf":          res.AsOf.Format("January 2, 2006"),
		"Open":          emFeeRows(res.Open, "open"),
		"Closed":        emFeeRows(res.Closed, "closed"),
		"OpenCount":     len(res.Open),
		"ClosedCount":   len(res.Closed),
		"OpenTotal":     emfees.Money(res.OpenTotal()),
		"ClosedTotal":   emfees.Money(res.ClosedTotal()),
		"GrandTotal":    emfees.Money(res.OpenTotal() + res.ClosedTotal()),
		"SkippedNoType": res.SkippedNoType,
	})
}

// EMFeeMemo streams one filled-in past-due memo (.docx) for ?idn= (& optional
// ?case=) in the ?kind=open|closed list.
func (s *Server) EMFeeMemo(w http.ResponseWriter, r *http.Request) {
	idn := strings.TrimSpace(r.URL.Query().Get("idn"))
	caseQ := strings.TrimSpace(r.URL.Query().Get("case"))
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	res, err := db.EMFees(s.DB, emFeeAsOf())
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	list := res.Open
	if kind == "closed" {
		list = res.Closed
	}
	var found *emfees.Rec
	for i := range list {
		if list[i].IDN == idn && (caseQ == "" || list[i].Case == caseQ) {
			found = &list[i]
			break
		}
	}
	if found == nil {
		http.Error(w, "no past-due record for that client", http.StatusNotFound)
		return
	}
	doc, err := emfees.FillMemo(*found, emfees.DateString(res))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	w.Header().Set("Content-Disposition", `attachment; filename="`+emfees.MemoFilename(*found)+`"`)
	_, _ = w.Write(doc)
}

// EMFeeMemosZip streams all memos as a zip with Open/ and Closed/ folders
// (?kind=open|closed|all, default all) — the same layout the skill writes to disk.
func (s *Server) EMFeeMemosZip(w http.ResponseWriter, r *http.Request) {
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	if kind == "" {
		kind = "all"
	}
	res, err := db.EMFees(s.DB, emFeeAsOf())
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	z, err := emfees.MemosZip(res, kind)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="past-due-em-fee-memos-`+kind+"-"+stamp()+`.zip"`)
	_, _ = w.Write(z)
}

// ExportEMFees streams the past-due list (Open + Closed) as one CSV — the web
// equivalent of the skill's summary workbook.
func (s *Server) ExportEMFees(w http.ResponseWriter, r *http.Request) {
	res, err := db.EMFees(s.DB, emFeeAsOf())
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	rows := [][]string{}
	emit := func(category string, recs []emfees.Rec) {
		for _, x := range recs {
			end := ""
			if !x.End.IsZero() {
				end = x.End.Format("1/2/2006")
			}
			rows = append(rows, []string{
				category, x.Name, x.IDN, x.Case, x.Court, x.Type,
				strconv.Itoa(x.Rate), x.Start.Format("1/2/2006"), end,
				strconv.Itoa(x.Days),
				strconv.FormatFloat(x.Owed, 'f', 2, 64),
				strconv.FormatFloat(x.Paid, 'f', 2, 64),
				strconv.FormatFloat(x.Behind, 'f', 2, 64),
				strconv.FormatFloat(x.DaysBehind, 'f', 1, 64),
				x.StartSrc,
			})
		}
	}
	emit("OPEN", res.Open)
	emit("CLOSED", res.Closed)
	writeCSV(w, "past-due-em-fees-"+stamp()+".csv", []string{
		"Category", "Defendant", "IDN", "Case Number", "Court", "GPS Type",
		"Daily Rate", "Start Date", "Closed Date", "Days",
		"Amount Owed", "Amount Paid", "Amount Behind", "Days Behind", "Start Source",
	}, rows)
}
