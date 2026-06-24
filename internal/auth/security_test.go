package auth

import (
	"net/http/httptest"
	"testing"
	"time"
)

// TestLoginLimiterBurstAndRefill: the bucket allows `burst` attempts, blocks the
// next one, and lets one through again after a refill interval elapses (using an
// injected clock so the test is fast and deterministic).
func TestLoginLimiterBurstAndRefill(t *testing.T) {
	l := newLoginLimiter(3, time.Second)
	base := time.Unix(0, 0)
	l.now = func() time.Time { return base }

	for i := 0; i < 3; i++ {
		if !l.allow("1.2.3.4") {
			t.Fatalf("attempt %d should be allowed (within burst)", i+1)
		}
	}
	if l.allow("1.2.3.4") {
		t.Fatal("the burst+1 attempt must be rate-limited")
	}
	// A different key has its own budget.
	if !l.allow("5.6.7.8") {
		t.Fatal("a distinct IP should not share the first IP's bucket")
	}
	// One refill interval later, one more token is available for the first key.
	base = base.Add(time.Second)
	if !l.allow("1.2.3.4") {
		t.Fatal("after a refill interval one more attempt should be allowed")
	}
	if l.allow("1.2.3.4") {
		t.Fatal("only one token should have refilled")
	}
}

// TestAllowLoginViaAuthenticator: the (N+1)th attempt is rejected through the
// public Authenticator method, the path both APILogin and basicAuth call.
func TestAllowLoginViaAuthenticator(t *testing.T) {
	a := New("pw", "sec", nil, nil, nil)
	// Drive the limiter to exhaustion for one key.
	allowed, blocked := 0, 0
	for i := 0; i < loginBurst+5; i++ {
		if a.AllowLogin("9.9.9.9") {
			allowed++
		} else {
			blocked++
		}
	}
	if allowed != loginBurst {
		t.Fatalf("allowed %d attempts, want exactly loginBurst=%d", allowed, loginBurst)
	}
	if blocked == 0 {
		t.Fatal("expected later attempts to be blocked once the burst is spent")
	}
}

// TestClientIP prefers CF-Connecting-IP, then the left-most X-Forwarded-For hop,
// then RemoteAddr (host only).
func TestClientIP(t *testing.T) {
	cases := []struct {
		name, cf, xff, remote, want string
	}{
		{"cf-connecting-ip wins", "203.0.113.7", "10.0.0.1", "127.0.0.1:5000", "203.0.113.7"},
		{"xff left-most hop", "", "203.0.113.9, 10.0.0.1", "127.0.0.1:5000", "203.0.113.9"},
		{"remoteaddr fallback", "", "", "198.51.100.4:44321", "198.51.100.4"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/api/login", nil)
			r.RemoteAddr = c.remote
			if c.cf != "" {
				r.Header.Set("CF-Connecting-IP", c.cf)
			}
			if c.xff != "" {
				r.Header.Set("X-Forwarded-For", c.xff)
			}
			if got := ClientIP(r); got != c.want {
				t.Errorf("ClientIP = %q, want %q", got, c.want)
			}
		})
	}
}

// TestCfAccessTrustGate: with trust ON (default) an allow-listed Cf-Access header
// authenticates the request; with trust OFF the header is ignored (resolve returns
// "" — defense-in-depth against a spoofed header on a misconfigured listener).
func TestCfAccessTrustGate(t *testing.T) {
	email := "alexander.bentley@knoxsheriff.org"
	a := New("pw", "sec", nil, nil, nil)

	r := httptest.NewRequest("GET", "/console", nil)
	r.Header.Set("Cf-Access-Authenticated-User-Email", email)
	if got := a.resolve(r); got != email {
		t.Fatalf("trust on: resolve = %q, want %q", got, email)
	}

	a.SetTrustCfAccess(false)
	if got := a.resolve(r); got != "" {
		t.Fatalf("trust off: resolve = %q, want \"\" (header must be ignored)", got)
	}
}

// TestSessionCookieRejectedUnderDifferentSecret is the #6 guarantee: a session
// cookie minted under session-secret A must NOT authenticate under an
// Authenticator built with a different secret B. This is why a predictable
// (password-derived, default-password) secret is dangerous — anyone who can
// recompute it can forge a cookie.
func TestSessionCookieRejectedUnderDifferentSecret(t *testing.T) {
	email := "alexander.bentley@knoxsheriff.org"
	aA := New("pw", "secret-A-0000000000000000000000000000", nil, nil, nil)
	aB := New("pw", "secret-B-1111111111111111111111111111", nil, nil, nil)

	// Mint a cookie under secret A.
	wr := httptest.NewRecorder()
	loginReq := httptest.NewRequest("POST", "/api/login", nil)
	if !aA.Login(wr, loginReq, email, "pw") {
		t.Fatal("Login under secret A should succeed for an allow-listed user")
	}
	cookies := wr.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected a session cookie to be set under secret A")
	}

	// The same cookie authenticates under A …
	rA := httptest.NewRequest("GET", "/console", nil)
	for _, c := range cookies {
		rA.AddCookie(c)
	}
	if got := aA.resolve(rA); got != email {
		t.Fatalf("cookie minted under A should authenticate under A; got %q", got)
	}

	// … but is rejected under B (different signing key → no identity).
	rB := httptest.NewRequest("GET", "/console", nil)
	for _, c := range cookies {
		rB.AddCookie(c)
	}
	if got := aB.resolve(rB); got != "" {
		t.Fatalf("cookie minted under A must NOT authenticate under B; got %q", got)
	}
}
