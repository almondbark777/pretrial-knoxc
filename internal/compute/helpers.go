package compute

import (
	"strings"
	"time"
	"unicode"
)

// FmtOfficer turns an officer email into a display name:
// "Nicholas.Loveless@knoxsheriff.org" -> "Nicholas Loveless".
// Single helper, per the conventions (Brief 5.3).
func FmtOfficer(email string) string {
	if email == "" {
		return ""
	}
	local := email
	if i := strings.IndexByte(email, '@'); i >= 0 {
		local = email[:i]
	}
	parts := strings.FieldsFunc(local, func(r rune) bool { return r == '.' || r == '_' })
	for i, p := range parts {
		if p == "" {
			continue
		}
		runes := []rune(strings.ToLower(p))
		runes[0] = unicode.ToUpper(runes[0])
		parts[i] = string(runes)
	}
	return strings.Join(parts, " ")
}

// nyLoc is America/New_York; resolved once. Falls back to UTC only if tzdata is
// somehow unavailable (cmd/server imports time/tzdata to guarantee it isn't).
var nyLoc *time.Location

func init() {
	if loc, err := time.LoadLocation("America/New_York"); err == nil {
		nyLoc = loc
	} else {
		nyLoc = time.UTC
	}
}

// TodayET returns the current Eastern-time calendar date as a noon-UTC Time,
// matching the JS Intl.DateTimeFormat('America/New_York') "today" default.
func TodayET() time.Time {
	n := time.Now().In(nyLoc)
	return Noon(n.Year(), n.Month(), n.Day())
}

// NowET returns the current instant in America/New_York (full timestamp, not
// just the date). Used to stamp audit_log rows in Eastern time, per the brief —
// reuses the same resolved location as TodayET.
func NowET() time.Time { return time.Now().In(nyLoc) }
