package pdfgen

import (
	"bytes"
	"testing"
)

func TestJPEGInfo(t *testing.T) {
	// Minimal marker stream: SOI, then a SOF0 declaring 400x200, 3 components.
	jpeg := []byte{
		0xFF, 0xD8, // SOI
		0xFF, 0xC0, 0x00, 0x11, 0x08, 0x00, 0xC8, 0x01, 0x90, 0x03, // SOF0 200x400 3comp
		0x01, 0x22, 0x00, 0x02, 0x11, 0x01, 0x03, 0x11, 0x01,
	}
	w, h, comps, err := jpegInfo(jpeg)
	if err != nil {
		t.Fatal(err)
	}
	if w != 400 || h != 200 || comps != 3 {
		t.Fatalf("got %dx%d comps=%d, want 400x200 comps=3", w, h, comps)
	}
	if _, _, _, err := jpegInfo([]byte{0x00, 0x01}); err == nil {
		t.Error("expected error for non-JPEG")
	}
}

func TestDocStructure(t *testing.T) {
	d := New()
	p := d.AddPage()
	p.Rect(0, PageH-60, PageW, 60, 0.15)
	p.Text(40, PageH-40, 16, true, 1, "Court Packet")
	p.Line(40, PageH-70, PageW-40, PageH-70, 1, 0.6)
	out := d.Bytes()
	if !bytes.HasPrefix(out, []byte("%PDF-1.4")) {
		t.Error("missing PDF header")
	}
	if !bytes.Contains(out, []byte("%%EOF")) {
		t.Error("missing EOF")
	}
	if !bytes.Contains(out, []byte("/Type /Catalog")) || !bytes.Contains(out, []byte("startxref")) {
		t.Error("missing catalog or xref")
	}
}
