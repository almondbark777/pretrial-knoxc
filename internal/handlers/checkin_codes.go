// checkin_codes.go is the officer-facing admin for the rotating lobby code that
// the check-in QR encodes (the "minting/printing the lobby code" piece). The
// weekly code is provenance, not a hard gate: a submission stamped with an
// expired/unknown code is auto-flagged, and reprinting the poster each week keeps
// at-home check-ins visible. This page mints a fresh code and renders a
// print-ready poster with a server-generated QR (internal/qr — no JS, no CDN).
package handlers

import (
	"crypto/rand"
	"html/template"
	"net/http"
	"strings"
	"time"

	"pretrial-knoxc/internal/auth"
	"pretrial-knoxc/internal/compute"
	"pretrial-knoxc/internal/db"
	"pretrial-knoxc/internal/models"
	"pretrial-knoxc/internal/qr"
)

// codeRow decorates a weekly code for the admin list.
type codeRow struct {
	models.WeeklyCode
	Window    string // "Jun 22 – Jun 29, 2026"
	CreatedBy string
}

// CheckinCodes renders the weekly-code admin: the active code, the mint form, the
// printable poster link, and the history.
func (s *Server) CheckinCodes(w http.ResponseWriter, r *http.Request) {
	codes, err := db.ListWeeklyCodes(s.DB)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	rows := make([]codeRow, 0, len(codes))
	for _, c := range codes {
		rows = append(rows, codeRow{WeeklyCode: c, Window: codeWindow(c.ValidFrom, c.ValidTo), CreatedBy: compute.FmtOfficer(c.CreatedBy)})
	}
	from, to := weekBounds(compute.TodayET())
	data := s.consoleBase(w, r, "checkins", s.trackFrom(r))
	data["Codes"] = rows
	data["SuggestCode"] = generateCode()
	data["SuggestFrom"] = from
	data["SuggestTo"] = to
	data["SuggestLabel"] = "Week of " + compute.TodayET().Format("Jan 2, 2006")
	if len(rows) > 0 && rows[0].Active {
		data["Active"] = rows[0]
	}
	s.renderConsole(w, "console_checkin_codes.html", data)
}

// CreateCheckinCode mints a new lobby code (deactivating the prior one) and
// returns to the admin page. CSRF-guarded (admin group).
func (s *Server) CreateCheckinCode(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	code := strings.ToUpper(strings.TrimSpace(r.FormValue("code")))
	if code == "" {
		code = generateCode()
	}
	from := strings.TrimSpace(r.FormValue("valid_from"))
	to := strings.TrimSpace(r.FormValue("valid_to"))
	if from == "" || to == "" {
		from, to = weekBounds(compute.TodayET())
	}
	label := strings.TrimSpace(r.FormValue("label"))
	if label == "" {
		label = "Week of " + compute.TodayET().Format("Jan 2, 2006")
	}
	_, err := db.CreateWeeklyCode(s.DB, code, label, from, to, auth.User(r))
	s.afterWrite(w, r, "/console/checkins/codes", err, "New lobby code minted.")
}

// CheckinPoster renders a full-page, print-ready poster for a code: the QR (built
// server-side), the URL, and the code text. ?c= selects the code; defaults to the
// active one.
func (s *Server) CheckinPoster(w http.ResponseWriter, r *http.Request) {
	code := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("c")))
	if code == "" {
		if wc, _ := db.ActiveWeeklyCode(s.DB); wc != nil {
			code = wc.Code
		}
	}
	url := s.publicBaseURL(r) + "/checkin?c=" + code
	svg := ""
	if m, err := qr.Encode(url); err == nil {
		svg = qr.SVG(m, 8, 4)
	}
	data := s.consoleBase(w, r, "checkins", s.trackFrom(r))
	data["Code"] = code
	data["URL"] = url
	data["QR"] = template.HTML(svg) // server-generated, trusted SVG
	s.renderConsole(w, "checkin_code_poster.html", data)
}

// publicBaseURL reconstructs the externally-visible origin (behind the Cloudflare
// tunnel, the proto is in X-Forwarded-Proto; default https).
func (s *Server) publicBaseURL(r *http.Request) string {
	proto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
	if proto == "" {
		if r.TLS != nil {
			proto = "https"
		} else {
			proto = "https" // the app is only reached over the HTTPS tunnel in practice
		}
	}
	return proto + "://" + r.Host
}

// generateCode returns a short, unambiguous lobby code like "K7Q2-9F" (no
// look-alike characters), random enough that an old printout reads as stale.
func generateCode() string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no I,O,0,1
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "LOBBY-" + compute.TodayET().Format("0102")
	}
	var sb strings.Builder
	for i, by := range b {
		if i == 4 {
			sb.WriteByte('-')
		}
		sb.WriteByte(alphabet[int(by)%len(alphabet)])
	}
	return sb.String()
}

// weekBounds returns this week's Monday and the following Sunday as YYYY-MM-DD.
func weekBounds(t time.Time) (from, to string) {
	wd := int(t.Weekday())
	if wd == 0 {
		wd = 7 // treat Sunday as the last day of the week
	}
	monday := t.AddDate(0, 0, -(wd - 1))
	sunday := monday.AddDate(0, 0, 6)
	return monday.Format("2006-01-02"), sunday.Format("2006-01-02")
}

// codeWindow formats a from/to pair for display ("Jun 22 – Jun 29, 2026").
func codeWindow(from, to string) string {
	f, okf := compute.ParseDay(from)
	t, okt := compute.ParseDay(to)
	if !okf || !okt {
		return from + " – " + to
	}
	return f.Format("Jan 2") + " – " + t.Format("Jan 2, 2006")
}
