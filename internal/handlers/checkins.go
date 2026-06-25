// checkins.go serves the officer-facing side of QR self-check-in: the approval
// queue at /console/checkins — the "query of people who've checked in that we
// approve" — plus the approve/reject write actions.
//
// A self-check-in lands here as status='pending'. The officer reads the
// presence badge (🟢 on-site / 🟡 unverified / 🔴 off-site, derived at
// submission from IP + GPS + the home-address comparison) and the captured
// answers, then approves (it becomes the official check-in record) or rejects
// with a reason. The underlying row is append-only — approve/reject only stamp
// the review columns, never the captured evidence.
package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"pretrial-knoxc/internal/auth"
	"pretrial-knoxc/internal/db"
	"pretrial-knoxc/internal/models"
)

// checkinRow is one pending submission decorated for the template: the stored
// record plus a display-ready badge tone/label, parsed flags, and the few
// distance/GPS strings that would otherwise need template-side math.
type checkinRow struct {
	models.Checkin
	BadgeTone   string   // chip tone: ok | warn | risk | neutral
	BadgeLabel  string   // On-site | Unverified | Off-site | Unscored
	FlagList    []string // human-readable flags parsed from the JSON column
	OfficeLabel string   // distance from office, e.g. "120 ft" / "2.3 mi" / "—"
	HomeLabel   string   // distance from the client's home address
	GpsLabel    string   // "granted ±8 ft" / "denied" / "n/a"
}

// ConsoleCheckins renders the approval queue (oldest first — FIFO). Any logged-
// in officer can work it; approving a check-in is a line-officer task.
func (s *Server) ConsoleCheckins(w http.ResponseWriter, r *http.Request) {
	pending, err := db.ListPendingCheckins(s.DB)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	rows := make([]checkinRow, 0, len(pending))
	for _, c := range pending {
		tone, label := badgeDisplay(c.PresenceBadge)
		gpsOK := strings.EqualFold(c.GPSPerm, "granted")
		rows = append(rows, checkinRow{
			Checkin:     c,
			BadgeTone:   tone,
			BadgeLabel:  label,
			FlagList:    parseFlags(c.Flags),
			OfficeLabel: distLabel(c.DistOfficeM, gpsOK),
			HomeLabel:   distLabel(c.DistHomeM, gpsOK),
			GpsLabel:    gpsLabel(c.GPSPerm, c.GPSAccuracy),
		})
	}
	data := s.consoleBase(w, r, "checkins", s.trackFrom(r))
	data["Checkins"] = rows
	data["PendingCount"] = len(rows)
	s.renderConsole(w, "console_checkins.html", data)
}

// ApproveSelfCheckin approves one pending submission into the official record.
func (s *Server) ApproveSelfCheckin(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id := formID(r)
	err := db.ApproveCheckin(s.DB, id, auth.User(r))
	s.afterWrite(w, r, checkinsBack(r), err, "Check-in approved.")
}

// RejectSelfCheckin marks a submission rejected with the officer's reason.
func (s *Server) RejectSelfCheckin(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id := formID(r)
	reason := strings.TrimSpace(r.FormValue("reason"))
	err := db.RejectCheckin(s.DB, id, auth.User(r), reason)
	s.afterWrite(w, r, checkinsBack(r), err, "Check-in rejected.")
}

// checkinsBack is the post-action redirect target (defaults to the queue).
func checkinsBack(r *http.Request) string {
	if n := strings.TrimSpace(r.FormValue("next")); strings.HasPrefix(n, "/console/") {
		return n
	}
	return "/console/checkins"
}

// badgeDisplay maps the stored presence badge to a chip tone + label. An empty
// badge (sparse telemetry, or a row predating presence scoring) reads as a
// neutral "Unscored" rather than a misleading green.
func badgeDisplay(badge string) (tone, label string) {
	switch strings.ToLower(strings.TrimSpace(badge)) {
	case "green":
		return "ok", "On-site"
	case "yellow":
		return "warn", "Unverified"
	case "red":
		return "risk", "Off-site"
	default:
		return "neutral", "Unscored"
	}
}

// parseFlags turns the JSON flags column into a display list, tolerating an
// empty/garbage value (returns nil).
func parseFlags(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

// distLabel renders a meters distance as feet (<1 mi) or miles. Returns "—"
// when there was no usable GPS fix, so the officer doesn't read a stored 0 as
// "right on top of the office."
func distLabel(meters float64, gpsOK bool) string {
	if !gpsOK || meters <= 0 {
		return "—"
	}
	if feet := meters * 3.28084; feet < 5280 {
		return fmt.Sprintf("%.0f ft", feet)
	}
	return fmt.Sprintf("%.1f mi", meters/1609.344)
}

// gpsLabel summarizes the GPS permission + accuracy for the officer.
func gpsLabel(perm string, accuracyM float64) string {
	switch strings.ToLower(strings.TrimSpace(perm)) {
	case "granted":
		if accuracyM > 0 {
			return fmt.Sprintf("granted ±%.0f ft", accuracyM*3.28084)
		}
		return "granted"
	case "denied":
		return "denied"
	default:
		return "n/a"
	}
}
