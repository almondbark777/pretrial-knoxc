// ratelimit.go is a small in-memory limiter for the PUBLIC, unauthenticated
// check-in POST (/checkin/submit). Everything else on the app sits behind
// Cloudflare Access + app login; this endpoint is open by design (a client
// scans the lobby QR), so it needs its own abuse guard: a burst cap and a
// minimum spacing per source IP. It is best-effort and process-local — enough to
// blunt a script hammering submissions, not a distributed-DoS shield (that's
// Cloudflare's job upstream).
package handlers

import (
	"sync"
	"time"
)

// submitLimiter enforces, per IP: at most `burst` submissions within `window`,
// and at least `minGap` between consecutive submissions.
type submitLimiter struct {
	mu     sync.Mutex
	hits   map[string][]time.Time
	burst  int
	window time.Duration
	minGap time.Duration
	now    func() time.Time // injectable for tests
}

func newSubmitLimiter(burst int, window, minGap time.Duration) *submitLimiter {
	return &submitLimiter{
		hits:   make(map[string][]time.Time),
		burst:  burst,
		window: window,
		minGap: minGap,
		now:    time.Now,
	}
}

// allow records an attempt from ip and reports whether it's permitted, plus the
// seconds to wait before retrying when it isn't.
func (l *submitLimiter) allow(ip string) (bool, int) {
	if ip == "" {
		ip = "unknown"
	}
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()

	// Opportunistic sweep so the map can't grow unbounded across many IPs.
	if len(l.hits) > 4096 {
		for k, ts := range l.hits {
			if len(ts) == 0 || now.Sub(ts[len(ts)-1]) > l.window {
				delete(l.hits, k)
			}
		}
	}

	// Prune this IP's stamps outside the window.
	cutoff := now.Add(-l.window)
	kept := l.hits[ip][:0]
	for _, t := range l.hits[ip] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}

	// Minimum spacing between consecutive submissions.
	if n := len(kept); n > 0 {
		if gap := now.Sub(kept[n-1]); gap < l.minGap {
			l.hits[ip] = kept
			return false, int((l.minGap - gap).Seconds()) + 1
		}
	}
	// Burst cap within the window.
	if len(kept) >= l.burst {
		retry := int(kept[0].Add(l.window).Sub(now).Seconds()) + 1
		l.hits[ip] = kept
		return false, retry
	}

	l.hits[ip] = append(kept, now)
	return true, 0
}
