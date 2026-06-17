package compute

import (
	"testing"
	"time"
)

func TestFirstCheckInDue(t *testing.T) {
	cases := []struct {
		name string
		y    int
		m    time.Month
		d    int
		wy   int
		wm   time.Month
		wd   int
	}{
		// Alex's example: Thu 18-Jun-2026 → end of business Wed 24-Jun-2026.
		// Fri 19-Jun (Juneteenth) + the weekend don't count.
		{"juneteenth+weekend", 2026, time.June, 18, 2026, time.June, 24},
		// July 4 2026 is a Saturday → observed Friday Jul 3. Thu 2-Jul → Wed 8-Jul.
		{"independence-day-observed-fri", 2026, time.July, 2, 2026, time.July, 8},
		// A plain week with no holidays: Mon 3-Aug → Thu 6-Aug.
		{"plain-week", 2026, time.August, 3, 2026, time.August, 6},
		// Spanning a weekend: Fri 7-Aug → Wed 12-Aug.
		{"spans-weekend", 2026, time.August, 7, 2026, time.August, 12},
	}
	for _, c := range cases {
		got := FirstCheckInDue(Noon(c.y, c.m, c.d))
		want := Noon(c.wy, c.wm, c.wd)
		if !got.Equal(want) {
			t.Errorf("%s: FirstCheckInDue(%v) = %s, want %s", c.name,
				Noon(c.y, c.m, c.d).Format("2006-01-02"), got.Format("2006-01-02 Mon"), want.Format("2006-01-02 Mon"))
		}
	}
}

func TestAddBusinessDaysObservedHolidays(t *testing.T) {
	// New Year's Day 2023 is a Sunday → observed Mon 2-Jan-2023. From Fri 30-Dec-2022,
	// the next business day is Tue 3-Jan-2023.
	if got := AddBusinessDays(Noon(2022, time.December, 30), 1); !got.Equal(Noon(2023, time.January, 3)) {
		t.Errorf("Sunday→Monday observed: got %s, want 2023-01-03", got.Format("2006-01-02 Mon"))
	}
	// New Year's Day 2022 is a Saturday → observed Fri 31-Dec-2021. From Thu 30-Dec-2021,
	// the next business day is Mon 3-Jan-2022 (exercises the Dec-31 cross-year branch).
	if got := AddBusinessDays(Noon(2021, time.December, 30), 1); !got.Equal(Noon(2022, time.January, 3)) {
		t.Errorf("Saturday→Friday(Dec31) observed: got %s, want 2022-01-03", got.Format("2006-01-02 Mon"))
	}
}

func TestAddBusinessDaysInvariants(t *testing.T) {
	// Over a full year, n business days out always lands on a business day strictly
	// after the start, for several n.
	start := Noon(2026, time.January, 1)
	for i := 0; i < 366; i++ {
		s := addDays(start, i)
		for _, n := range []int{1, 3, 5} {
			got := AddBusinessDays(s, n)
			if !got.After(s) {
				t.Fatalf("AddBusinessDays(%s, %d) = %s not after start", s.Format("2006-01-02"), n, got.Format("2006-01-02"))
			}
			if !IsBusinessDay(got) {
				t.Fatalf("AddBusinessDays(%s, %d) = %s is not a business day", s.Format("2006-01-02"), n, got.Format("2006-01-02 Mon"))
			}
		}
	}
}
