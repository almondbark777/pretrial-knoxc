// checkin_public.go serves the PUBLIC, unauthenticated self-check-in flow that
// a client reaches by scanning the lobby QR (/checkin?c=<weekcode>):
//
//	GET  /checkin         the reporting form + consent + browser telemetry capture
//	POST /checkin/submit  records the submission (pending) with full telemetry
//	GET  /checkin/done    a plain confirmation
//
// Privacy: with SMS-OTP off, the page deliberately does NOT pre-fill from the
// client database — anyone could otherwise type a name+DOB on an unauthenticated
// page and read that person's record. The client enters their own information;
// the server matches it to an IDN at submit time. Once identity is verified
// (OTP on), safe pre-fill can be added.
//
// Telemetry is split server-observed (src IP, server timestamp — recorded here,
// unspoofable by the form) vs client-supplied (GPS, device fingerprint, locale —
// posted by the page). The presence badge is computed server-side from the
// office geofence, the client's home-address distance, and an impossible-travel
// check against their last check-in.
package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"pretrial-knoxc/internal/compute"
	"pretrial-knoxc/internal/db"
	"pretrial-knoxc/internal/models"
)

// maxImageBytes caps a decoded selfie/signature (the request body is already
// capped upstream; this bounds a single field and what we persist per row).
const maxImageBytes = 1 << 20 // 1 MiB

// consentText is the notice the client accepts. Stored verbatim on each
// submission (with its version) so the record proves exactly what was agreed to.
const consentText = "By checking in I confirm the information is true and I consent to Knox County Pre-Trial Services collecting my location, network (IP) address, device information, and the date and time of this check-in to verify my reporting. I understand a false check-in may be a violation of my release conditions."

// CheckinPage renders the public reporting form.
func (s *Server) CheckinPage(w http.ResponseWriter, r *http.Request) {
	// The lobby QR carries the current weekly code in ?c=; stamp it through the
	// form so the submission records which code was used (provenance).
	code := strings.TrimSpace(r.URL.Query().Get("c"))
	s.render(w, "checkin_public.html", map[string]any{
		"WeekCode":       code,
		"ConsentText":    consentText,
		"ConsentVersion": db.GetCheckinConfig(s.DB, "consent_version"),
		"Err":            r.URL.Query().Get("err"),
	})
}

// CheckinDone is the post-submit confirmation.
func (s *Server) CheckinDone(w http.ResponseWriter, r *http.Request) {
	s.render(w, "checkin_done.html", map[string]any{})
}

