package handlers

import (
	"testing"
	"time"
)

func TestSubmitLimiter(t *testing.T) {
	base := time.Date(2026, 6, 26, 9, 0, 0, 0, time.UTC)
	cur := base
	l := newSubmitLimiter(3, 10*time.Minute, 15*time.Second)
	l.now = func() time.Time { return cur }

	// First submission allowed.
	if ok, _ := l.allow("1.2.3.4"); !ok {
		t.Fatal("first submit should be allowed")
	}
	// Immediate second submission blocked by min-gap.
	if ok, retry := l.allow("1.2.3.4"); ok || retry <= 0 {
		t.Fatalf("min-gap should block; ok=%v retry=%d", ok, retry)
	}
	// A different IP is independent.
	if ok, _ := l.allow("9.9.9.9"); !ok {
		t.Fatal("different IP should be allowed")
	}
	// After the gap, allowed again.
	cur = cur.Add(20 * time.Second)
	if ok, _ := l.allow("1.2.3.4"); !ok {
		t.Fatal("should be allowed after the gap")
	}
	cur = cur.Add(20 * time.Second)
	if ok, _ := l.allow("1.2.3.4"); !ok {
		t.Fatal("third within window allowed")
	}
	// Fourth within the window exceeds the burst cap.
	cur = cur.Add(20 * time.Second)
	if ok, retry := l.allow("1.2.3.4"); ok || retry <= 0 {
		t.Fatalf("burst cap should block; ok=%v retry=%d", ok, retry)
	}
	// Once the window rolls past, the burst resets.
	cur = cur.Add(11 * time.Minute)
	if ok, _ := l.allow("1.2.3.4"); !ok {
		t.Fatal("burst should reset after the window")
	}
}
