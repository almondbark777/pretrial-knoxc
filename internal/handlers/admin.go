package handlers

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"pretrial-knoxc/internal/auth"
	"pretrial-knoxc/internal/db"
)

// ── role helpers ──────────────────────────────────────────────────────────

// requireSupervisor returns the caller's email and true when they are a
// supervisor. Otherwise it writes a 403 (HTML or JSON) and returns false, so
// callers can `if user, ok := s.requireSupervisor(w, r); !ok { return }`.
func (s *Server) requireSupervisor(w http.ResponseWriter, r *http.Request) (string, bool) {
	user := auth.User(r)
	if s.Auth.IsSupervisor(user) {
		return user, true
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "supervisor role required"})
	} else {
		w.WriteHeader(http.StatusForbidden)
		s.render(w, "message.html", map[string]any{
			"User": user, "Title": "Not permitted",
			"Message": "This action is restricted to supervisors. Ask a supervisor to perform deletions, restores, or field overrides.",
			"Back":    backOr(r, "/dashboard"),
		})
	}
	return user, false
}

// redirectMsg performs a Post/Redirect/Get back to `to` with a flash message.
func redirectMsg(w http.ResponseWriter, r *http.Request, to, msg string) {
	if msg != "" {
		sep := "?"
		if strings.Contains(to, "?") {
			sep = "&"
		}
		to += sep + "msg=" + url.QueryEscape(msg)
	}
	http.Redirect(w, r, to, http.StatusSeeOther)
}

func backOr(r *http.Request, def string) string {
	if b := strings.TrimSpace(r.FormValue("next")); b != "" {
		return b
	}
	return def
}

// ── Delete / restore (supervisor-gated) ──────────────────────────────────────

// DeleteConfirm renders the one-screen confirmation that names exactly who/what
// will be deleted. GET /admin/delete?idn=&case=
func (s *Server) DeleteConfirm(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireSupervisor(w, r)
	if !ok {
		return
	}
	idn := strings.TrimSpace(r.URL.Query().Get("idn"))
	caseTok := strings.TrimSpace(r.URL.Query().Get("case"))
	clients, err := s.clients()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	cases := clients[idn]
	if len(cases) == 0 {
		w.WriteHeader(http.StatusNotFound)
		s.render(w, "message.html", map[string]any{
			"User": user, "Title": "Not found",
			"Message": "No active client with IDN " + idn + " (already deleted?).", "Back": "/dashboard",
		})
		return
	}
	rep := openRep(cases)
	s.render(w, "delete_confirm.html", map[string]any{
		"User":            user,
		"CSRF":            s.Auth.CSRF(w, r),
		"IDN":             idn,
		"Name":            rep.Name,
		"Status":          rep.Status,
		"Officer":         rep.Officer,
		"CaseTok":         caseTok,
		"Cases":           caseOptions(cases),
		"CaseCount":       len(caseOptions(cases)),
		"ImporterRetired": s.ImporterRetired,
	})
}

// Delete performs the delete. POST /admin/delete (idn, case?, reason)
func (s *Server) Delete(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireSupervisor(w, r)
	if !ok {
		return
	}
	_ = r.ParseForm()
	idn := strings.TrimSpace(r.FormValue("idn"))
	caseTok := strings.TrimSpace(r.FormValue("case"))
	reason := strings.TrimSpace(r.FormValue("reason"))
	if idn == "" {
		redirectMsg(w, r, "/dashboard", "Delete failed: no IDN supplied.")
		return
	}
	var err error
	var msg string
	if caseTok != "" {
		err = db.DeleteCase(s.DB, idn, caseTok, user, reason, s.ImporterRetired)
		msg = "Deleted case " + caseTok + " of IDN " + idn + "."
	} else {
		err = db.DeletePerson(s.DB, idn, user, reason, s.ImporterRetired)
		msg = "Deleted IDN " + idn + " — they will no longer appear in any view."
	}
	if err != nil {
		redirectMsg(w, r, "/", "Delete failed: "+err.Error())
		return
	}
	s.clearCache() // change is visible everywhere immediately
	redirectMsg(w, r, "/admin/deleted", msg)
}

