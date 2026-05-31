// Package auth carries forward the two-gate access model exactly (Brief 4.5):
//  1. Cloudflare Access (upstream) — we trust the Cf-Access-Authenticated-User-Email
//     header when the email is on the allow-list.
//  2. App login — session cookie (12h) OR HTTP Basic fallback, single shared
//     APP_PASSWORD, 22 @knoxsheriff.org allow-list emails.
//
// /health and static assets bypass auth.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/gorilla/sessions"
)

// defaultUsers — the built-in 22 @knoxsheriff.org allow-list (matched
// case-insensitively), ported from webapp/users.py. Used as the FALLBACK when
// ALLOWED_EMAILS is unset, so deployments can override the list via config
// without a rebuild (the email list is no longer hard-wired into behavior).
var defaultUsers = []string{
	"Daniel.Harris@knoxsheriff.org", "Justin.Webber@knoxsheriff.org",
	"Bryan.Hackett@knoxsheriff.org", "natashja.akers@knoxsheriff.org",
	"shellie.medford@knoxsheriff.org", "James.Rexroad@knoxsheriff.org",
	"james.alley@knoxsheriff.org", "Nicholas.Loveless@knoxsheriff.org",
	"William.Dunaway@knoxsheriff.org", "Marcus.Olsen@knoxsheriff.org",
	"Renee.Russell@knoxsheriff.org", "robert.burleson@knoxsheriff.org",
	"william.torbett@knoxsheriff.org", "Carla.Kidwell@knoxsheriff.org",
	"Kathy.Jones@knoxsheriff.org", "chloe.fudge@knoxsheriff.org",
	"Donna.Ogle@knoxsheriff.org", "Tyler.Rickman@knoxsheriff.org",
	"Stoney.Gentry@knoxsheriff.org", "amy.arroyo@knoxsheriff.org",
	"Johnie.Carter@knoxsheriff.org", "alexander.bentley@knoxsheriff.org",
}

// Authenticator gates HTTP requests.
type Authenticator struct {
	allowed     map[string]bool
	supervisors map[string]bool
	password    string
	store       *sessions.CookieStore
}

const sessionName = "kh_sess"

// New builds an Authenticator. sessionSecret signs the cookie (12h lifetime).
// allowedEmails is the @knoxsheriff.org allow-list (from ALLOWED_EMAILS); when
// empty it falls back to the built-in defaultUsers, so an unset env keeps the
// prior 22-user behavior. supervisorEmails is the SUPERVISOR_EMAILS subset that
// may delete / restore / override (Phase 7 roles); entries not on the allow-list
// are ignored — a supervisor must still be an allowed user.
func New(password, sessionSecret string, allowedEmails, supervisorEmails []string) *Authenticator {
	if len(allowedEmails) == 0 {
		allowedEmails = defaultUsers
	}
	allowed := make(map[string]bool, len(allowedEmails))
	for _, u := range allowedEmails {
		if e := strings.ToLower(strings.TrimSpace(u)); e != "" {
			allowed[e] = true
		}
	}
	supervisors := map[string]bool{}
	for _, e := range supervisorEmails {
		e = strings.ToLower(strings.TrimSpace(e))
		if e != "" && allowed[e] {
			supervisors[e] = true
		}
	}
	store := sessions.NewCookieStore([]byte(sessionSecret))
	store.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   60 * 60 * 12, // 12h
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		// TLS terminated upstream by Cloudflare, so Secure is left false here.
	}
	return &Authenticator{allowed: allowed, supervisors: supervisors, password: password, store: store}
}

func (a *Authenticator) IsAllowed(email string) bool {
	return a.allowed[strings.ToLower(strings.TrimSpace(email))]
}

// IsSupervisor reports whether the email is in the supervisor tier (a subset of
// the allow-list). Supervisor-only actions: delete / restore / field overrides.
func (a *Authenticator) IsSupervisor(email string) bool {
	return a.supervisors[strings.ToLower(strings.TrimSpace(email))]
}

// Login validates credentials and writes the session cookie.
func (a *Authenticator) Login(w http.ResponseWriter, r *http.Request, email, password string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	if !a.allowed[email] || !a.checkPassword(password) {
		return false
	}
	sess, _ := a.store.Get(r, sessionName)
	sess.Values["user"] = email
	_ = sess.Save(r, w)
	return true
}

