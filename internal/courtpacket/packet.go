// Package courtpacket lays out one self-check-in as a tamper-sealed PDF an
// officer can hand to a DA — the "evidence packet" that backs a check-in in a
// violation or revocation hearing. It is the print analogue of the approval
// queue card: the same captured data, organized for the record, with the hash
// chain spelled out so a custodian can attest the record is unaltered.
//
// It builds on internal/pdfgen (no PDF dependency). The honesty principle from
// the rest of the feature carries through to the page: server-observed telemetry
// (timestamp, IP, weekly code) is labelled separately from client-supplied
// telemetry (GPS, device, locale) so the packet never overclaims the soft
// signals. Captured images (selfie, drawn signature) are embedded and annotated
// with whether their bytes still match the digest sealed into the hash chain.
package courtpacket

import (
	"fmt"
	"strings"

	"pretrial-knoxc/internal/models"
	"pretrial-knoxc/internal/pdfgen"
)

// Media is one embedded image (selfie or signature) plus the result of checking
// its bytes against the digest sealed in the check-in row.
type Media struct {
	JPEG     []byte
	Verified bool // bytes hash to the sealed digest
	Note     string
}

// Input is everything the packet renders.
type Input struct {
	Checkin     models.Checkin
	Selfie      *Media
	Signature   *Media
	ChainOK     bool  // VerifyCheckinChain found no break at/through this record
	ChainFirst  int64 // first checkin_id whose hash didn't verify (0 if intact)
	GeneratedBy string
	GeneratedAt string
}

// Layout margins / geometry (points, from the top-left via the cursor).
const (
	marginL  = 54.0
	marginR  = 558.0
	marginT  = 54.0
	marginB  = 56.0 // bottom limit before a page break
	contentW = marginR - marginL
)

