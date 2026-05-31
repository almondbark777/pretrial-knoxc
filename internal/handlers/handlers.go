// Package handlers wires HTTP routes to the db + compute layers. Handlers are
// thin: load clients (cached 60s), compute server-side, render template or JSON.
package handlers

import (
	"database/sql"
	"encoding/json"
	"html/template"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"pretrial-knoxc/internal/auth"
	"pretrial-knoxc/internal/compute"
	"pretrial-knoxc/internal/db"
	"pretrial-knoxc/internal/models"
)

// Server holds dependencies for the HTTP handlers.
type Server struct {
	DB   *sql.DB
	Auth *auth.Authenticator
	Tmpl *template.Template

	// ImporterRetired flips Delete from a tombstone (importer-proof) to a plain
	// physical raw_* row delete at SharePoint cutover (Brief: IMPORTER_RETIRED).
	ImporterRetired bool

	cacheTTL time.Duration
	mu       sync.Mutex
	cached   map[string][]*compute.Client
	cachedAt time.Time
}

// New builds a Server.
func New(db *sql.DB, a *auth.Authenticator, tmpl *template.Template, ttl time.Duration, importerRetired bool) *Server {
	return &Server{DB: db, Auth: a, Tmpl: tmpl, cacheTTL: ttl, ImporterRetired: importerRetired}
}

// clients returns the joined client set (idn -> all case rows), rebuilt at most
// once per TTL.
func (s *Server) clients() (map[string][]*compute.Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cached != nil && time.Since(s.cachedAt) < s.cacheTTL {
		return s.cached, nil
	}
	cl, err := db.BuildClients(s.DB, compute.TodayET())
	if err != nil {
		return nil, err
	}
	s.cached = cl
	s.cachedAt = time.Now()
	return cl, nil
}

func (s *Server) clearCache() {
	s.mu.Lock()
	s.cached = nil
	s.mu.Unlock()
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// ── Health (auth-free) ────────────────────────────────────────────────────────

func (s *Server) Health(w http.ResponseWriter, r *http.Request) {
	if err := s.DB.Ping(); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "db": "up"})
}

// ── Auth pages ────────────────────────────────────────────────────────────────

func (s *Server) LoginPage(w http.ResponseWriter, r *http.Request) {
	next := r.URL.Query().Get("next")
	if next == "" {
		next = "/"
	}
	s.render(w, "login.html", map[string]any{"Next": next, "Error": r.URL.Query().Get("err")})
}

func (s *Server) APILogin(w http.ResponseWriter, r *http.Request) {
	var body struct{ Email, Password, Next string }
	ct := r.Header.Get("Content-Type")
	if strings.Contains(ct, "application/json") {
		_ = json.NewDecoder(r.Body).Decode(&body)
	} else {
		_ = r.ParseForm()
		body.Email = r.FormValue("email")
		body.Password = r.FormValue("password")
		body.Next = r.FormValue("next")
	}
	if body.Next == "" {
		body.Next = "/"
	}
	if !s.Auth.Login(w, r, body.Email, body.Password) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "Invalid email or password"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "redirect": body.Next})
}

func (s *Server) APILogout(w http.ResponseWriter, r *http.Request) {
	s.Auth.Logout(w, r)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "redirect": "/login"})
}

func (s *Server) APIRefresh(w http.ResponseWriter, r *http.Request) {
	s.clearCache()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ── Landing page: the existing client tracker (stays the main page) ───────────

// Home renders the thin shell that frames the bundled "PTR Client Lookup" tracker
// in an iframe. The tracker is the front door during the transition; a top-bar
// button leads into the new admin & data-entry app. The tracker itself is served
// untouched from /static/lookup/ and fed by /api/lookup_data.
func (s *Server) Home(w http.ResponseWriter, r *http.Request) {
	s.render(w, "shell.html", map[string]any{
		"User":         auth.User(r),
		"IsSupervisor": s.Auth.IsSupervisor(auth.User(r)),
	})
}

// APILookupData feeds the bundled tracker the four datasets (bb/ci/pm/gp) — the
// Go reimplementation of the Python /api/lookup_data, with tombstones/overrides
// applied so the tracker stays consistent with every other view.
func (s *Server) APILookupData(w http.ResponseWriter, r *http.Request) {
	data, err := db.LookupDatasets(s.DB)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, data)
}