func (a *Authenticator) Logout(w http.ResponseWriter, r *http.Request) {
	sess, _ := a.store.Get(r, sessionName)
	sess.Options.MaxAge = -1
	_ = sess.Save(r, w)
}

// ── CSRF (synchronizer token) ────────────────────────────────────────────────

const csrfKey = "csrf"

// CSRF returns this session's CSRF token, minting + persisting one on first use.
// Form-rendering handlers call this and embed the value as a hidden field; the
// CSRF middleware then checks it on state-changing POSTs. Works for both
// session-cookie and Cf-Access users (the latter get a token-only session cookie).
func (a *Authenticator) CSRF(w http.ResponseWriter, r *http.Request) string {
	sess, _ := a.store.Get(r, sessionName)
	tok, _ := sess.Values[csrfKey].(string)
	if tok == "" {
		tok = randToken()
		sess.Values[csrfKey] = tok
		_ = sess.Save(r, w)
	}
	return tok
}

// ValidCSRF reports whether the request's `csrf` form value matches the session
// token (constant-time). A missing token on either side fails closed.
func (a *Authenticator) ValidCSRF(r *http.Request) bool {
	sess, err := a.store.Get(r, sessionName)
	if err != nil {
		return false
	}
	want, _ := sess.Values[csrfKey].(string)
	got := r.FormValue("csrf")
	if want == "" || got == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// SetCookieSecure flips the session cookie's Secure flag (set via COOKIE_SECURE
// when the browser↔edge hop is HTTPS, e.g. behind Cloudflare — recommended in
// production). Left false by default so plain-HTTP local dev still works.
func (a *Authenticator) SetCookieSecure(secure bool) { a.store.Options.Secure = secure }

func randToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "" // fails ValidCSRF closed — never silently accepts
	}
	return hex.EncodeToString(b)
}

func (a *Authenticator) checkPassword(p string) bool {
	return subtle.ConstantTimeCompare([]byte(p), []byte(a.password)) == 1
}

var publicPrefixes = []string{"/health", "/metrics", "/favicon.ico", "/static/", "/login", "/api/login", "/api/logout"}

func isPublic(path string) bool {
	for _, p := range publicPrefixes {
		if path == p || strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

type ctxKey int

const userKey ctxKey = 0

// User returns the authenticated user email from the request context.
func User(r *http.Request) string {
	if v, ok := r.Context().Value(userKey).(string); ok {
		return v
	}
	return ""
}

// Middleware enforces the gates. resolveUser identifies the caller; on failure
// HTML requests redirect to /login, API requests get 401.
func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isPublic(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		user := a.resolve(r)
		if user != "" {
			ctx := context.WithValue(r.Context(), userKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		if wantsHTML(r) {
			nextPath := r.URL.Path
			if r.URL.RawQuery != "" {
				nextPath += "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, "/login?next="+nextPath, http.StatusSeeOther)
			return
		}
		w.Header().Set("WWW-Authenticate", `Basic realm="Knox County Pre-Trial Services"`)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"Authentication required"}`))
	})
}

func (a *Authenticator) resolve(r *http.Request) string {
	// 1) Cloudflare Access header (trusted upstream identity).
	if email := r.Header.Get("Cf-Access-Authenticated-User-Email"); email != "" {
		if a.IsAllowed(email) {
			return strings.ToLower(strings.TrimSpace(email))
		}
	}
	// 2) Session cookie.
	if sess, err := a.store.Get(r, sessionName); err == nil {
		if u, ok := sess.Values["user"].(string); ok && a.allowed[u] {
			return u
		}
	}
	// 3) HTTP Basic fallback.
	if u := a.basicAuth(r); u != "" {
		return u
	}
	return ""
}

func (a *Authenticator) basicAuth(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(strings.ToLower(h), "basic ") {
		return ""
	}
	dec, err := base64.StdEncoding.DecodeString(h[len("basic "):])
	if err != nil {
		return ""
	}
	user, pass, found := strings.Cut(string(dec), ":")
	if !found {
		return ""
	}
	user = strings.ToLower(strings.TrimSpace(user))
	if a.allowed[user] && a.checkPassword(pass) {
		return user
	}
	return ""
}

func wantsHTML(r *http.Request) bool {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		return false
	}
	accept := r.Header.Get("Accept")
	return accept == "" || accept == "*/*" || strings.Contains(accept, "text/html")
}
