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
// LastLetter is when this client most recently had a memo generated (from
// letter_log) — it informs the per-row "include in this print run" toggle.
type emFeeRow struct {
	Name, IDN, Case, Court, Type      string
	Rate                              string
	Start, End                        string
	Days                              int
	Owed, Paid, Behind, DaysBehind    string
	StartSrc, Kind, RowClass, SwitchT string
	LastLetter, LastLetterBy          string
}

func emFeeRows(recs []emfees.Rec, kind string, last map[string]db.LetterStamp) []emFeeRow {
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
		if st, ok := last[r.IDN]; ok {
			row.LastLetter = shortStamp(st.At)
			row.LastLetterBy = compute.FmtOfficer(st.By)
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
	last := db.LastLetters(s.DB, "em_fees")
	s.render(w, "report_emfees.html", map[string]any{
		"User":          user,
		"IsSupervisor":  s.Auth.IsSupervisor(user),
		"ActiveNav":     "reports",
		"AsOf":          res.AsOf.Format("January 2, 2006"),
		"Open":          emFeeRows(res.Open, "open", last),
		"Closed":        emFeeRows(res.Closed, "closed", last),
		"OpenCount":     len(res.Open),
		"ClosedCount":   len(res.Closed),
		"OpenTotal":     emfees.Money(res.OpenTotal()),
		"ClosedTotal":   emfees.Money(res.ClosedTotal()),
		"GrandTotal":    emfees.Money(res.OpenTotal() + res.ClosedTotal()),
		"SkippedNoType": res.SkippedNoType,
		"CSRF":          s.Auth.CSRF(w, r), // the batch-generate form POSTs the selection
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
	// Record the generation before streaming — the report's "Last letter"
	// column is only trustworthy if no letter leaves the building unlogged.
	if err := db.LogLetters(s.DB, auth.User(r), "em_fees", []db.LetterRef{letterRef(*found, kind)}); err != nil {
		http.Error(w, "letter log failed: "+err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	w.Header().Set("Content-Disposition", `attachment; filename="`+emfees.MemoFilename(*found)+`"`)
	_, _ = w.Write(doc)
}

// letterRef builds the letter_log entry for one memo.
func letterRef(rec emfees.Rec, kind string) db.LetterRef {
	if kind != "closed" {
		kind = "open"
	}
	return db.LetterRef{IDN: rec.IDN, Case: rec.Case, Detail: "behind " + emfees.Money(rec.Behind) + " · " + kind}
}

// EMFeeMemosZip streams the SELECTED memos as a zip with Open/ and Closed/
// folders — the same layout the skill writes to disk. The report's checkboxes
// POST `sel` values shaped "kind|idn|case" (CSRF-guarded), so the print run is
// exactly the toggled set; every included memo is recorded in letter_log.
func (s *Server) EMFeeMemosZip(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	sel := map[string]bool{}
	for _, v := range r.Form["sel"] {
		sel[v] = true
	}
	if len(sel) == 0 {
		http.Error(w, "no clients selected — tick at least one letter on the report", http.StatusBadRequest)
		return
	}
	res, err := db.EMFees(s.DB, emFeeAsOf())
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	filtered := emfees.Result{AsOf: res.AsOf, SkippedJunk: res.SkippedJunk, SkippedNoType: res.SkippedNoType}
	var refs []db.LetterRef
	for _, rec := range res.Open {
		if sel["open|"+rec.IDN+"|"+rec.Case] {
			filtered.Open = append(filtered.Open, rec)
			refs = append(refs, letterRef(rec, "open"))
		}
	}
	for _, rec := range res.Closed {
		if sel["closed|"+rec.IDN+"|"+rec.Case] {
			filtered.Closed = append(filtered.Closed, rec)
			refs = append(refs, letterRef(rec, "closed"))
		}
	}
	if len(refs) == 0 {
		http.Error(w, "selection didn't match any current past-due records — reload the report and try again", http.StatusBadRequest)
		return
	}
	z, err := emfees.MemosZip(filtered, "all")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if err := db.LogLetters(s.DB, auth.User(r), "em_fees", refs); err != nil {
		http.Error(w, "letter log failed: "+err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="past-due-em-fee-memos-`+strconv.Itoa(len(refs))+"-selected-"+stamp()+`.zip"`)
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
