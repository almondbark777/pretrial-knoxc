package handlers

import "testing"

// TestCombineDateTime: the picked date carries the officer's clock when a valid
// HH:MM is supplied, and is left as a date-only value otherwise (blank or junk).
func TestCombineDateTime(t *testing.T) {
	cases := []struct{ date, tm, want string }{
		{"2026-06-26", "14:30", "2026-06-26 14:30"}, // valid 24h time appended
		{"2026-06-26", "09:05", "2026-06-26 09:05"},
		{"2026-06-26", "", "2026-06-26"},      // no time -> date only
		{"2026-06-26", "bogus", "2026-06-26"}, // malformed -> date only
		{"2026-06-26", "25:99", "2026-06-26"}, // out of range -> date only
		{" 2026-06-26 ", " 7:00 ", "2026-06-26 7:00"},
	}
	for _, c := range cases {
		if got := combineDateTime(c.date, c.tm); got != c.want {
			t.Errorf("combineDateTime(%q,%q) = %q, want %q", c.date, c.tm, got, c.want)
		}
	}
}

// TestStampWithTime: a stored check-in with a clock shows the time; a plain date
// shows date only (no spurious noon), and junk degrades gracefully.
func TestStampWithTime(t *testing.T) {
	cases := []struct{ in, want string }{
		{"6/26/2026 14:30", "Jun 26, 2026 · 2:30 PM"},
		{"2026-06-26 09:05", "Jun 26, 2026 · 9:05 AM"},
		{"6/26/2026", "Jun 26, 2026"}, // date only -> no time
		{"", "—"},                     // blank -> dash
	}
	for _, c := range cases {
		if got := stampWithTime(c.in); got != c.want {
			t.Errorf("stampWithTime(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
