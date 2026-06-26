package courtpacket

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"strings"
	"testing"

	"pretrial-knoxc/internal/models"
)

func solidJPEG(w, h int, c color.Color) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	_ = jpeg.Encode(&b, img, &jpeg.Options{Quality: 80})
	return b.Bytes()
}

// TestWriteSample renders a full packet (with embedded JPEG selfie + signature)
// to GENPDF_OUT when set — a manual visual check, skipped in normal runs.
func TestWriteSample(t *testing.T) {
	out := os.Getenv("GENPDF_OUT")
	if out == "" {
		t.Skip("set GENPDF_OUT to write a sample packet")
	}
	pdf := Build(Input{
		Checkin:     sampleCheckin(),
		Selfie:      &Media{JPEG: solidJPEG(200, 240, color.RGBA{120, 160, 200, 255}), Verified: true},
		Signature:   &Media{JPEG: solidJPEG(420, 150, color.RGBA{235, 235, 235, 255}), Verified: true},
		ChainOK:     true,
		GeneratedBy: "alexander.bentley@knoxsheriff.org",
		GeneratedAt: "2026-06-26 11:00:00 EDT",
	})
	if err := os.WriteFile(out, pdf, 0644); err != nil {
		t.Fatal(err)
	}
}

func sampleCheckin() models.Checkin {
	return models.Checkin{
		ID: 42, IDN: "1234567", Status: "approved", ReportType: "Pre-Trial",
		ClientName: "Jordan Avery", Phone: "(865) 555-0100",
		AddressLine1: "120 Oak St", City: "Knoxville", State: "TN", Zip: "37902",
		EmploymentStatus: "Employed", Employer: "Acme Diner",
		CitationSince: false, ArrestedSince: true, ArrestedDate: "2026-06-20",
		NextCourtDate:  "2026-07-15",
		ConsentVersion: "2026-06-25", ConsentAt: "2026-06-26 09:14:02 EDT",
		ConsentText: strings.Repeat("By checking in I confirm the information is true and consent to collection of telemetry. ", 3),
		ServerTS:    "2026-06-26 09:14:02 EDT", SrcIP: "203.0.113.7", IPCity: "Knoxville", IPRegion: "Tennessee", IPISP: "Acme Cable",
		WeekCodeValid: true, GPSPerm: "granted", GPSLat: 35.9646, GPSLng: -83.9202, GPSAccuracy: 8,
		DistOfficeM: 40, DistHomeM: 0, PresenceBadge: "green", Flags: `["matches_home"]`,
		DeviceID: "d1a2b3c4", Timezone: "America/New_York", Locale: "en-US",
		UserAgent:     "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15",
		SignatureKind: "typed", SignatureData: "Jordan Avery",
		PrevHash: "abc123", RecordHash: "def456789012345678901234567890123456789012345678901234567890abcd",
		ApprovedBy: "nicholas.loveless@knoxsheriff.org", ApprovedAt: "2026-06-26 10:00:00 EDT",
	}
}

func TestBuildProducesPDF(t *testing.T) {
	out := Build(Input{
		Checkin:     sampleCheckin(),
		ChainOK:     true,
		GeneratedBy: "alexander.bentley@knoxsheriff.org",
		GeneratedAt: "2026-06-26 11:00:00 EDT",
	})
	if !bytes.HasPrefix(out, []byte("%PDF")) {
		t.Fatal("not a PDF")
	}
	if !bytes.Contains(out, []byte("%%EOF")) {
		t.Error("missing EOF")
	}
	if len(out) < 1000 {
		t.Errorf("PDF suspiciously small: %d bytes", len(out))
	}
}

func TestWrapNeverExceeds(t *testing.T) {
	lines := wrap(strings.Repeat("word ", 60), 10, 200)
	if len(lines) < 2 {
		t.Fatal("expected multiple wrapped lines")
	}
	limit := 200.0 / (10 * 0.55)
	maxChars := int(limit) + 1
	for _, ln := range lines {
		if len(ln) > maxChars {
			t.Errorf("line too long (%d > %d): %q", len(ln), maxChars, ln)
		}
	}
}