// CheckinSubmit records one self-check-in (status pending) with server-observed
// + client-supplied telemetry and a computed presence assessment.
func (s *Server) CheckinSubmit(w http.ResponseWriter, r *http.Request) {
	// Abuse guard on the one unauthenticated write endpoint (burst + spacing).
	if s.checkinLimiter != nil {
		if ok, _ := s.checkinLimiter.allow(clientIP(r)); !ok {
			http.Redirect(w, r, "/checkin?err=toomany", http.StatusSeeOther)
			return
		}
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	// Consent is mandatory — no consent, no record.
	if r.FormValue("consent_accept") == "" {
		http.Redirect(w, r, "/checkin?err=consent", http.StatusSeeOther)
		return
	}

	now := compute.NowET()
	c := models.Checkin{
		ReportType:       strings.TrimSpace(r.FormValue("report_type")),
		ClientName:       strings.TrimSpace(r.FormValue("client_name")),
		Phone:            strings.TrimSpace(r.FormValue("phone")),
		AddressLine1:     strings.TrimSpace(r.FormValue("address_line1")),
		AddressLine2:     strings.TrimSpace(r.FormValue("address_line2")),
		City:             strings.TrimSpace(r.FormValue("city")),
		State:            strings.TrimSpace(r.FormValue("state")),
		Zip:              strings.TrimSpace(r.FormValue("zip")),
		EmploymentStatus: strings.TrimSpace(r.FormValue("employment_status")),
		Employer:         strings.TrimSpace(r.FormValue("employer")),
		UnemployedLength: strings.TrimSpace(r.FormValue("unemployed_length")),
		CitationSince:    r.FormValue("citation_since") != "",
		CitationDate:     strings.TrimSpace(r.FormValue("citation_date")),
		ArrestedSince:    r.FormValue("arrested_since") != "",
		ArrestedDate:     strings.TrimSpace(r.FormValue("arrested_date")),
		NextCourtDate:    strings.TrimSpace(r.FormValue("next_court_date")),

		// Signature: typed name + the accepted attestation = an e-signature.
		SignatureKind: "typed",
		SignatureData: strings.TrimSpace(r.FormValue("signature_name")),

		// Consent record.
		ConsentVersion: strings.TrimSpace(r.FormValue("consent_version")),
		ConsentText:    consentText,
		ConsentAt:      now.Format("2006-01-02 15:04:05 MST"),

		// Server-observed (trustworthy).
		ServerTS:  now.Format("2006-01-02 15:04:05 MST"),
		SrcIP:     clientIP(r),
		UserAgent: r.Header.Get("User-Agent"),

		// Client-supplied (corroborating).
		ClientTS:    strings.TrimSpace(r.FormValue("client_ts")),
		GPSPerm:     strings.TrimSpace(r.FormValue("gps_perm")),
		GPSLat:      atof(r.FormValue("gps_lat")),
		GPSLng:      atof(r.FormValue("gps_lng")),
		GPSAccuracy: atof(r.FormValue("gps_accuracy")),
		Timezone:    strings.TrimSpace(r.FormValue("timezone")),
		Locale:      strings.TrimSpace(r.FormValue("locale")),
		DeviceID:    strings.TrimSpace(r.FormValue("device_id")),
	}

	// Which weekly code was used (provenance); flag a stale/unknown one.
	if code := strings.TrimSpace(r.FormValue("week_code")); code != "" {
		if wc, _ := db.WeeklyCodeByCode(s.DB, code); wc != nil {
			c.WeekCodeID = wc.ID
			c.WeekCodeValid = wc.Active
		}
	} else {
		c.WeekCodeValid = false
	}

	// Match the typed name + DOB to a client record (privacy-safe: matching at
	// submit, not pre-fill). Unmatched submissions still record — the officer
	// resolves identity from the queue.
	dob := strings.TrimSpace(r.FormValue("dob"))
	c.IDN = s.matchClientIDN(c.ClientName, dob)

	// Enrich the server-observed source IP (best-effort; inert unless configured).
	// Done before the hash is sealed so the geo is part of the evidence record.
	if s.IPGeo != nil && s.IPGeo.Enabled() {
		ctx, cancel := context.WithTimeout(r.Context(), 2500*time.Millisecond)
		geo := s.IPGeo.Lookup(ctx, c.SrcIP)
		cancel()
		c.IPCity, c.IPRegion, c.IPISP = geo.City, geo.Region, geo.ISP
	}

	// Captured images: a selfie and, when the client draws rather than types,
	// their signature. We seal each image's sha256 INTO the record (so the bytes
	// stored separately in checkin_media can't be swapped undetected) before the
	// hash chain closes; the bytes themselves are saved after we have the row id.
	selfie := decodeDataImage(r.FormValue("selfie_jpeg"))
	sig := decodeDataImage(r.FormValue("signature_jpeg"))
	if len(selfie) > 0 {
		c.SelfiePath = "sha256:" + sha256Hex(selfie)
		c.SelfieLiveness = "self-captured" // honest: a photo, not a verified-liveness check
	}
	if len(sig) > 0 {
		c.SignatureKind = "drawn"
		name := c.SignatureData
		c.SignatureData = strings.TrimSpace(name + " · drawn:sha256:" + sha256Hex(sig))
	}

	// Presence assessment (badge + distances + flags), server-side.
	s.assessPresence(&c, dob)

	id, _, err := db.InsertCheckin(s.DB, c)
	if err != nil {
		http.Error(w, "could not record check-in", http.StatusInternalServerError)
		return
	}
	// Persist the heavy image blobs against the new row (the digests are already
	// sealed above). A media failure shouldn't lose the check-in — log-and-skip.
	if len(selfie) > 0 {
		_, _ = db.SaveCheckinMedia(s.DB, id, "selfie", "image/jpeg", selfie)
	}
	if len(sig) > 0 {
		_, _ = db.SaveCheckinMedia(s.DB, id, "signature", "image/jpeg", sig)
	}
	s.clearCache() // refresh the pending-count nav badge
	http.Redirect(w, r, "/checkin/done", http.StatusSeeOther)
}

// decodeDataImage parses a "data:image/jpeg;base64,…" URL into raw JPEG bytes,
// returning nil for anything blank, non-JPEG, or over the size cap.
func decodeDataImage(dataURL string) []byte {
	dataURL = strings.TrimSpace(dataURL)
	if dataURL == "" {
		return nil
	}
	i := strings.IndexByte(dataURL, ',')
	if i < 0 || !strings.Contains(strings.ToLower(dataURL[:i]), "image/jpeg") {
		return nil
	}
	raw, err := base64.StdEncoding.DecodeString(dataURL[i+1:])
	if err != nil || len(raw) < 4 || len(raw) > maxImageBytes {
		return nil
	}
	if raw[0] != 0xFF || raw[1] != 0xD8 { // JPEG SOI magic
		return nil
	}
	return raw
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// matchClientIDN finds the unique client whose name tokens and birthdate match
// the typed values. Returns "0" (the sentinel the queue shows as unmatched) when
// there's no single match.
func (s *Server) matchClientIDN(name, dob string) string {
	clients, err := s.clients()
	if err != nil {
		return "0"
	}
	wantName := nameKey(name)
	wantDOB, dobOK := compute.ParseDay(dob)
	if wantName == "" || !dobOK {
		return "0"
	}
	hit := ""
	for idn, recs := range clients {
		for _, c := range recs {
			if nameKey(c.Name) != wantName {
				continue
			}
			if cd, ok := compute.ParseDay(c.Birthdate); !ok || !sameDay(cd, wantDOB) {
				continue
			}
			if hit != "" && hit != idn {
				return "0" // ambiguous — let the officer disambiguate
			}
			hit = idn
		}
	}
	if hit == "" {
		return "0"
	}
	return hit
}

// assessPresence sets DistOfficeM, DistHomeM, PresenceBadge and Flags from the
// office geofence, the client's home-address distance, the lobby-code validity,
// and an impossible-travel check against the previous check-in.
func (s *Server) assessPresence(c *models.Checkin, dob string) {
	var flags []string
	gpsOK := strings.EqualFold(c.GPSPerm, "granted") && (c.GPSLat != 0 || c.GPSLng != 0)

	if gpsOK {
		offLat := db.GetCheckinConfigFloat(s.DB, "office_lat")
		offLng := db.GetCheckinConfigFloat(s.DB, "office_lng")
		radius := db.GetCheckinConfigFloat(s.DB, "geofence_radius_m")
		if radius <= 0 {
			radius = 150
		}
		c.DistOfficeM = compute.HaversineMeters(c.GPSLat, c.GPSLng, offLat, offLng)

		// Distance from the client's home address, when we have it geocoded.
		if c.IDN != "0" {
			if ct, _ := db.GetClientContact(s.DB, c.IDN); ct != nil && ct.HasHomeGeo {
				c.DistHomeM = compute.HaversineMeters(c.GPSLat, c.GPSLng, ct.HomeLat, ct.HomeLng)
			}
		}

		switch {
		case c.DistOfficeM <= radius:
			c.PresenceBadge = "green"
		default:
			c.PresenceBadge = "red"
			flags = append(flags, "off_site")
			if c.DistHomeM > 0 && c.DistHomeM <= 150 {
				flags = append(flags, "matches_home")
			}
		}

		// Impossible travel: too far, too fast since the last check-in.
		if f := s.impossibleTravelFlag(c); f != "" {
			flags = append(flags, f)
		}
	} else {
		// No usable GPS fix → can't place them; unverified.
		c.PresenceBadge = "yellow"
		if strings.EqualFold(c.GPSPerm, "denied") {
			flags = append(flags, "gps_denied")
		} else {
			flags = append(flags, "gps_unavailable")
		}
	}

	// Device-binding signals (independent of GPS). The current submission isn't
	// inserted yet, so these reflect prior history only.
	if c.IDN != "0" && c.DeviceID != "" {
		seen, others := db.DeviceUsage(s.DB, c.DeviceID, c.IDN)
		if len(others) > 0 {
			flags = append(flags, "shared_device") // one handset used for several clients
		}
		if !seen {
			if prior, _ := db.ListCheckinsForIDN(s.DB, c.IDN); len(prior) > 0 {
				flags = append(flags, "new_device") // checked in before, but never from this phone
			}
		}
	}

	if !c.WeekCodeValid {
		flags = append(flags, "stale_code")
	}
	if c.IDN == "0" {
		flags = append(flags, "identity_unmatched")
	}

	if len(flags) > 0 {
		if b, err := json.Marshal(flags); err == nil {
			c.Flags = string(b)
		}
	}
}

// impossibleTravelFlag returns "impossible_travel" when the straight-line speed
// from the client's most recent GPS check-in to this one exceeds a generous
// ceiling (≈ highway-implausible), else "".
func (s *Server) impossibleTravelFlag(c *models.Checkin) string {
	if c.IDN == "0" {
		return ""
	}
	prior, err := db.ListCheckinsForIDN(s.DB, c.IDN)
	if err != nil {
		return ""
	}
	now, ok := parseStamp(c.ServerTS)
	if !ok {
		return ""
	}
	for _, p := range prior {
		if !strings.EqualFold(p.GPSPerm, "granted") || (p.GPSLat == 0 && p.GPSLng == 0) {
			continue
		}
		then, ok := parseStamp(p.ServerTS)
		if !ok {
			continue
		}
		hours := now.Sub(then).Hours()
		if hours <= 0 {
			continue
		}
		meters := compute.HaversineMeters(c.GPSLat, c.GPSLng, p.GPSLat, p.GPSLng)
		mph := (meters / 1609.344) / hours
		if mph > 600 { // faster than any ground travel — physically implausible
			return "impossible_travel"
		}
		return "" // most-recent prior GPS check-in is the only one that matters
	}
	return ""
}

// ── small helpers ───────────────────────────────────────────────────────────

// clientIP returns the source IP (host only). middleware.RealIP has already
// resolved X-Forwarded-For into RemoteAddr upstream.
func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// nameKey normalizes a name to a sorted set of lowercase alpha tokens, so
// "Avery, Jordan" and "Jordan Avery" key the same.
func nameKey(name string) string {
	var toks []string
	for _, f := range strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
		return !(r >= 'a' && r <= 'z')
	}) {
		if len(f) > 1 { // drop single-letter middle initials / noise
			toks = append(toks, f)
		}
	}
	// insertion sort (tiny n) to avoid pulling in sort for one call site
	for i := 1; i < len(toks); i++ {
		for j := i; j > 0 && toks[j] < toks[j-1]; j-- {
			toks[j], toks[j-1] = toks[j-1], toks[j]
		}
	}
	return strings.Join(toks, " ")
}

func atof(s string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}

func parseStamp(s string) (time.Time, bool) {
	for _, layout := range []string{"2006-01-02 15:04:05 MST", "2006-01-02 15:04:05"} {
		if len(s) >= len(layout) {
			if t, err := time.Parse(layout, s[:len(layout)]); err == nil {
				return t, true
			}
		}
	}
	return time.Time{}, false
}
