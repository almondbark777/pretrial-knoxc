// Package pdfgen is a tiny, dependency-free PDF writer — just enough to lay out
// the QR-check-in court packet: positioned text in Helvetica / Helvetica-Bold,
// horizontal rules, filled rectangles (header band, chips), and embedded JPEG
// images (the client's selfie + drawn signature, dropped in verbatim as
// DCTDecode XObjects).
//
// It exists for the same reason internal/otp uses stdlib net/http instead of an
// SDK: the project keeps a single-binary, minimal-dependency stance, and a court
// packet is a fixed, text-heavy form that doesn't need a full PDF library. The
// 14 standard PDF fonts (Helvetica family) are built into every reader, so no
// font is embedded.
//
// Coordinate system is PDF-native: origin bottom-left, points (1/72"). US Letter
// pages are 612 x 792. Callers that prefer a top-down cursor convert with
// PageH - y; internal/courtpacket does exactly that.
package pdfgen

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
)

// Page dimensions (US Letter, points).
const (
	PageW = 612.0
	PageH = 792.0
)

type embeddedImage struct {
	name       string
	w, h       int // pixel dimensions, read from the JPEG SOF marker
	colorSpace string
	data       []byte
}

// Page accumulates a content stream plus the image XObjects it references.
type Page struct {
	buf  bytes.Buffer
	imgs []embeddedImage
}

// Doc is a sequence of pages.
type Doc struct {
	pages []*Page
}

// New returns an empty document.
func New() *Doc { return &Doc{} }

// AddPage appends and returns a new US-Letter page.
func (d *Doc) AddPage() *Page {
	p := &Page{}
	d.pages = append(d.pages, p)
	return p
}

// Text draws a left-aligned string at (x, y) — y is the text baseline. bold
// selects Helvetica-Bold. gray is the fill level (0 = black, 1 = white).
func (p *Page) Text(x, y, size float64, bold bool, gray float64, s string) {
	font := "F1"
	if bold {
		font = "F2"
	}
	fmt.Fprintf(&p.buf, "BT /%s %.2f Tf %.3f g %.2f %.2f Td (%s) Tj ET\n",
		font, size, gray, x, y, escapeText(s))
}

// Line strokes from (x1,y1) to (x2,y2).
func (p *Page) Line(x1, y1, x2, y2, width, gray float64) {
	fmt.Fprintf(&p.buf, "%.3f G %.2f w %.2f %.2f m %.2f %.2f l S\n",
		gray, width, x1, y1, x2, y2)
}

// Rect fills a rectangle with the given gray level.
func (p *Page) Rect(x, y, w, h, gray float64) {
	fmt.Fprintf(&p.buf, "%.3f g %.2f %.2f %.2f %.2f re f\n", gray, x, y, w, h)
}

// Image places a JPEG, scaled to fit a w×h box anchored at (x,y) (bottom-left),
// preserving the image's aspect ratio and centering it in the box. Returns the
// drawn width/height. The JPEG bytes are embedded unmodified (DCTDecode).
func (p *Page) Image(jpeg []byte, x, y, w, h float64) (float64, float64, error) {
	iw, ih, comps, err := jpegInfo(jpeg)
	if err != nil {
		return 0, 0, err
	}
	cs := "DeviceRGB"
	switch comps {
	case 1:
		cs = "DeviceGray"
	case 4:
		cs = "DeviceCMYK"
	}
	name := fmt.Sprintf("Im%d", len(p.imgs)+1)
	p.imgs = append(p.imgs, embeddedImage{name: name, w: iw, h: ih, colorSpace: cs, data: jpeg})

	// Fit within the box, keep aspect ratio, center.
	scale := w / float64(iw)
	if s := h / float64(ih); s < scale {
		scale = s
	}
	dw, dh := float64(iw)*scale, float64(ih)*scale
	dx, dy := x+(w-dw)/2, y+(h-dh)/2
	fmt.Fprintf(&p.buf, "q %.2f 0 0 %.2f %.2f %.2f cm /%s Do Q\n", dw, dh, dx, dy, name)
	return dw, dh, nil
}

