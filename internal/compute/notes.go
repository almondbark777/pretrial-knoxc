package compute

import (
	"regexp"
	"strings"
)

// Port of the v0.73 GPS-notes helpers stripHtml + isFeesWaived.

var (
	reTags1   = regexp.MustCompile(`(?i)</?(p|br|div|span|strong|em|b|i|u)[^>]*>`)
	reTags2   = regexp.MustCompile(`<[^>]+>`)
	reSpaces  = regexp.MustCompile(`\s+`)
	reWaiv    = regexp.MustCompile(`(?i)waiv`)
	reWaivCtx = regexp.MustCompile(`(?i)(fee|gps|payment|charge)`)
)

// StripHtml strips simple HTML and decodes a few entities (matches the JS).
func StripHtml(s string) string {
	if s == "" {
		return ""
	}
	s = reTags1.ReplaceAllString(s, " ")
	s = reTags2.ReplaceAllString(s, "")
	r := strings.NewReplacer("&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">")
	s = r.Replace(s)
	s = reSpaces.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// IsFeesWaived: notes contain /waiv/i AND /(fee|gps|payment|charge)/i.
func IsFeesWaived(notes string) bool {
	t := StripHtml(notes)
	if t == "" {
		return false
	}
	return reWaiv.MatchString(t) && reWaivCtx.MatchString(t)
}