// Build renders the packet and returns the PDF bytes.
func Build(in Input) []byte {
	doc := pdfgen.New()
	lay := &layout{doc: doc, in: in}
	lay.newPage()
	c := in.Checkin

	// ── header band ──
	lay.headerBand("Self Check-In — Evidence Record", "Knox County Sheriff's Office · Pre-Trial Release Services")

	status := strings.ToUpper(nz(c.Status, "pending"))
	lay.recordStrip(fmt.Sprintf("Check-in #%d", c.ID), status, "Submitted "+nz(c.ServerTS, "—"))

	// ── subject ──
	lay.section("Subject")
	who := nz(c.ClientName, "(name not given)")
	idn := c.IDN
	if idn == "0" || idn == "" {
		idn = "UNMATCHED — officer to confirm identity"
	}
	lay.field("Name", who)
	lay.field("IDN", idn)
	lay.field("Report type", nz(c.ReportType, "—"))

	// ── presence assessment ──
	lay.section("Presence assessment (computed)")
	lay.field("Badge", presenceLabel(c.PresenceBadge))
	if flags := parseFlags(c.Flags); len(flags) > 0 {
		lay.field("Flags", strings.Join(flags, ", "))
	} else {
		lay.field("Flags", "none")
	}

	// ── server-observed telemetry ──
	lay.section("Server-observed telemetry (recorded by the system; not client-spoofable)")
	lay.field("Server timestamp", nz(c.ServerTS, "—"))
	ipv := nz(c.SrcIP, "—")
	if geo := joinNonEmpty(" · ", c.IPCity, c.IPRegion, c.IPISP); geo != "" {
		ipv += "  (" + geo + ")"
	}
	lay.field("Source IP", ipv)
	lay.field("Lobby code", weekCodeLabel(c.WeekCodeValid))
	lay.field("Distance from office", distLabel(c.DistOfficeM, gpsGranted(c.GPSPerm)))

	// ── client-supplied telemetry ──
	lay.section("Client-supplied telemetry (handed over by the device; corroborating)")
	lay.field("Client timestamp", nz(c.ClientTS, "—"))
	lay.field("GPS permission", nz(c.GPSPerm, "—"))
	if gpsGranted(c.GPSPerm) && (c.GPSLat != 0 || c.GPSLng != 0) {
		lay.field("GPS position", fmt.Sprintf("%.6f, %.6f  (±%.0f ft)", c.GPSLat, c.GPSLng, c.GPSAccuracy*3.28084))
	} else {
		lay.field("GPS position", "not shared")
	}
	lay.field("Distance from home address", distLabel(c.DistHomeM, gpsGranted(c.GPSPerm)))
	lay.field("Device fingerprint", nz(c.DeviceID, "—"))
	lay.field("Time zone / locale", joinNonEmpty(" / ", c.Timezone, c.Locale))
	lay.field("User agent", nz(c.UserAgent, "—"))

	// ── identity factors + images ──
	lay.section("Identity factors")
	if c.OTPVerifiedAt != "" {
		lay.field("SMS one-time code", "verified "+c.OTPVerifiedAt+"  ("+c.OTPPhoneMask+")")
	} else {
		lay.field("SMS one-time code", "not verified")
	}
	lay.images(in.Selfie, in.Signature, c.SignatureKind, c.SignatureData)

	// ── reporting-form answers ──
	lay.section("Reporting form")
	lay.field("Employment", joinNonEmpty(" · ", c.EmploymentStatus, c.Employer, unemp(c.UnemployedLength)))
	lay.field("Citation since last report", yesNo(c.CitationSince, c.CitationDate))
	lay.field("Arrested since last report", yesNo(c.ArrestedSince, c.ArrestedDate))
	lay.field("Next court date", nz(c.NextCourtDate, "—"))
	lay.field("Address given", joinNonEmpty(", ", joinNonEmpty(" ", c.AddressLine1, c.AddressLine2), c.City, c.State, c.Zip))
	lay.field("Phone given", nz(c.Phone, "—"))

	// ── consent ──
	lay.section("Consent (verbatim, as accepted)")
	lay.field("Consent version", nz(c.ConsentVersion, "—")+"   accepted "+nz(c.ConsentAt, "—"))
	lay.paragraph(nz(c.ConsentText, "(no consent text recorded)"), 9, 0.2)

	// ── tamper-evidence ──
	lay.section("Tamper-evidence (SHA-256 hash chain)")
	lay.field("Previous record hash", hashLabel(c.PrevHash))
	lay.field("This record hash", hashLabel(c.RecordHash))
	if in.ChainOK {
		lay.attest("Chain verified: this record's stored hash matches a recomputation over its captured fields, " +
			"and the table-wide chain is intact through it. A records custodian can attest the record is unaltered.")
	} else if in.ChainFirst != 0 {
		lay.attest(fmt.Sprintf("CHAIN BREAK DETECTED at check-in #%d — the record cannot be certified unaltered. Investigate before relying on this packet.", in.ChainFirst))
	} else {
		lay.attest("Chain not verified in this render.")
	}

	// ── review ──
	lay.section("Officer review")
	switch strings.ToLower(c.Status) {
	case "approved":
		lay.field("Approved by", joinNonEmpty("  on  ", fmtOfficer(c.ApprovedBy), c.ApprovedAt))
	case "rejected":
		lay.field("Rejected by", joinNonEmpty("  on  ", fmtOfficer(c.ApprovedBy), c.ApprovedAt))
		lay.field("Reason", nz(c.RejectReason, "—"))
	default:
		lay.field("Status", "PENDING — not yet reviewed by an officer")
	}

	return doc.Bytes()
}

// ── layout engine (top-down cursor over pdfgen pages) ────────────────────────

type layout struct {
	doc   *pdfgen.Doc
	page  *pdfgen.Page
	y     float64 // cursor, measured from the top of the page
	pages int
	in    Input
}

// py converts the top-down cursor to a PDF baseline y.
func (l *layout) py(top float64) float64 { return pdfgen.PageH - top }

func (l *layout) newPage() {
	l.page = l.doc.AddPage()
	l.pages++
	l.y = marginT
	l.footer()
}

// footer stamps the integrity line at the bottom of the current page.
func (l *layout) footer() {
	foot := fmt.Sprintf("Generated by %s on %s · integrity protected by SHA-256 hash chain · Knox County Pre-Trial Services",
		fmtOfficer(l.in.GeneratedBy), l.in.GeneratedAt)
	l.page.Line(marginL, pdfgen.PageH-marginB+18, marginR, pdfgen.PageH-marginB+18, 0.4, 0.85)
	l.page.Text(marginL, marginB-26, 7.5, false, 0.5, foot)
}

// ensure breaks to a new page if h more points won't fit above the footer.
func (l *layout) ensure(h float64) {
	if l.y+h > pdfgen.PageH-marginB {
		l.newPage()
	}
}

func (l *layout) headerBand(title, sub string) {
	l.page.Rect(0, l.py(64), pdfgen.PageW, 64, 0.16)
	l.page.Text(marginL, l.py(34), 18, true, 1, title)
	l.page.Text(marginL, l.py(52), 10, false, 0.85, sub)
	l.y = 64 + 14
}