// Bytes serializes the whole document to a PDF byte slice.
func (d *Doc) Bytes() []byte {
	if len(d.pages) == 0 {
		d.AddPage()
	}
	// Object numbering: 1 catalog, 2 pages, 3 Helvetica, 4 Helvetica-Bold, then
	// per page: content stream, page, and one object per embedded image.
	const fixed = 4
	var objects []string // index i holds object number i+1

	// Pre-compute each page's object numbers so the /Pages Kids list can refer to
	// them before they're built.
	num := fixed
	type pageObjs struct {
		content int
		page    int
		images  []int
	}
	pos := make([]pageObjs, len(d.pages))
	for i, pg := range d.pages {
		num++
		pos[i].content = num
		num++
		pos[i].page = num
		for range pg.imgs {
			num++
			pos[i].images = append(pos[i].images, num)
		}
	}

	// 1: catalog
	objects = append(objects, "<< /Type /Catalog /Pages 2 0 R >>")
	// 2: pages
	var kids strings.Builder
	for i := range d.pages {
		fmt.Fprintf(&kids, "%d 0 R ", pos[i].page)
	}
	objects = append(objects, fmt.Sprintf("<< /Type /Pages /Count %d /Kids [%s] >>", len(d.pages), strings.TrimSpace(kids.String())))
	// 3,4: fonts
	objects = append(objects, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>")
	objects = append(objects, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold /Encoding /WinAnsiEncoding >>")

	// Per-page objects, in the same order the numbers were assigned.
	for i, pg := range d.pages {
		content := pg.buf.Bytes()
		objects = append(objects, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content))

		var xobj strings.Builder
		for j := range pg.imgs {
			fmt.Fprintf(&xobj, "/%s %d 0 R ", pg.imgs[j].name, pos[i].images[j])
		}
		resources := "<< /Font << /F1 3 0 R /F2 4 0 R >>"
		if xobj.Len() > 0 {
			resources += " /XObject << " + strings.TrimSpace(xobj.String()) + " >>"
		}
		resources += " >>"
		objects = append(objects, fmt.Sprintf(
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.0f %.0f] /Resources %s /Contents %d 0 R >>",
			PageW, PageH, resources, pos[i].content))

		for j := range pg.imgs {
			im := pg.imgs[j]
			objects = append(objects, fmt.Sprintf(
				"<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /%s /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>\nstream\n%s\nendstream",
				im.w, im.h, im.colorSpace, len(im.data), im.data))
		}
	}

	// Assemble with a cross-reference table.
	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n%\xE2\xE3\xCF\xD3\n")
	offsets := make([]int, len(objects)+1)
	for i, body := range objects {
		offsets[i+1] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	xrefPos := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n", len(objects)+1)
	out.WriteString("0000000000 65535 f\r\n")
	for i := 1; i <= len(objects); i++ {
		fmt.Fprintf(&out, "%010d 00000 n\r\n", offsets[i])
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		len(objects)+1, xrefPos)
	return out.Bytes()
}

// winAnsi maps the handful of common non-ASCII runes the packet uses to their
// WinAnsiEncoding (CP1252) byte values, so em dashes, middle dots, smart quotes
// and the like render correctly instead of degrading to '?'.
var winAnsi = map[rune]byte{
	'–': 0x96, // – en dash
	'—': 0x97, // — em dash
	'·': 0xB7, // · middle dot
	'•': 0x95, // • bullet
	'‘': 0x91, // ' left single quote
	'’': 0x92, // ' right single quote / apostrophe
	'“': 0x93, // " left double quote
	'”': 0x94, // " right double quote
	'…': 0x85, // … ellipsis
	' ': 0x20, // nbsp → space
}

// escapeText escapes the characters significant inside a PDF string literal,
// passes ASCII and known WinAnsi punctuation through, and degrades anything else
// to '?' (the packet's data is otherwise ASCII).
func escapeText(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 8)
	for _, r := range s {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == '(':
			b.WriteString(`\(`)
		case r == ')':
			b.WriteString(`\)`)
		case r >= 32 && r < 127:
			b.WriteByte(byte(r))
		default:
			if w, ok := winAnsi[r]; ok {
				b.WriteByte(w)
			} else {
				b.WriteByte('?')
			}
		}
	}
	return b.String()
}

// jpegInfo reads width, height and component count from a baseline/progressive
// JPEG by walking its markers to the Start-of-Frame.
func jpegInfo(b []byte) (w, h, comps int, err error) {
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return 0, 0, 0, errors.New("pdfgen: not a JPEG")
	}
	i := 2
	for i+1 < len(b) {
		if b[i] != 0xFF {
			i++
			continue
		}
		marker := b[i+1]
		i += 2
		// Standalone markers (no length): padding, RST, SOI/EOI.
		if marker == 0xFF || marker == 0x01 || (marker >= 0xD0 && marker <= 0xD9) {
			continue
		}
		if i+1 >= len(b) {
			break
		}
		segLen := int(b[i])<<8 | int(b[i+1])
		// SOF0..SOF15, excluding the non-frame markers DHT(C4), JPG(C8), DAC(CC).
		if marker >= 0xC0 && marker <= 0xCF && marker != 0xC4 && marker != 0xC8 && marker != 0xCC {
			if i+7 >= len(b) {
				break
			}
			h = int(b[i+3])<<8 | int(b[i+4])
			w = int(b[i+5])<<8 | int(b[i+6])
			comps = int(b[i+7])
			if w == 0 || h == 0 || comps == 0 {
				return 0, 0, 0, errors.New("pdfgen: bad JPEG dimensions")
			}
			return w, h, comps, nil
		}
		i += segLen
	}
	return 0, 0, 0, errors.New("pdfgen: no JPEG SOF marker")
}
