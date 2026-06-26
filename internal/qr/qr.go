// Package qr renders the QR code for the lobby check-in poster.
//
// It wraps rsc.io/qr — Russ Cox's small, pure-Go, no-transitive-deps QR encoder
// — and adds a compact SVG renderer. An earlier hand-rolled encoder lived here
// but shipped symbols some phones wouldn't scan (a format-info placement bug a
// self-referential round-trip test couldn't catch); correctness won out over the
// project's zero-new-deps preference. A ZXing-based decode test (qr_test.go)
// proves the output is actually scannable.
package qr

import (
	"strings"

	"rsc.io/qr"
)

// Encode returns the QR matrix for s at error-correction level M (~15% recovery,
// the sweet spot for a printed poster). matrix[y][x] is true for a dark module.
func Encode(s string) ([][]bool, error) {
	code, err := qr.Encode(s, qr.M)
	if err != nil {
		return nil, err
	}
	n := code.Size
	m := make([][]bool, n)
	for y := 0; y < n; y++ {
		m[y] = make([]bool, n)
		for x := 0; x < n; x++ {
			m[y][x] = code.Black(x, y)
		}
	}
	return m, nil
}

// SVG renders the matrix as a crisp, scalable QR image with a quiet zone. module
// is the size of one module in user units; quiet is the border in modules (4 is
// the spec minimum). The returned SVG scales to any print size.
func SVG(matrix [][]bool, module, quiet int) string {
	n := len(matrix)
	dim := (n + 2*quiet) * module
	var b strings.Builder
	b.Grow(n*n*24 + 256)
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" width="`)
	itoa(&b, dim)
	b.WriteString(`" height="`)
	itoa(&b, dim)
	b.WriteString(`" viewBox="0 0 `)
	itoa(&b, dim)
	b.WriteByte(' ')
	itoa(&b, dim)
	b.WriteString(`" shape-rendering="crispEdges">`)
	b.WriteString(`<rect width="100%" height="100%" fill="#fff"/><path fill="#000" d="`)
	for r := 0; r < n; r++ {
		for c := 0; c < n; c++ {
			if !matrix[r][c] {
				continue
			}
			x := (c + quiet) * module
			y := (r + quiet) * module
			b.WriteByte('M')
			itoa(&b, x)
			b.WriteByte(' ')
			itoa(&b, y)
			b.WriteByte('h')
			itoa(&b, module)
			b.WriteByte('v')
			itoa(&b, module)
			b.WriteByte('h')
			itoa(&b, -module)
			b.WriteByte('z')
		}
	}
	b.WriteString(`"/></svg>`)
	return b.String()
}

func itoa(b *strings.Builder, n int) {
	if n == 0 {
		b.WriteByte('0')
		return
	}
	if n < 0 {
		b.WriteByte('-')
		n = -n
	}
	var tmp [12]byte
	i := len(tmp)
	for n > 0 {
		i--
		tmp[i] = byte('0' + n%10)
		n /= 10
	}
	b.Write(tmp[i:])
}
