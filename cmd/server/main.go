// Command server is the single-binary Knox County Pre-Trial web app.
//
//	go build ./cmd/server   ->   ./server
//
// Listens on 127.0.0.1:8000 (cloudflared reaches it locally). Native SQLite, no
// Python, no T-SQL shim. Business math is computed server-side (internal/compute).
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	_ "time/tzdata" // embed tz database so America/New_York works on any host

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"pretrial-knoxc/internal/auth"
	"pretrial-knoxc/internal/build"
	"pretrial-knoxc/internal/compute"
	"pretrial-knoxc/internal/db"
	"pretrial-knoxc/internal/handlers"
	"pretrial-knoxc/internal/metrics"
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
	if envBool("COOKIE_SECURE") {
		a.SetCookieSecure(true) // browser↔Cloudflare hop is HTTPS; set Secure in prod
	}
	const cacheTTL = 60 * time.Second
	srv := handlers.New(database, a, tmpl, cacheTTL, importerRetired)

	mtr := metrics.New(time.Now())

	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)
	// Gzip responses (html/css/js/json) when the client accepts it. The roster page
	// ships a few hundred KB of JSON; compression cuts that ~85% on the wire, which
	// matters most on slow office LANs / low-end machines. Applied before the metrics
	// middleware so duration still covers the full handler.
	r.Use(middleware.Compress(5))
	r.Use(mtr.Middleware) // records every request; reads chi's matched route pattern
	r.Use(securityHeaders)
	r.Use(a.Middleware)

	// Static assets (public).
	staticDir := filepath.Join(base, "static")
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir(staticDir))))

	// Auth-free. /metrics is localhost-only by virtue of the 127.0.0.1 listener
	// (not added to the cloudflared ingress), so Netdata on the same box scrapes
	// it without a session; it exposes route names + counts, never PII.
	r.Get("/health", srv.Health)
	r.Get("/metrics", mtr.Handler)

	// Auth pages.
	r.Get("/login", srv.LoginPage)
	r.Post("/api/login", srv.APILogin)
	r.Post("/api/logout", srv.APILogout)

	// Landing page = the client tracker (shell + iframe). The primary app is the
	// Case Console at /console.
	r.Get("/", srv.Home)
	r.Get("/api/lookup_data", srv.APILookupData)

	// Case Console — the application.
	r.Get("/console", srv.Console)
	r.Get("/console/clients", srv.ConsoleClients)
	r.Get("/console/clients/new", srv.ConsoleIntake) // static segment wins over {idn}
	r.Get("/console/clients/{idn}", srv.ConsoleRecordPage)
	r.Get("/console/calendar", srv.ConsoleCalendar)
	r.Get("/console/compliance", srv.ConsoleCompliance)
	r.Get("/console/reports", srv.ConsoleReports)
	r.Get("/console/admin", srv.ConsoleAdmin)
	r.Get("/console/help", srv.ConsoleHelp)
	r.Get("/api/clients/{idn}", srv.APIClientByID)

	// The classic "Direction A" interface was removed (2026-06-09) — old bookmarks
	// land on the console equivalent. The JSON endpoints below predate the console
	// but are still real API surface (the console's global search uses /api/lookup).
	r.Get("/dashboard", redirectTo("/console"))
	r.Get("/my_day.html", redirectTo("/console"))
	r.Get("/pretrial_app.html", redirectTo("/console/clients"))
	r.Get("/analytics.html", redirectTo("/console/reports"))
	r.Get("/calendar.html", legacyCalendarRedirect)
	r.Get("/client_profile.html", legacyProfileRedirect)
	r.Get("/api/lookup", srv.APILookup)
	r.Get("/api/clients", srv.APIClient)
	r.Get("/api/stats", srv.APIStats)
	r.Get("/api/defendants", srv.APIDefendants)
	r.Get("/api/refresh", srv.APIRefresh)

	// CSV exports (read-only; the dependency-free "Export to Excel" equivalent).
	r.Get("/export/behind.csv", srv.ExportBehind)
	r.Get("/export/missed.csv", srv.ExportMissed)
	r.Get("/export/violations.csv", srv.ExportViolations)
	r.Get("/export/cases.csv", srv.ExportCases)
	r.Get("/export/em-fees.csv", srv.ExportEMFees)

	// Printable reports (clean black-on-white via print CSS).
	r.Get("/reports", srv.Reports)
	r.Get("/reports/behind", srv.ReportBehind)
	r.Get("/reports/missed", srv.ReportMissed)

	// Past-Due EM Fees report + memo generation (the show-cause letters).
	r.Get("/reports/em-fees", srv.ReportEMFees)
	r.Get("/reports/em-fees/memo", srv.EMFeeMemo) // one filled .docx (logged in letter_log)
	// Batch generation is a CSRF-guarded POST of the report's selection
	// (checkboxes decide who gets a letter; every memo is logged). The old GET
	// bookmark lands back on the report to pick a selection.
	r.With(csrfGuard(a)).Post("/reports/em-fees/memos.zip", srv.EMFeeMemosZip)
	r.Get("/reports/em-fees/memos.zip", redirectTo("/reports/em-fees"))

	// Admin & data-entry (write/correction surface). Every POST carries a CSRF
	// token (csrfGuard). Supervisor-gated routes enforce the role inside the
	// handler; CRUD routes are open to any allowed officer. Everything is audited.
	r.Route("/admin", func(ar chi.Router) {
		ar.Use(csrfGuard(a))
		ar.Get("/delete", srv.DeleteConfirm)             // confirmation screen (supervisor)
		ar.Post("/delete", srv.Delete)                   // perform delete (supervisor)
		ar.Post("/restore", srv.Restore)                 // un-tombstone (supervisor)
		ar.Post("/undo_last_delete", srv.UndoLastDelete) // one-click newest restore (supervisor)
		ar.Get("/deleted", srv.Deleted)                  // tombstone list + restore (supervisor)
		ar.Get("/audit", srv.Audit)                      // audit-log viewer (supervisor)
		ar.Post("/override", srv.SetOverride)            // set field override (supervisor)
		ar.Post("/override/clear", srv.ClearOverride)    // clear override (supervisor)
		ar.Post("/waiver", srv.SetFeeWaiver)             // grant GPS fee waiver (supervisor)
		ar.Post("/waiver/clear", srv.ClearFeeWaiver)     // remove app fee waiver (supervisor)

		// Data entry (any allowed officer): add a client, payments, check-ins.
		// The classic add-client form is gone — the console intake wizard is the
		// UI; it (and any old bookmark) reaches the same POST endpoint.
		ar.Get("/add_defendant", redirectTo("/console/clients/new"))
		ar.Post("/add_defendant", srv.AddDefendant)
		ar.Post("/payment/add", srv.AddPayment)
		ar.Post("/payment/delete", srv.DeleteAddedPayment)
		ar.Post("/checkin/add", srv.AddCheckIn)
		ar.Post("/checkin/bulk", srv.BulkAddCheckIn)
		ar.Post("/checkin/delete", srv.DeleteAddedCheckIn)
		ar.Post("/schedule/add", srv.AddScheduledCheckIn)
		ar.Post("/schedule/delete", srv.DeleteScheduledCheckIn)

		// Per-defendant extension CRUD (any allowed officer).
		ar.Post("/note/add", srv.AddNote)
		ar.Post("/note/delete", srv.DeleteNote)
		ar.Post("/tag/add", srv.AddTag)
		ar.Post("/tag/delete", srv.DeleteTag)
		ar.Post("/courtdate/add", srv.AddCourtDate)
		ar.Post("/courtdate/delete", srv.DeleteCourtDate)
		ar.Post("/courtdate/outcome", srv.SetCourtOutcome)
		ar.Post("/reminder/add", srv.AddReminder)
		ar.Post("/reminder/delete", srv.DeleteReminder)
		ar.Post("/violation/add", srv.AddViolation)
		ar.Post("/violation/delete", srv.DeleteViolation)
		ar.Post("/drugscreen/add", srv.AddDrugScreen)
		ar.Post("/drugscreen/delete", srv.DeleteDrugScreen)
		ar.Post("/pin/toggle", srv.TogglePin)
		ar.Post("/view/save", srv.SaveView)
		ar.Post("/view/delete", srv.DeleteView)
	})

	httpSrv := &http.Server{Addr: addr, Handler: r}

	// Serve in the background so main can wait for a shutdown signal.
	go func() {
		log.Printf("PTR server listening on %s (db=%s, version=%s)", addr, dbPath, build.Version)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	// Graceful shutdown on SIGINT/SIGTERM (systemd sends SIGTERM on stop/restart):
	// stop accepting connections, let in-flight requests finish, then the deferred
	// database.Close() runs — a clean SQLite close instead of an abrupt kill.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	log.Println("shutdown signal received; draining in-flight requests…")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(ctx); err != nil {
		log.Printf("graceful shutdown timed out: %v", err)
	}
	log.Println("server stopped")
}

