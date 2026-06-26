package qr

import (
	"image"
	"image/color"
	"testing"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
)

// render rasterizes a module matrix to a grayscale image with a white quiet zone
// — the same thing a phone camera sees on the printed poster.
func render(m [][]bool, scale, quiet int) image.Image {
	n := len(m)
	dim := (n + 2*quiet) * scale
	img := image.NewGray(image.Rect(0, 0, dim, dim))
	for i := range img.Pix {
		img.Pix[i] = 255 // white background (incl. quiet zone)
	}
	for r := 0; r < n; r++ {
		for c := 0; c < n; c++ {
			if !m[r][c] {
				continue
			}
			x0, y0 := (c+quiet)*scale, (r+quiet)*scale
			for dy := 0; dy < scale; dy++ {
				for dx := 0; dx < scale; dx++ {
					img.SetGray(x0+dx, y0+dy, color.Gray{Y: 0})
				}
			}
		}
	}
	return img
}

// TestEncodeScannable is the test that actually matters: render the QR and decode
// it with gozxing (a Go port of ZXing — the same algorithm Android/Google Lens
// use). If this reproduces the input string, a real phone can scan it.
func TestEncodeScannable(t *testing.T) {
	inputs := []string{
		"https://ptr.example.org/checkin?c=ABCD-23",
		"http://127.0.0.1:8000/checkin?c=PR2A-7E",
		"K7Q2-9F",
		"https://pretrial.knoxcounty.org/checkin?c=ZZZ9-Q7",
	}
	reader := qrcode.NewQRCodeReader()
	for _, in := range inputs {
		m, err := Encode(in)
		if err != nil {
			t.Fatalf("Encode(%q): %v", in, err)
		}
		bmp, err := gozxing.NewBinaryBitmapFromImage(render(m, 6, 4))
		if err != nil {
			t.Fatalf("bitmap(%q): %v", in, err)
		}
		res, err := reader.Decode(bmp, nil)
		if err != nil {
			t.Fatalf("decode(%q) failed — a phone would not scan this: %v", in, err)
		}
		if res.GetText() != in {
			t.Errorf("round-trip mismatch:\n  in:  %q\n  out: %q", in, res.GetText())
		}
	}
}

func TestSVGWellFormed(t *testing.T) {
	m, err := Encode("https://example.com/checkin?c=TEST")
	if err != nil {
		t.Fatal(err)
	}
	svg := SVG(m, 8, 4)
	if len(svg) < 100 || svg[:4] != "<svg" {
		t.Fatal("SVG output malformed")
	}
	if svg[len(svg)-6:] != "</svg>" {
		t.Error("SVG not closed")
	}
}