// ── Dashboard (server-side stats + rosters) — the new app's home (/dashboard) ──

func (s *Server) Dashboard(w http.ResponseWriter, r *http.Request) {
	clients, err := s.clients()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	track := compute.TodayET()
	user := auth.User(r)
	s.render(w, "index.html", map[string]any{
		"User":         user,
		"IsSupervisor": s.Auth.IsSupervisor(user),
		"Today":        track.Format("January 2, 2006"),
		"Stats":        computeStats(clients, track),
		"Behind":       behindRoster(clients, track),
		"Missed":       missedCheckInsRoster(clients, track),
		"Msg":          r.URL.Query().Get("msg"),
	})
}

// ── My Day (the logged-in officer's personal worklist) ────────────────────────

func (s *Server) MyDay(w http.ResponseWriter, r *http.Request) {
	clients, err := s.clients()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	track := compute.TodayET()
	user := auth.User(r)
	s.render(w, "my_day.html", map[string]any{
		"User":         user,
		"IsSupervisor": s.Auth.IsSupervisor(user),
		"Today":        track.Format("January 2, 2006"),
		"MD":           myDay(clients, track, compute.FmtOfficer(user)),
	})
}

// ── Live lookup search ────────────────────────────────────────────────────────

func (s *Server) APILookup(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	clients, err := s.clients()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	hits := []models.SearchHit{}
	if len(q) >= 2 {
		ql := strings.ToLower(q)
		for _, cases := range clients { // one hit per IDN (rep = open-preferred)
			c := openRep(cases)
			if c == nil {
				continue
			}
			if strings.Contains(strings.ToLower(c.Name), ql) || strings.HasPrefix(c.IDN, q) {
				lvl, _ := compute.ParseLevel(c.Level)
				hits = append(hits, models.SearchHit{
					IDN: c.IDN, Name: c.Name, Status: c.Status, Level: lvl,
					Officer: c.Officer, CaseNum: c.CaseNo,
				})
			}
		}
		sort.Slice(hits, func(i, j int) bool { return hits[i].Name < hits[j].Name })
		if len(hits) > 25 {
			hits = hits[:25]
		}
	}
	writeJSON(w, http.StatusOK, hits)
}

// ── Client profile (server-rendered, server-side math) ────────────────────────

func (s *Server) ClientProfile(w http.ResponseWriter, r *http.Request) {
	idn := strings.TrimSpace(r.URL.Query().Get("idn"))
	clients, err := s.clients()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	track := compute.TodayET()
	user := auth.User(r)
	cases := clients[idn]
	if len(cases) == 0 {
		s.render(w, "profile.html", map[string]any{
			"User": user, "NotFound": idn, "IsSupervisor": s.Auth.IsSupervisor(user),
			"Msg": r.URL.Query().Get("msg"),
		})
		return
	}
	c, caseFilter := selectCase(cases, r.URL.Query().Get("case"))
	ci := compute.ComputeCheckIns(*c, track)
	ptr := compute.ComputePTRFees(*c, track, caseFilter)
	gps := compute.ComputeGPS(*c, track, nil, caseFilter)
	extras, err := db.LoadExtras(s.DB, idn)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.render(w, "profile.html", map[string]any{
		"User":              user,
		"IsSupervisor":      s.Auth.IsSupervisor(user),
		"CSRF":              s.Auth.CSRF(w, r),
		"C":                 c,
		"CI":                ci,
		"PTR":               ptr,
		"GPS":               gps,
		"Today":             track.Format("January 2, 2006"),
		"Waived":            compute.IsFeesWaived(c.GpNotes),
		"Cases":             caseOptions(cases),
		"SelectedCase":      caseFilter,
		"Extras":            extras,
		"OverridableFields": db.OverridableFields(),
		"Msg":               r.URL.Query().Get("msg"),
	})
}

