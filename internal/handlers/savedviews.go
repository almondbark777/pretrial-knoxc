// savedviews.go — save/delete named roster views (per-officer filter combos,
// CSRF-guarded under /admin, audited in the db layer). The roster's filters are
// already shareable URL params, so a saved view is just a sanitized query
// string with a name; the chips on /console/clients link straight to it.
package handlers

import (
	"net/http"
	"net/url"
	"strings"

	"pretrial-knoxc/internal/auth"
	"pretrial-knoxc/internal/db"
)

// viewParams are the roster filter keys a saved view may carry — anything else
// posted is dropped, so stored specs stay clean and safe to link.
var viewParams = []string{"q", "status", "level", "officer", "comp", "gps", "due"}

// sanitizeViewQuery keeps only known filter params and re-encodes them
// deterministically (url.Values.Encode sorts keys).
func sanitizeViewQuery(raw string) string {
	vals, _ := url.ParseQuery(raw)
	out := url.Values{}
	for _, k := range viewParams {
		if v := strings.TrimSpace(vals.Get(k)); v != "" {
			out.Set(k, v)
		}
	}
	return out.Encode()
}

// SaveView stores the posted filter combo under a name and lands on the saved
// view itself. POST /admin/view/save (name, query)
func (s *Server) SaveView(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		redirectMsg(w, r, "/console/clients", "View name is required.")
		return
	}
	if len(name) > 60 {
		name = name[:60]
	}
	query := sanitizeViewQuery(r.FormValue("query"))
	if err := db.SaveView(s.DB, auth.User(r), name, query, "/console/clients"); err != nil {
		redirectMsg(w, r, "/console/clients", "Failed: "+err.Error())
		return
	}
	to := "/console/clients"
	if query != "" {
		to += "?" + query
	}
	redirectMsg(w, r, to, "View “"+name+"” saved.")
}

// DeleteView removes one of the caller's saved views. POST /admin/view/delete (id)
func (s *Server) DeleteView(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	back := safeNext(r, "/console/clients")
	if err := db.DeleteSavedView(s.DB, formID(r), auth.User(r)); err != nil {
		redirectMsg(w, r, back, "Failed: "+err.Error())
		return
	}
	redirectMsg(w, r, back, "Saved view deleted.")
}
