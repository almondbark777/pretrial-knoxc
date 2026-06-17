package handlers

import (
	"net/http"
	"strings"

	"pretrial-knoxc/internal/db"
)

// users.go is the admin-only user/role management surface (the in-app replacement
// for editing ALLOWED_EMAILS / SUPERVISOR_EMAILS on the box). Both handlers are
// admin-gated and CSRF-guarded by the /admin route group, audited in db, and
// invalidate the role cache so a change takes effect on the next request. The
// break-glass admin (ADMIN_EMAILS / the built-in default) can't be demoted or
// removed — the lockout backstop.

// SaveUser adds a user or changes their role. POST /admin/user/save
func (s *Server) SaveUser(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	_ = r.ParseForm()
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	role := strings.ToLower(strings.TrimSpace(r.FormValue("role")))
	if email == "" || !strings.Contains(email, "@") {
		redirectMsg(w, r, "/console/admin", "Enter a valid email address.")
		return
	}
	if !db.ValidRole(role) {
		redirectMsg(w, r, "/console/admin", "Pick a role: officer, supervisor, or admin.")
		return
	}
	// The break-glass admin is permanently admin — never let the UI set it lower.
	if s.Auth.IsBreakGlassAdmin(email) && role != "admin" {
		redirectMsg(w, r, "/console/admin", "That account is a permanent admin and can't be changed.")
		return
	}
	if err := db.SetUserRole(s.DB, email, role, actor); err != nil {
		redirectMsg(w, r, "/console/admin", "Could not save user: "+err.Error())
		return
	}
	if s.Roles != nil {
		s.Roles.Invalidate()
	}
	redirectMsg(w, r, safeNext(r, "/console/admin"), "Saved "+email+" as "+role+".")
}

// RemoveUser revokes a user's access. POST /admin/user/remove
func (s *Server) RemoveUser(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	_ = r.ParseForm()
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	if email == "" {
		redirectMsg(w, r, "/console/admin", "No user specified.")
		return
	}
	if s.Auth.IsBreakGlassAdmin(email) {
		redirectMsg(w, r, "/console/admin", "That account is a permanent admin and can't be removed.")
		return
	}
	if email == strings.ToLower(strings.TrimSpace(actor)) {
		redirectMsg(w, r, "/console/admin", "You can't remove your own access.")
		return
	}
	if err := db.RemoveUser(s.DB, email, actor); err != nil {
		redirectMsg(w, r, "/console/admin", "Could not remove user: "+err.Error())
		return
	}
	if s.Roles != nil {
		s.Roles.Invalidate()
	}
	redirectMsg(w, r, safeNext(r, "/console/admin"), "Removed "+email+".")
}
