package compute

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Event is one dated item on a client's calendar — a faithful port of the
// canonical getEventsForClient (assets/8a6913e5-*.js): referral, GPS install/
// switch/closed, each check-in (split in-person/phone/other), each payment
// (split GPS-side vs PTR-side), and the missed/upcoming check-in windows.
type Event struct {
	Date  time.Time
	Kind  string
	Label string
}

var (
	rePhone    = regexp.MustCompile(`\bphone\b|\btext\b|\bcall\b`)
	reInPerson = regexp.MustCompile(`in.?person|office|in.?office|walk.?in`)
)

// GetEventsForClient ports getEventsForClient(c, todayStr). track is the as-of
// date (used for the missed/due window classification). Events are returned
// sorted ascending by date. v0.78: PTR fees are NOT synthesized on the 1st —
// only real payment rows appear.
func GetEventsForClient(c Client, track time.Time) []Event {
	var events []Event
	push := func(d time.Time, ok bool, kind, label string) {
		if !ok || d.IsZero() {
			return
		}
		events = append(events, Event{Date: d, Kind: kind, Label: label})
	}

	name := c.Name
	if name == "" {
		name = c.IDN
	}
	push(c.RefD, c.RefOK, "referral", "Referred: "+name)
	if d, ok := ParseDay(c.GpInstall); ok {
		push(d, true, "gps-install", "GPS installed")
	}
	if d, ok := ParseDay(c.GpSwitchedDate); ok {
		push(d, true, "gps-switch", "GPS switched to "+c.GpSwitchedTo)
	}
	push(c.ClosedD, c.ClosedOK, "closed", "Case closed")

	for _, ci := range c.CheckIns {
		tl := strings.ToLower(strings.TrimSpace(ci.Type))
		kind := "checkin-other"
		if rePhone.MatchString(tl) {
			kind = "checkin-phone"
		} else if reInPerson.MatchString(tl) || tl == "" {
			kind = "checkin-inperson"
		}
		label := "Check-in"
		if ci.Type != "" {
			label += " (" + ci.Type + ")"
		}
		push(ci.D, ci.DOK, kind, label)
	}

	for _, p := range c.Payments {
		tl := strings.ToLower(strings.TrimSpace(p.Type))
		kind := "payment"
		if rePTR.MatchString(tl) {
			kind = "ptr-fee"
		}
		label := "$" + strconv.FormatFloat(p.Amt, 'f', -1, 64)
		if p.Type != "" {
			label += " " + p.Type
		}
		push(p.D, p.DOK, kind, label)
	}

	ci := ComputeCheckIns(c, track)
	for _, w := range ci.Windows {
		if w.Missed {
			push(w.Deadline, true, "missed", "Missed: "+w.Label)
		} else if !w.Satisfied && gt(w.Start, ci.Today) {
			push(w.Deadline, true, "due", "Due: "+w.Label)
		}
	}

	sort.SliceStable(events, func(i, j int) bool { return events[i].Date.Before(events[j].Date) })
	return events
}
