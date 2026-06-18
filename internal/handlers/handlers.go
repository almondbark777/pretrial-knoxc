// Package handlers wires HTTP routes to the db + compute layers. Handlers are
// thin: load clients (cached 60s), compute server-side, render template or JSON.
package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"html/template"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"pretrial-knoxc/internal/auth"
	"pretrial-knoxc/internal/build"
	"pretrial-knoxc/internal/chat"
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

	// DBPath is the SQLite file path. The CSV upload page passes it to the
	// reconcile tool (which opens its own connection) and stages uploads next
	// to it (importcsv.go). Empty in most tests — only the import flow uses it.
	DBPath string

	// ReconcileExec, when non-nil, replaces the real python reconcile_import.py
	// invocation (importcsv.go) — used by tests to stub the subprocess.
	ReconcileExec func(ctx context.Context, dir string, apply, addsOnly bool) (*ReconcileSummary, string, error)

	// Roles is the DB-backed role cache (app_users). The user-management handlers
	// call Invalidate() after a change so it takes effect immediately. May be nil
	// in tests that don't exercise role management.
	Roles *db.RoleCache

	// Chat is the in-memory group-chat hub (presence + live message fan-out).
	// Set in main after New(); may be nil in tests that don't exercise chat.
	Chat *chat.Hub

	cacheTTL time.Duration
	mu       sync.Mutex
	cached   map[string][]*compute.Client
	cachedAt time.Time

	importMu sync.Mutex // one CSV upload/reconcile at a time
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
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": err.Error(), "version": build.Version})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "db": "up", "version": build.Version})
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

// ── Live lookup search (feeds the console's global search) ────────────────────

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
		// Case-number match scans every case the person has, not just the
		// representative one — officers often start from court paperwork that
		// names one specific case ("@1606962" or just "1606962").
		caseHit := func(cases []*compute.Client) bool {
			for _, cc := range cases {
				if cc.CaseNo != "" && strings.Contains(strings.ToLower(cc.CaseNo), ql) {
					return true
				}
			}
			return false
		}
		for _, cases := range clients { // one hit per IDN (rep = open-preferred)
			c := openRep(cases)
			if c == nil {
				continue
			}
			if strings.Contains(strings.ToLower(c.Name), ql) || strings.HasPrefix(c.IDN, q) || caseHit(cases) {
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

// APIPrefill powers the intake form's IDN autofill. Given ?idn=, it returns the
// identity + case fields we already have for that person — from the same merged,
// deduplicated client set every other view uses (blue book + app-added records) —
// so an officer re-referring an existing defendant doesn't re-key what we already
// know. Dates are normalized to YYYY-MM-DD for the form's date inputs; level is 0
// when unknown. Returns {"found":false} for a brand-new IDN.
// GET /api/prefill
func (s *Server) APIPrefill(w http.ResponseWriter, r *http.Request) {
	idn := strings.TrimSpace(r.URL.Query().Get("idn"))
	if idn == "" {
		writeJSON(w, http.StatusOK, map[string]any{"found": false})
		return
	}
	clients, err := s.clients()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	c := openRep(clients[idn])
	if c == nil {
		writeJSON(w, http.StatusOK, map[string]any{"found": false})
		return
	}
	iso := func(v string) string {
		if t, ok := compute.ParseDay(strings.TrimSpace(v)); ok {
			return t.Format("2006-01-02")
		}
		return ""
	}
	lvl, _ := compute.ParseLevel(c.Level)
	writeJSON(w, http.StatusOK, map[string]any{
		"found":           true,
		"name":            c.Name,
		"birthdate":       iso(c.Birthdate),
		"caseNo":          c.CaseNo,
		"level":           lvl, // 0 = unknown
		"chargeType":      c.ChargeType,
		"bondAmount":      c.BondAmount,
		"supervisionType": c.SupervisionType,
		"orderFrom":       c.OrderFrom,
		"dma":             c.DMA,
		"officer":         c.Officer,
		"status":          c.Status,
		"gps":             c.GpsActive,
		"gpsType":         strings.ToUpper(strings.TrimSpace(c.GpsType)),
		"gpsInstall":      iso(c.GpInstall),
	})
}

// ── Stats + case-grid bundles (JSON) ──────────────────────────────────────────

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

func (s *Server) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.Tmpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), 500)
	}
}
