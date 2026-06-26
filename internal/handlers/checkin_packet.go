// checkin_packet.go serves the officer-facing artifacts built from a self
// check-in: the tamper-sealed court-packet PDF an officer hands to a DA, and the
// raw selfie image the approval queue thumbnails. Both are read-only GETs behind
// the console's auth; neither changes state, so neither needs a CSRF token.
package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"pretrial-knoxc/internal/auth"
	"pretrial-knoxc/internal/compute"
	"pretrial-knoxc/internal/courtpacket"
	"pretrial-knoxc/internal/db"
)

// CheckinPacket streams the evidence-packet PDF for one check-in. It loads the
// captured images, recomputes whether each still matches the digest sealed in
// the record, and verifies the hash chain — so the packet states integrity as
// fact, not assertion.
func (s *Server) CheckinPacket(w http.ResponseWriter, r *http.Request) {
	id := pathID(r, "id")
	c, err := db.GetCheckin(s.DB, id)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if c == nil {
		http.Error(w, "no such check-in", http.StatusNotFound)
		return
	}

	in := courtpacket.Input{
		Checkin:     *c,
		GeneratedBy: auth.User(r),
		GeneratedAt: compute.NowET().Format("2006-01-02 15:04:05 MST"),
	}

	// Selfie + signature media (and their integrity vs the sealed digest).
	if raw, _, _, _ := db.GetCheckinMedia(s.DB, id, "selfie"); len(raw) > 0 {
		in.Selfie = &courtpacket.Media{JPEG: raw, Verified: db.VerifyMedia(raw, c.SelfiePath)}
	}
	if raw, _, _, _ := db.GetCheckinMedia(s.DB, id, "signature"); len(raw) > 0 {
		in.Signature = &courtpacket.Media{JPEG: raw, Verified: db.VerifyMedia(raw, sealedSigDigest(c.SignatureData))}
	}

	// Chain integrity: a break at or before this record means it can't be
	// certified; a break only downstream leaves this record verifiable.
	if firstBad, err := db.VerifyCheckinChain(s.DB); err == nil {
		if firstBad == 0 || firstBad > c.ID {
			in.ChainOK = true
		} else {
			in.ChainFirst = firstBad
		}
	}

	pdf := courtpacket.Build(in)
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="checkin-%d-%s.pdf"`, id, packetSlug(c.ClientName, c.IDN)))
	_, _ = w.Write(pdf)
}

// CheckinSelfie streams the stored selfie JPEG for a check-in (officer-only, via
// the console auth). Used as the <img> source in the approval queue.
func (s *Server) CheckinSelfie(w http.ResponseWriter, r *http.Request) {
	id := pathID(r, "id")
	raw, mime, _, err := db.GetCheckinMedia(s.DB, id, "selfie")
	if err != nil || len(raw) == 0 {
		http.Error(w, "no selfie on file", http.StatusNotFound)
		return
	}
	if mime == "" {
		mime = "image/jpeg"
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "private, max-age=300")
	_, _ = w.Write(raw)
}

// sealedSigDigest pulls the sha256 hex a drawn signature embeds in signature_data
// ("Name · drawn:sha256:<hex>"), or "" for a typed signature.
func sealedSigDigest(sigData string) string {
	const marker = "drawn:sha256:"
	if i := strings.Index(sigData, marker); i >= 0 {
		return strings.TrimSpace(sigData[i+len(marker):])
	}
	return ""
}

// packetSlug builds a filename-safe "name_idn" stub.
func packetSlug(name, idn string) string {
	var b strings.Builder
	for _, ch := range strings.ToLower(name) {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= '0' && ch <= '9':
			b.WriteRune(ch)
		case ch == ' ':
			b.WriteByte('_')
		}
	}
	slug := strings.Trim(b.String(), "_")
	if slug == "" {
		slug = "client"
	}
	if len(slug) > 40 {
		slug = slug[:40]
	}
	return slug + "_" + idn
}

// pathID parses a chi URL path param as an int64 (0 on anything non-numeric).
func pathID(r *http.Request, key string) int64 {
	id, _ := strconv.ParseInt(strings.TrimSpace(chi.URLParam(r, key)), 10, 64)
	return id
}
