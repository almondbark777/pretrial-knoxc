package compute

import (
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// JSToFixed formats x with exactly prec decimal places, matching JavaScript's
// Number.prototype.toFixed. The offline client tracker renders GPS "Days
// Covered" with daysCovered.toFixed(1); Go's strconv.FormatFloat rounds halves
// to even, while toFixed rounds halves away from zero (e.g. 3050/8 = 381.25
// renders "381.3" in JS but "381.2" via FormatFloat). We round the float's
// EXACT value (via big.Rat, the same value toFixed sees) half-away-from-zero so
// the dashboard's day counts match the tracker to the digit.
func JSToFixed(x float64, prec int) string {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return strconv.FormatFloat(x, 'f', prec, 64)
	}
	neg := math.Signbit(x) && x != 0
	r := new(big.Rat).SetFloat64(math.Abs(x)) // exact value of the double
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(prec)), nil)
	r.Mul(r, new(big.Rat).SetInt(scale))
	// Round half away from zero: floor(r + 1/2) (r is non-negative here).
	r.Add(r, big.NewRat(1, 2))
	n := new(big.Int).Quo(r.Num(), r.Denom()) // truncate toward zero == floor for r>=0
	digits := n.String()
	var out string
	if prec == 0 {
		out = digits
	} else {
		for len(digits) <= prec {
			digits = "0" + digits
		}
		out = digits[:len(digits)-prec] + "." + digits[len(digits)-prec:]
	}
	if neg {
		// Match toFixed's sign handling, including its "-0.0" for small
		// negatives that round to zero.
		out = "-" + out
	}
	return out
}

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

// InET converts any instant to America/New_York for display — the one ET
// location source, shared with TodayET/NowET (e.g. the data-freshness stamp
// the importer writes in UTC).
func InET(t time.Time) time.Time { return t.In(nyLoc) }

// StatsEpoch is the system go-live date. Aggregate "activity" tallies on the
// console (e.g. the number of violations logged) count only events on or after
// this date, so the overall stats reflect the production era rather than migrated
// history. Per-client records still show each client's full history; current-state
// counts (active roster, behind-on-GPS, balances) reflect the present moment.
// Surfaced in the UI as StatsEpochLabel.
func StatsEpoch() time.Time { return Noon(2026, 6, 1) }

// StatsEpochLabel is the human-readable go-live date shown next to epoch-scoped
// stats so the reporting period is unambiguous.
const StatsEpochLabel = "Jun 1, 2026"

// CheckInDataFloor is the date the office began capturing check-ins digitally.
// Defendants supervised since before this had no digital check-in trail (the
// data starts ~early 2026), so judging them against the full referral-to-today
// window would flood the missed-check-in reports with gaps that reflect ABSENT
// DATA, not non-compliance. Two reporting-layer rules use this floor (the core
// compute math is unchanged, so per-client profile detail still shows every
// window): (1) aggregate "missed" counts/rosters ignore windows whose deadline
// is before the floor; (2) a client with NO check-in records at all is not
// flagged as missing. Mirrors the StatsEpoch pattern.
func CheckInDataFloor() time.Time { return Noon(2026, 3, 1) }
