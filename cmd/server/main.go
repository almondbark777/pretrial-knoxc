// Command server is the single-binary Knox County Pre-Trial web app.
//
//	go build ./cmd/server   ->   ./server
//
// Listens on 127.0.0.1:8000 (cloudflared reaches it locally). Native SQLite, no
// Python, no T-SQL shim. Business math is computed server-side (internal/compute).
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "time/tzdata" // embed tz database so America/New_York works on any host

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"pretrial-knoxc/internal/auth"
	"pretrial-knoxc/internal/db"
	"pretrial-knoxc/internal/handlers"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// envList splits a comma/whitespace-separated env var (e.g. SUPERVISOR_EMAILS)
// into trimmed, non-empty entries.
func envList(k string) []string {
	v := os.Getenv(k)
	if strings.TrimSpace(v) == "" {
		return nil
	}
	fields := strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' })
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// envBool reads a boolean-ish env var (true/1/yes), default false.
func envBool(k string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(k))) {
	case "true", "1", "yes", "on":
		return true
	}
	return false
}

func main() {
	addr := env("LISTEN_ADDR", "127.0.0.1:8000")
	dbPath := env("SQLITE_DB_PATH", "/opt/ptr-knoxc/db/pretrial_release.db")
	password := env("APP_PASSWORD", "pretrialtestsite")
	secret := os.Getenv("APP_SESSION_SECRET")
	if secret == "" {
		// Derive from the password if unset (sessions reset on rotation) — same
		// behavior as the Python app. Set it explicitly in production.
		h := sha256.Sum256([]byte("kh-session::" + password))
		secret = hex.EncodeToString(h[:])
	}

	database, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("open db %s: %v", dbPath, err)
	}
	defer database.Close()

	// Self-provision the admin + extension tables (idempotent CREATE IF NOT EXISTS).
	if err := db.EnsureSchema(database); err != nil {
		log.Fatalf("ensure schema: %v", err)
	}

	base := baseDir()
	tmpl, err := template.New("").Funcs(tmplFuncs()).ParseGlob(filepath.Join(base, "templates", "*.html"))
	if err != nil {
		log.Fatalf("parse templates: %v", err)
	}

	importerRetired := envBool("IMPORTER_RETIRED")
	a := auth.New(password, secret, envList("ALLOWED_EMAILS"), envList("SUPERVISOR_EMAILS"))
	srv := handlers.New(database, a, tmpl, 60*time.Second, importerRetired)

	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)
	r.Use(a.Middleware)

	// Static assets (public).
	staticDir := filepath.Join(base, "static")
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir(staticDir))))

	// Auth-free.
	r.Get("/health", srv.Health)

	// Auth pages.
	r.Get("/login", srv.LoginPage)
	r.Post("/api/login", srv.APILogin)
	r.Post("/api/logout", srv.APILogout)

	// Landing page = the existing client tracker (shell + iframe); the new app
	// lives at /dashboard, reached via a top-bar button. Keeps the two separate.
	r.Get("/", srv.Home)
	r.Get("/api/lookup_data", srv.APILookupData)

	// App (the new admin & data-entry / read-only surface).
	r.Get("/dashboard", srv.Dashboard)
	r.Get("/pretrial_app.html", srv.CaseManagement)
	r.Get("/analytics.html", srv.Analytics)
	r.Get("/calendar.html", srv.Calendar)
	r.Get("/client_profile.html", srv.ClientProfile)
	r.Get("/api/lookup", srv.APILookup)
	r.Get("/api/clients", srv.APIClient)
	r.Get("/api/stats", srv.APIStats)
	r.Get("/api/defendants", srv.APIDefendants)
	r.Get("/api/refresh", srv.APIRefresh)

	// Admin & data-entry (write/correction surface). Supervisor-gated routes
	// enforce the role inside the handler; CRUD routes are open to any allowed
	// officer. Everything here is audited.
	r.Get("/admin/delete", srv.DeleteConfirm)          // confirmation screen (supervisor)
	r.Post("/admin/delete", srv.Delete)                // perform delete (supervisor)
	r.Post("/admin/restore", srv.Restore)              // un-tombstone (supervisor)
	r.Get("/admin/deleted", srv.Deleted)               // tombstone list + restore (supervisor)
	r.Post("/admin/override", srv.SetOverride)         // set field override (supervisor)
	r.Post("/admin/override/clear", srv.ClearOverride) // clear override (supervisor)

	// Per-defendant extension CRUD (any allowed officer).
	r.Post("/admin/note/add", srv.AddNote)
	r.Post("/admin/note/delete", srv.DeleteNote)
	r.Post("/admin/tag/add", srv.AddTag)
	r.Post("/admin/tag/delete", srv.DeleteTag)
	r.Post("/admin/courtdate/add", srv.AddCourtDate)
	r.Post("/admin/courtdate/delete", srv.DeleteCourtDate)
	r.Post("/admin/reminder/add", srv.AddReminder)
	r.Post("/admin/reminder/delete", srv.DeleteReminder)
	r.Post("/admin/violation/add", srv.AddViolation)
	r.Post("/admin/violation/delete", srv.DeleteViolation)

	log.Printf("PTR server listening on %s (db=%s)", addr, dbPath)
	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatal(err)
	}
}

// baseDir resolves the directory holding templates/ and static/. Defaults to the
// executable's directory; override with APP_BASE_DIR (handy for `go run`).
func baseDir() string {
	if d := os.Getenv("APP_BASE_DIR"); d != "" {
		return d
	}
	if exe, err := os.Executable(); err == nil {
		return filepath.Dir(exe)
	}
	return "."
}

func tmplFuncs() template.FuncMap {
	return template.FuncMap{
		"fmtDate": func(t time.Time) string {
			if t.IsZero() {
				return ""
			}
			return t.Format("Jan 2, 2006")
		},
		"fmtDateP": func(t *time.Time) string {
			if t == nil || t.IsZero() {
				return ""
			}
			return t.Format("Jan 2, 2006")
		},
		"deref": func(p *float64) float64 {
			if p == nil {
				return 0
			}
			return *p
		},
		"derefi": func(p *int) int {
			if p == nil {
				return 0
			}
			return *p
		},
		"isNil": func(p *float64) bool { return p == nil },
	}
}