// redirectTo returns a handler that 302s to a fixed console path — the landing
// spot for bookmarks of the removed classic interface.
func redirectTo(to string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, to, http.StatusFound)
	}
}

// legacyCalendarRedirect forwards /calendar.html to /console/calendar, carrying
// the old page's ?idn= / ?month= query so per-client and month links keep working.
func legacyCalendarRedirect(w http.ResponseWriter, r *http.Request) {
	to := "/console/calendar"
	if q := r.URL.RawQuery; q != "" {
		to += "?" + q
	}
	http.Redirect(w, r, to, http.StatusFound)
}

// legacyProfileRedirect forwards /client_profile.html?idn=X to /console/clients/X
// (the client list when no idn is given).
func legacyProfileRedirect(w http.ResponseWriter, r *http.Request) {
	to := "/console/clients"
	if idn := strings.TrimSpace(r.URL.Query().Get("idn")); idn != "" {
		to += "/" + url.PathEscape(idn)
	}
	http.Redirect(w, r, to, http.StatusFound)
}

// securityHeaders sets conservative, low-risk response headers on every response.
// X-Frame-Options SAMEORIGIN blocks clickjacking while still allowing the landing
// page's own same-origin iframe (the embedded client tracker). No strict CSP — the
// pages use small inline <script>s for search/filter/toasts, which a default-src
// policy would break; that's a deliberate, separate decision.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

