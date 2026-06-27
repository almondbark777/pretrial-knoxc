package handlers

// photos.go — defendant / victim photo upload, serving, and delete for the
// client record (problem report #10). Officer-level (any logged-in officer, like
// the rest of the record's data entry); CSRF via the /admin/* POST guard on the
// upload/delete routes. Images are stored as DB blobs (db.SaveDefendantPhoto).

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"pretrial-knoxc/internal/auth"
	"pretrial-knoxc/internal/db"
)

// maxPhotoBytes caps a single upload at 8 MB (decoded). Phone photos are well
// under this; it bounds DB growth + keeps the base64 column sane.
const maxPhotoBytes = 8 << 20

// AddDefendantPhoto handles the record's photo upload. POST /admin/photo/add
// (multipart: idn, kind, caption?, photo file).
func (s *Server) AddDefendantPhoto(w http.ResponseWriter, r *http.Request) {
	idn := strings.TrimSpace(r.FormValue("idn"))
	back := safeNext(r, "/console/clients/"+url.PathEscape(idn))
	if err := r.ParseMultipartForm(maxPhotoBytes + (1 << 20)); err != nil {
		redirectMsg(w, r, back, "Photo upload failed: "+err.Error())
		return
	}
	f, hdr, err := r.FormFile("photo")
	if err != nil {
		redirectMsg(w, r, back, "Choose a photo to upload.")
		return
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxPhotoBytes+1))
	if err != nil {
		redirectMsg(w, r, back, "Photo upload failed: "+err.Error())
		return
	}
	if len(raw) == 0 {
		redirectMsg(w, r, back, "That file was empty.")
		return
	}
	if len(raw) > maxPhotoBytes {
		redirectMsg(w, r, back, "Photo too large — keep it under 8 MB.")
		return
	}
	// Determine MIME from the bytes (don't trust the client) and require an image.
	mime := http.DetectContentType(raw)
	if !strings.HasPrefix(mime, "image/") {
		redirectMsg(w, r, back, "That file isn't an image.")
		return
	}
	_ = hdr
	if err := db.SaveDefendantPhoto(s.DB, idn, r.FormValue("kind"), mime,
		r.FormValue("caption"), raw, auth.User(r)); err != nil {
		redirectMsg(w, r, back, "Save photo failed: "+err.Error())
		return
	}
	s.clearCache()
	redirectMsg(w, r, back, "Photo added.")
}

// DefendantPhoto serves one stored image. GET /console/clients/{idn}/photo/{id}
func (s *Server) DefendantPhoto(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	raw, mime, owner, err := db.GetDefendantPhoto(s.DB, id)
	if err != nil || raw == nil {
		http.NotFound(w, r)
		return
	}
	// Defense in depth: the photo must belong to the IDN in the path.
	if strings.TrimSpace(owner) != strings.TrimSpace(chi.URLParam(r, "idn")) {
		http.NotFound(w, r)
		return
	}
	if mime == "" {
		mime = "application/octet-stream"
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "private, max-age=300")
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(raw))
}

// DeleteDefendantPhoto removes a photo. POST /admin/photo/delete (id, idn)
func (s *Server) DeleteDefendantPhoto(w http.ResponseWriter, r *http.Request) {
	idn := strings.TrimSpace(r.FormValue("idn"))
	back := safeNext(r, "/console/clients/"+url.PathEscape(idn))
	id, err := strconv.ParseInt(strings.TrimSpace(r.FormValue("id")), 10, 64)
	if err != nil {
		redirectMsg(w, r, back, "Bad photo id.")
		return
	}
	if err := db.DeleteDefendantPhoto(s.DB, id, auth.User(r)); err != nil {
		redirectMsg(w, r, back, "Delete photo failed: "+err.Error())
		return
	}
	s.clearCache()
	redirectMsg(w, r, back, "Photo removed.")
}
