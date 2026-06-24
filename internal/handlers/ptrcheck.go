// ptrcheck.go serves /console/ptr-check — a read-only tool to check the daily
// PTR export (.txt, the pipe-delimited "Clients" pull) against the live Blue Book
// data already in the system. The officer drops the .txt; it is parsed and
// matched (by IDN or any shared case #) entirely in the browser, so no file ever
// leaves their PC. The live side is embedded as JSON (the same approach the
// Clients roster uses), and the page surfaces who's on the export but missing
// from the system, who's in the system but absent from the export, field
// mismatches, and a 24h / 72h / Everything referral window.
package handlers

import (
	"encoding/json"
	"html/template"
	"net/http"

	"pretrial-knoxc/internal/compute"
)

// ptrCheckRow is one live Blue Book record shipped to the PTR-check page. Only
// the fields the comparison needs are sent — nothing beyond what the Clients
// roster already embeds for the same logged-in officer.
type ptrCheckRow struct {
	IDN    string `json:"idn"`
	Name   string `json:"name"`
	Cases  string `json:"cases"` // raw CaseNo ("@123, @456"); split + de-@'d client-side
	Level  string `json:"level"`
	Ref    string `json:"ref"`    // "M/D/YYYY H:MM" so the page's parseDate() reads it
	Order  string `json:"order"`  // raw OrderFrom; canonicalized client-side
	Sup    string `json:"sup"`    // raw SupervisionType; canonicalized client-side
	Status string `json:"status"` // case status (OPEN/CLOSED/…)
}

// ConsolePtrCheck renders the PTR Check page. Read-only: it embeds the live Blue
// Book records as JSON and lets the browser do the matching against the dropped
// PTR export.
func (s *Server) ConsolePtrCheck(w http.ResponseWriter, r *http.Request) {
	clients, err := s.clients()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	// Lives under Reports (entry point is a button on the Reports header), so keep
	// the Reports nav item highlighted while it's open.
	data := s.consoleBase(w, r, "reports", s.trackFrom(r))
	data["BBJson"] = ptrCheckBlueBookJSON(clients)
	s.renderConsole(w, "console_ptr_check.html", data)
}

// ptrCheckBlueBookJSON flattens the cached client set into comparison rows (one
// per client record; the page's union-find collapses multi-record people by
// IDN/case). Returns template.JS for a safe inline embed.
func ptrCheckBlueBookJSON(clients map[string][]*compute.Client) template.JS {
	out := make([]ptrCheckRow, 0, len(clients))
	for _, recs := range clients {
		for _, c := range recs {
			out = append(out, ptrCheckRow{
				IDN:    c.IDN,
				Name:   c.Name,
				Cases:  c.CaseNo,
				Level:  c.Level,
				Ref:    ptrCheckRefStr(c),
				Order:  c.OrderFrom,
				Sup:    c.SupervisionType,
				Status: c.Status,
			})
		}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return template.JS("[]")
	}
	return template.JS(b)
}

// ptrCheckRefStr formats the referral as "M/D/YYYY H:MM" (date-only when there's
// no timestamp), matching the M/D/YYYY parser the page shares with the PTR side.
func ptrCheckRefStr(c *compute.Client) string {
	switch {
	case c.RefDTOK:
		return c.RefDT.Format("1/2/2006 15:04")
	case c.RefOK:
		return c.RefD.Format("1/2/2006")
	default:
		return ""
	}
}