// csrfGuard rejects state-changing POSTs to /admin/* whose form CSRF token does
// not match the session token (synchronizer-token pattern). GET/HEAD pass through.
func csrfGuard(a *auth.Authenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && !a.ValidCSRF(r) {
				http.Error(w, "Invalid or missing CSRF token — reload the page and try again.", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
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

// moneyFmt renders a dollar amount as "$1,234.50" (negative → "-$1,234.50").
func moneyFmt(f float64) string {
	neg := f < 0
	if neg {
		f = -f
	}
	s := fmt.Sprintf("%.2f", f)
	dot := strings.IndexByte(s, '.')
	intp, frac := s[:dot], s[dot:]
	var b strings.Builder
	n := len(intp)
	for i := 0; i < n; i++ {
		if i > 0 && (n-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteByte(intp[i])
	}
	out := "$" + b.String() + frac
	if neg {
		out = "-" + out
	}
	return out
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
		// money / moneyP / intP / boolP render the compute layer's numbers (incl.
		// nil pointers == "missing") as clean currency / counts for the console.
		"money":  func(f float64) string { return moneyFmt(f) },
		"moneyi": func(i int) string { return moneyFmt(float64(i)) },
		"moneyP": func(p *float64) string {
			if p == nil {
				return "—"
			}
			return moneyFmt(*p)
		},
		"intP": func(p *int) string {
			if p == nil {
				return "—"
			}
			return strconv.Itoa(*p)
		},
		// days1 renders a *float64 day count to one decimal (e.g. GPS Days
		// Covered), matching the offline tracker's daysCovered.toFixed(1)
		// (JS rounds halves away from zero — see compute.JSToFixed).
		"days1": func(p *float64) string {
			if p == nil {
				return "—"
			}
			return compute.JSToFixed(*p, 1)
		},
		"boolP": func(p *bool) bool { return p != nil && *p },
		// evclass maps a calendar event Kind to its console .ev color class.
		// officer renders an email as a display name; shortdate normalizes a
		// mixed-format timestamp string to "Jan 2, 2006".
		"officer": compute.FmtOfficer,
		"shortdate": func(s string) string {
			if t, ok := compute.ParseDay(s); ok {
				return t.Format("Jan 2, 2006")
			}
			return s
		},
		"evclass": func(kind string) string {
			switch {
			case strings.HasPrefix(kind, "checkin"):
				return "checkin"
			case kind == "payment" || kind == "ptr-fee":
				return "payment"
			case kind == "gps-install" || kind == "gps-switch":
				return "gps"
			case kind == "missed":
				return "missed"
			case kind == "due":
				return "due"
			case kind == "closed":
				return "closed"
			case kind == "referral":
				return "referral"
			}
			return ""
		},
		// initials renders an avatar monogram from a display name ("Alex Bentley" → "AB").
		// Single source of truth lives in handlers.Initials.
		"initials": handlers.Initials,
	}
}