// Restore lifts a tombstone. POST /admin/restore (idn, case?)
func (s *Server) Restore(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireSupervisor(w, r)
	if !ok {
		return
	}
	_ = r.ParseForm()
	idn := strings.TrimSpace(r.FormValue("idn"))
	caseTok := strings.TrimSpace(r.FormValue("case"))
	if idn == "" {
		redirectMsg(w, r, "/admin/deleted", "Restore failed: no IDN supplied.")
		return
	}
	var err error
	if caseTok != "" {
		err = db.RestoreCase(s.DB, idn, caseTok, user)
	} else {
		err = db.RestorePerson(s.DB, idn, user)
	}
	if err != nil {
		redirectMsg(w, r, "/admin/deleted", "Restore failed: "+err.Error())
		return
	}
	s.clearCache()
	redirectMsg(w, r, "/admin/deleted", "Restored IDN "+idn+".")
}

// Deleted lists current tombstones with restore buttons. GET /admin/deleted
func (s *Server) Deleted(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireSupervisor(w, r)
	if !ok {
		return
	}
	tombs, err := db.ListTombstones(s.DB)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.render(w, "deleted.html", map[string]any{
		"User": user, "IsSupervisor": true, "ActiveNav": "deleted",
		"Tombstones": tombs, "Msg": r.URL.Query().Get("msg"),
		"ImporterRetired": s.ImporterRetired, "CSRF": s.Auth.CSRF(w, r),
	})
}

// Audit renders the audit-log viewer (supervisor only). GET /admin/audit?idn=
// filters to one defendant's history; otherwise shows the most recent activity.
func (s *Server) Audit(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireSupervisor(w, r)
	if !ok {
		return
	}
	idn := strings.TrimSpace(r.URL.Query().Get("idn"))
	rows, err := db.ListAudit(s.DB, idn, 200)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.render(w, "audit.html", map[string]any{
		"User": user, "IsSupervisor": true, "ActiveNav": "audit",
		"Rows": rows, "IDN": idn, "Limit": 200,
	})
}

// ── Field overrides (supervisor-gated) ───────────────────────────────────────

// SetOverride upserts a field override. POST /admin/override (idn, field, value)
func (s *Server) SetOverride(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireSupervisor(w, r)
	if !ok {
		return
	}
	_ = r.ParseForm()
	idn := strings.TrimSpace(r.FormValue("idn"))
	field := strings.TrimSpace(r.FormValue("field"))
	value := strings.TrimSpace(r.FormValue("value"))
	back := "/client_profile.html?idn=" + url.QueryEscape(idn)
	if !db.IsOverridable(field) {
		redirectMsg(w, r, back, "Override failed: field not overridable.")
		return
	}
	if err := db.SetOverride(s.DB, idn, field, value, user); err != nil {
		redirectMsg(w, r, back, "Override failed: "+err.Error())
		return
	}
	s.clearCache()
	redirectMsg(w, r, back, "Override applied to "+field+".")
}

// ClearOverride removes a field override. POST /admin/override/clear (idn, field)
func (s *Server) ClearOverride(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireSupervisor(w, r)
	if !ok {
		return
	}
	_ = r.ParseForm()
	idn := strings.TrimSpace(r.FormValue("idn"))
	field := strings.TrimSpace(r.FormValue("field"))
	back := "/client_profile.html?idn=" + url.QueryEscape(idn)
	if err := db.ClearOverride(s.DB, idn, field, user); err != nil {
		redirectMsg(w, r, back, "Clear override failed: "+err.Error())
		return
	}
	s.clearCache()
	redirectMsg(w, r, back, "Override on "+field+" cleared.")
}

// ── Per-defendant CRUD (any allowed officer) ─────────────────────────────────