func (l *layout) recordStrip(left, mid, right string) {
	l.ensure(26)
	l.page.Text(marginL, l.py(l.y+12), 11, true, 0, left)
	l.page.Text(marginL+150, l.py(l.y+12), 11, true, 0.1, mid)
	l.page.Text(marginR-float64(len(right))*5.4, l.py(l.y+12), 9, false, 0.35, right)
	l.y += 22
	l.page.Line(marginL, l.py(l.y), marginR, l.py(l.y), 0.6, 0.75)
	l.y += 10
}

func (l *layout) section(title string) {
	l.ensure(28)
	l.y += 6
	l.page.Text(marginL, l.py(l.y+11), 11.5, true, 0.05, title)
	l.y += 16
	l.page.Line(marginL, l.py(l.y), marginR, l.py(l.y), 0.4, 0.85)
	l.y += 9
}

// field renders a label/value row, wrapping the value if it's long.
func (l *layout) field(label, value string) {
	const labelW = 168.0
	lines := wrap(value, 9.5, contentW-labelW)
	if len(lines) == 0 {
		lines = []string{"—"}
	}
	l.ensure(float64(len(lines))*13 + 2)
	l.page.Text(marginL, l.py(l.y+9), 8.5, false, 0.45, strings.ToUpper(label))
	for i, ln := range lines {
		l.page.Text(marginL+labelW, l.py(l.y+9), 9.5, false, 0, ln)
		if i < len(lines)-1 {
			l.y += 12
			l.ensure(13)
		}
	}
	l.y += 15
}

// paragraph wraps a block of text at the given size.
func (l *layout) paragraph(text string, size, gray float64) {
	for _, ln := range wrap(text, size, contentW) {
		l.ensure(size + 4)
		l.page.Text(marginL, l.py(l.y+size), size, false, gray, ln)
		l.y += size + 3
	}
	l.y += 4
}

// attest renders an emphasized integrity statement in a tinted box.
func (l *layout) attest(text string) {
	lines := wrap(text, 9.5, contentW-20)
	h := float64(len(lines))*13 + 14
	l.ensure(h + 4)
	l.page.Rect(marginL, l.py(l.y+h), contentW, h, 0.94)
	yy := l.y + 13
	for _, ln := range lines {
		l.page.Text(marginL+10, l.py(yy), 9.5, false, 0.05, ln)
		yy += 13
	}
	l.y += h + 6
}

// images embeds the selfie and signature side by side with integrity notes.
func (l *layout) images(selfie, sig *Media, sigKind, sigData string) {
	if selfie == nil && sig == nil {
		l.field("Selfie", "none captured")
		if sigKind == "drawn" {
			l.field("Signature", "drawn (image unavailable)")
		} else {
			l.field("Signature", "typed: "+typedName(sigData))
		}
		return
	}
	const boxH = 116.0
	l.ensure(boxH + 30)
	x := marginL + 168
	// Selfie box.
	if selfie != nil && len(selfie.JPEG) > 0 {
		l.page.Text(marginL, l.py(l.y+9), 8.5, false, 0.45, "SELFIE")
		_, _, err := l.page.Image(selfie.JPEG, x, l.py(l.y+boxH), 96, boxH)
		note := selfie.Note
		if note == "" {
			note = integrityNote(selfie.Verified)
		}
		l.page.Text(x+106, l.py(l.y+18), 8.5, false, 0.3, note)
		if err != nil {
			l.page.Text(x+106, l.py(l.y+30), 8.5, false, 0.3, "(image could not be embedded)")
		}
	}
	// Signature box (offset to the right, or below if both present).
	if sig != nil && len(sig.JPEG) > 0 {
		sy := l.y
		if selfie != nil {
			sy = l.y + boxH + 8
			l.ensure(boxH + 40)
		}
		l.page.Text(marginL, l.py(sy+9), 8.5, false, 0.45, "SIGNATURE")
		l.page.Image(sig.JPEG, x, l.py(sy+74), 200, 70)
		note := sig.Note
		if note == "" {
			note = "drawn · " + integrityNote(sig.Verified)
		}
		l.page.Text(x+210, l.py(sy+18), 8.5, false, 0.3, note)
		l.y = sy + 78
	} else {
		l.y += boxH + 6
		if sigKind != "drawn" {
			l.field("Signature", "typed: "+typedName(sigData))
		}
	}
	l.y += 6
}
