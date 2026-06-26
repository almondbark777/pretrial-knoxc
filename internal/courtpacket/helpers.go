package courtpacket

import (
	"encoding/json"
	"fmt"
	"strings"
)

// nz returns def when s is blank.
func nz(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

func gpsGranted(perm string) bool { return strings.EqualFold(strings.TrimSpace(perm), "granted") }

// presenceLabel turns the stored badge into a plain-English assessment.
func presenceLabel(badge string) string {
	switch strings.ToLower(strings.TrimSpace(badge)) {
	case "green":
		return "On-site (within the office geofence)"
	case "yellow":
		return "Unverified (location not usable)"
	case "red":
		return "Off-site (outside the office geofence)"
	default:
		return "Unscored"
	}
}

func weekCodeLabel(valid bool) string {
	if valid {
		return "current"
	}
	return "stale / unknown — flagged"
}

// distLabel renders meters as feet (<1 mi) or miles; "—" when GPS wasn't usable.
func distLabel(meters float64, gpsOK bool) string {
	if !gpsOK || meters <= 0 {
		return "—"
	}
	if feet := meters * 3.28084; feet < 5280 {
		return fmt.Sprintf("%.0f ft", feet)
	}
	return fmt.Sprintf("%.1f mi", meters/1609.344)
}

// parseFlags turns the JSON flags column into a display slice.
func parseFlags(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var out []string
	if json.Unmarshal([]byte(raw), &out) != nil {
		return nil
	}
	return out
}

func yesNo(b bool, date string) string {
	if !b {
		return "No"
	}
	if strings.TrimSpace(date) != "" {
		return "YES (" + date + ")"
	}
	return "YES"
}

func unemp(length string) string {
	if strings.TrimSpace(length) == "" {
		return ""
	}
	return "unemployed " + length
}

// typedName extracts the human-readable name from signature_data, which for a
// drawn signature carries a trailing " · drawn:sha256:…" provenance tag.
func typedName(sigData string) string {
	if i := strings.Index(sigData, " · drawn:"); i >= 0 {
		return strings.TrimSpace(sigData[:i])
	}
	return nz(sigData, "—")
}

func integrityNote(verified bool) string {
	if verified {
		return "verified — image matches the digest sealed in the record"
	}
	return "WARNING — image does NOT match the sealed digest"
}

func hashLabel(h string) string {
	h = strings.TrimSpace(h)
	if h == "" {
		return "(none — first record in the chain)"
	}
	return h
}

// joinNonEmpty joins the non-blank parts with sep.
func joinNonEmpty(sep string, parts ...string) string {
	var keep []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			keep = append(keep, strings.TrimSpace(p))
		}
	}
	if len(keep) == 0 {
		return "—"
	}
	return strings.Join(keep, sep)
}

// fmtOfficer turns an officer email into a display name ("First Last"), matching
// the rest of the app's convention without importing the compute package.
func fmtOfficer(email string) string {
	email = strings.TrimSpace(email)
	if email == "" {
		return "—"
	}
	local := email
	if i := strings.IndexByte(email, '@'); i >= 0 {
		local = email[:i]
	}
	if local == "" {
		return email
	}
	return strings.Title(strings.ReplaceAll(local, ".", " ")) // nolint:staticcheck — ASCII names
}

// wrap breaks s into lines no wider than maxW points at the given font size,
// using a conservative average Helvetica advance so text never overruns the
// right margin (it errs toward wrapping a touch early).
func wrap(s string, size, maxW float64) []string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\r", ""))
	if s == "" {
		return nil
	}
	perChar := size * 0.55 // generous average advance
	maxChars := int(maxW / perChar)
	if maxChars < 8 {
		maxChars = 8
	}
	var lines []string
	for _, para := range strings.Split(s, "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}
		cur := ""
		for _, wd := range words {
			// Hard-break a single token longer than the line.
			for len(wd) > maxChars {
				if cur != "" {
					lines = append(lines, cur)
					cur = ""
				}
				lines = append(lines, wd[:maxChars])
				wd = wd[maxChars:]
			}
			switch {
			case cur == "":
				cur = wd
			case len(cur)+1+len(wd) <= maxChars:
				cur += " " + wd
			default:
				lines = append(lines, cur)
				cur = wd
			}
		}
		if cur != "" {
			lines = append(lines, cur)
		}
	}
	return lines
}