// profileBack returns the profile redirect target for the posted idn.
// safeNext returns the request's `next` form value if it's a same-origin path
// (starts with "/" but not "//"), else def. Guards against open redirects so the
// new console can post to shared /admin/* endpoints and come back to /console/*.
func safeNext(r *http.Request, def string) string {
	if n := strings.TrimSpace(r.FormValue("next")); strings.HasPrefix(n, "/") && !strings.HasPrefix(n, "//") {
		return n
	}
	return def
}

func profileBack(r *http.Request) (idn, to string) {
	idn = strings.TrimSpace(r.FormValue("idn"))
	return idn, safeNext(r, "/client_profile.html?idn="+url.QueryEscape(idn))
}

func (s *Server) AddNote(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	idn, back := profileBack(r)
	err := db.AddNote(s.DB, idn, r.FormValue("body"), auth.User(r))
	s.afterWrite(w, r, back, err, "Note added.")
}

func (s *Server) DeleteNote(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	_, back := profileBack(r)
	s.afterWrite(w, r, back, db.DeleteNote(s.DB, formID(r), auth.User(r)), "Note deleted.")
}

func (s *Server) AddTag(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	idn, back := profileBack(r)
	err := db.AddTag(s.DB, idn, r.FormValue("label"), auth.User(r))
	s.afterWrite(w, r, back, err, "Tag added.")
}

func (s *Server) DeleteTag(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	_, back := profileBack(r)
	s.afterWrite(w, r, back, db.DeleteTag(s.DB, formID(r), auth.User(r)), "Tag removed.")
}

func (s *Server) AddCourtDate(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	idn, back := profileBack(r)
	err := db.AddCourtDate(s.DB, idn, r.FormValue("court_date"), r.FormValue("court"), r.FormValue("notes"), auth.User(r))
	s.afterWrite(w, r, back, err, "Court date added.")
}

func (s *Server) DeleteCourtDate(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	_, back := profileBack(r)
	s.afterWrite(w, r, back, db.DeleteCourtDate(s.DB, formID(r), auth.User(r)), "Court date deleted.")
}

// SetCourtOutcome logs a hearing's result (+ optional next date) on a court_dates
// row — the after-the-hearing FTA step (§5.6). POST /admin/courtdate/outcome.
func (s *Server) SetCourtOutcome(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	_, back := profileBack(r)
	err := db.SetCourtOutcome(s.DB, formID(r), r.FormValue("outcome"), r.FormValue("next_date"), auth.User(r))
	s.afterWrite(w, r, back, err, "Court outcome logged.")
}

func (s *Server) AddReminder(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	idn, back := profileBack(r)
	err := db.AddReminder(s.DB, idn, r.FormValue("body"), r.FormValue("due_date"), r.FormValue("assigned_to"), auth.User(r))
	s.afterWrite(w, r, back, err, "Reminder added.")
}

func (s *Server) DeleteReminder(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	_, back := profileBack(r)
	s.afterWrite(w, r, back, db.DeleteReminder(s.DB, formID(r), auth.User(r)), "Reminder deleted.")
}

func (s *Server) AddViolation(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	idn, back := profileBack(r)
	err := db.AddViolation(s.DB, idn, r.FormValue("violation_date"), r.FormValue("category"),
		r.FormValue("severity"), r.FormValue("description"), r.FormValue("action_taken"), auth.User(r))
	s.afterWrite(w, r, back, err, "Violation recorded.")
}

func (s *Server) DeleteViolation(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	_, back := profileBack(r)
	s.afterWrite(w, r, back, db.DeleteViolation(s.DB, formID(r), auth.User(r)), "Violation deleted.")
}

// afterWrite is the common tail for extension writes: on error flash it, else
// clear the cache (extension data shows on the profile) and flash success.
func (s *Server) afterWrite(w http.ResponseWriter, r *http.Request, back string, err error, okMsg string) {
	if err != nil {
		redirectMsg(w, r, back, "Failed: "+err.Error())
		return
	}
	s.clearCache()
	redirectMsg(w, r, back, okMsg)
}

func formID(r *http.Request) int64 {
	id, _ := strconv.ParseInt(strings.TrimSpace(r.FormValue("id")), 10, 64)
	return id
}
