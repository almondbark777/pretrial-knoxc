package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"pretrial-knoxc/internal/auth"
)

// TestLoginRedirectSafeNext is the open-redirect guard (#5/#27): sanitizeNext
// only passes unambiguous same-origin absolute paths; everything else collapses
// to the default. backOr/safeNext delegate to it, so the table covers all three
// callers (LoginPage reflected, APILogin form+JSON, the 403 Back link).
func TestLoginRedirectSafeNext(t *testing.T) {
	const def = "/"
	cases := []struct {
		in, want string
	}{
		{"/console", "/console"},
		{"/console/clients/123?x=1", "/console/clients/123?x=1"},
		{"", def},
		{"  ", def},
		{"//evil.com", def},                // schema-relative → off-site host
		{"///evil.com", def},               // collapses to //
		{"/\\evil.com", def},               // backslash smuggling (browsers treat \ as /)
		{"http://evil.com", def},           // absolute URL
		{"https://evil.com/path", def},     // absolute URL
		{"javascript:alert(1)", def},       // scheme, no leading slash
		{"evil.com", def},                  // bare host
		{"\\\\evil.com", def},              // UNC-style
		{"/legit//inner", "/legit//inner"}, // // not at the front is fine (same-origin path)
	}
	for _, c := range cases {
		if got := sanitizeNext(c.in, def); got != c.want {
			t.Errorf("sanitizeNext(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// loginServer builds a Server with an allow-listed user and a known password, with
// the login limiter fresh (a new Authenticator each call). No DB/templates needed:
// APILogin renders JSON only.
func loginServer(t *testing.T) *Server {
	t.Helper()
	a := auth.New("pw", "secret-1234567890123456789012345678", nil, nil, nil)
	return New(nil, a, nil, time.Minute, false)
}

// TestAPILoginMalformedJSON (#26): a bad JSON body is a 400 with a clear error,
// not a silent 401.
func TestAPILoginMalformedJSON(t *testing.T) {
	srv := loginServer(t)
	req := httptest.NewRequest("POST", "/api/login", strings.NewReader("{not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.APILogin(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed JSON: status = %d, want 400 (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.OK || resp.Error == "" {
		t.Errorf("malformed JSON: body = %s, want ok:false + error", rec.Body.String())
	}
}

// TestAPILoginGoodAndBad: a good login is 200 with a sanitized redirect; a wrong
// password is 401. Also confirms an off-site ?next is scrubbed to "/".
func TestAPILoginGoodAndBad(t *testing.T) {
	srv := loginServer(t)
	email := "alexander.bentley@knoxsheriff.org"

	good := func(next string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"email": email, "password": "pw", "next": next})
		req := httptest.NewRequest("POST", "/api/login", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		srv.APILogin(rec, req)
		return rec
	}

	rec := good("/console")
	if rec.Code != http.StatusOK {
		t.Fatalf("good login: status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		OK       bool   `json:"ok"`
		Redirect string `json:"redirect"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if !resp.OK || resp.Redirect != "/console" {
		t.Errorf("good login: %s, want ok:true redirect:/console", rec.Body.String())
	}

	// Off-site next is sanitized to "/".
	rec = good("//evil.com")
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Redirect != "/" {
		t.Errorf("off-site next: redirect = %q, want /", resp.Redirect)
	}

	// Wrong password → 401.
	body, _ := json.Marshal(map[string]string{"email": email, "password": "wrong", "next": "/"})
	req := httptest.NewRequest("POST", "/api/login", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "10.0.0.55:1" // distinct IP so the limiter budget is fresh
	rec = httptest.NewRecorder()
	srv.APILogin(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong password: status = %d, want 401", rec.Code)
	}
}

// TestAPILoginRateLimited (#15): only FAILED attempts burn the brute-force budget.
// A flood of wrong passwords from one IP eventually trips 429 — but a CORRECT login
// is never rate-limited, so a shared-NAT office can't be locked out by legit sign-ins.
func TestAPILoginRateLimited(t *testing.T) {
	srv := loginServer(t)
	email := "alexander.bentley@knoxsheriff.org"
	attempt := func(pw string) int {
		body, _ := json.Marshal(map[string]string{"email": email, "password": pw, "next": "/"})
		req := httptest.NewRequest("POST", "/api/login", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("CF-Connecting-IP", "203.0.113.200") // one shared key
		rec := httptest.NewRecorder()
		srv.APILogin(rec, req)
		return rec.Code
	}
	// Wrong passwords burn tokens; after the burst the next failure is 429.
	var got429 bool
	for i := 0; i < 80; i++ { // > loginBurst (50)
		if attempt("wrong") == http.StatusTooManyRequests {
			got429 = true
			break
		}
	}
	if !got429 {
		t.Fatal("expected a 429 once the per-IP FAILED-login burst is exhausted")
	}
	// Even with the bucket drained by failures, a correct login still succeeds —
	// legitimate users must never be locked out.
	if code := attempt("pw"); code != http.StatusOK {
		t.Fatalf("correct login after burst = %d, want 200 (legit users must not be locked out)", code)
	}
}

// TestAPILoginFormBranchNextSanitized covers the form (non-JSON) branch of #5.
func TestAPILoginFormBranchNextSanitized(t *testing.T) {
	srv := loginServer(t)
	form := url.Values{
		"email":    {"alexander.bentley@knoxsheriff.org"},
		"password": {"pw"},
		"next":     {"http://evil.com"},
	}
	req := httptest.NewRequest("POST", "/api/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.APILogin(rec, req)
	var resp struct {
		Redirect string `json:"redirect"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Redirect != "/" {
		t.Errorf("form-branch off-site next: redirect = %q, want /", resp.Redirect)
	}
}