// APIClient returns one client's full computed bundle as JSON (for API users /
// future SPA). Demonstrates the server-side math is the single source of truth.
func (s *Server) APIClient(w http.ResponseWriter, r *http.Request) {
	idn := strings.TrimSpace(r.URL.Query().Get("idn"))
	clients, err := s.clients()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	track := compute.TodayET()
	cases := clients[idn]
	if len(cases) == 0 {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
		return
	}
	c, caseFilter := selectCase(cases, r.URL.Query().Get("case"))
	writeJSON(w, http.StatusOK, map[string]any{
		"idn":      c.IDN,
		"name":     c.Name,
		"checkIns": compute.ComputeCheckIns(*c, track),
		"ptr":      compute.ComputePTRFees(*c, track, caseFilter),
		"gps":      compute.ComputeGPS(*c, track, nil, caseFilter),
	})
}

// ── Dashboard stats + case-management bundle (JSON) ───────────────────────────

func (s *Server) APIStats(w http.ResponseWriter, r *http.Request) {
	clients, err := s.clients()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, http.StatusOK, computeStats(clients, compute.TodayET()))
}

func (s *Server) APIDefendants(w http.ResponseWriter, r *http.Request) {
	clients, err := s.clients()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, http.StatusOK, defendantRows(clients, compute.TodayET()))
}

// ── Case management grid (server-rendered) ────────────────────────────────────

func (s *Server) CaseManagement(w http.ResponseWriter, r *http.Request) {
	clients, err := s.clients()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	track := compute.TodayET()
	user := auth.User(r)
	s.render(w, "pretrial_app.html", map[string]any{
		"User":         user,
		"IsSupervisor": s.Auth.IsSupervisor(user),
		"Today":        track.Format("January 2, 2006"),
		"Rows":         defendantRows(clients, track),
		"Stats":        computeStats(clients, track),
	})
}

// ── Analytics (server-rendered, dependency-free CSS bars) ──────────────────────

func (s *Server) Analytics(w http.ResponseWriter, r *http.Request) {
	clients, err := s.clients()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	track := compute.TodayET()
	user := auth.User(r)
	s.render(w, "analytics.html", map[string]any{
		"User":         user,
		"IsSupervisor": s.Auth.IsSupervisor(user),
		"Today":        track.Format("January 2, 2006"),
		"A":            analyticsData(clients, track),
	})
}

// ── Per-client calendar (month grid) ──────────────────────────────────────────

func (s *Server) Calendar(w http.ResponseWriter, r *http.Request) {
	clients, err := s.clients()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	track := compute.TodayET()
	year, month := track.Year(), track.Month()
	if mp := r.URL.Query().Get("month"); mp != "" {
		if t, e := time.Parse("2006-01", mp); e == nil {
			year, month = t.Year(), t.Month()
		}
	}
	cur := compute.Noon(year, month, 1)
	prev, next := cur.AddDate(0, -1, 0).Format("2006-01"), cur.AddDate(0, 1, 0).Format("2006-01")
	user := auth.User(r)

	// No idn → roster mode: aggregated team calendar across all clients (Brief 2.9).
	idn := strings.TrimSpace(r.URL.Query().Get("idn"))
	if idn == "" {
		rc := rosterCalendarMonth(clients, track, year, month)
		s.render(w, "calendar.html", map[string]any{
			"User": user, "IsSupervisor": s.Auth.IsSupervisor(user),
			"Roster": true, "RC": rc, "Title": rc.Title,
			"PrevMonth": prev, "NextMonth": next,
		})
		return
	}

	// idn present → per-client calendar (existing behavior).
	cases := clients[idn]
	if len(cases) == 0 {
		s.render(w, "calendar.html", map[string]any{"User": user, "NotFound": idn})
		return
	}
	c, _ := selectCase(cases, r.URL.Query().Get("case"))
	title, days := calendarMonth(c, track, year, month)
	s.render(w, "calendar.html", map[string]any{
		"User":      user,
		"C":         c,
		"Title":     title,
		"Days":      days,
		"PrevMonth": prev,
		"NextMonth": next,
	})
}

func (s *Server) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.Tmpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), 500)
	}
}
