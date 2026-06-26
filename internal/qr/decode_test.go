package qr

import "testing"

// decode reverses the encoder enough to recover the original payload from a
// rendered matrix WITHOUT using error correction: read the format info to find
// the mask, unmask, read the data modules in zigzag order, de-interleave the
// blocks, and parse the byte-mode header. If this reproduces the input string,
// the symbol is genuinely decodable — the strongest end-to-end check short of a
// third-party scanner.
func decode(t *testing.T, m [][]bool) string {
	t.Helper()
	size := len(m)
	ver := (size - 17) / 4
	v := versionsM[ver]

	// Reconstruct the function-pattern map for this version.
	fnMod := newGrid(size)
	fn := newGrid(size)
	placeFunctionPatterns(fnMod, fn, ver, v.alignment)

	// Recover the mask from the format info (first copy, around the TL finder).
	var bits uint
	read := func(set bool, shift int) {
		if set {
			bits |= 1 << uint(shift)
		}
	}
	for i := 0; i <= 5; i++ {
		read(m[8][i], 14-i)
	}
	read(m[8][7], 8)
	read(m[8][8], 7)
	read(m[7][8], 6)
	for i := 0; i <= 5; i++ {
		read(m[i][8], i)
	}
	mask := -1
	for cand := 0; cand < 8; cand++ {
		if formatBits(cand) == bits {
			mask = cand
			break
		}
	}
	if mask < 0 {
		t.Fatalf("format info %015b matched no level-M mask", bits)
	}

	// Unmask and read the data modules in the encoder's traversal order.
	var stream []bool
	up := true
	for col := size - 1; col > 0; col -= 2 {
		if col == 6 {
			col--
		}
		for n := 0; n < size; n++ {
			row := n
			if up {
				row = size - 1 - n
			}
			for k := 0; k < 2; k++ {
				c := col - k
				if fn[row][c] {
					continue
				}
				bit := m[row][c]
				if maskAt(mask, row, c) {
					bit = !bit
				}
				stream = append(stream, bit)
			}
		}
		up = !up
	}
	codewords := bitsToBytes(stream)

	// De-interleave: split the data section back into blocks, then concatenate.
	var lens []int
	for _, g := range v.blocks {
		for i := 0; i < g[0]; i++ {
			lens = append(lens, g[1])
		}
	}
	maxData := 0
	for _, l := range lens {
		if l > maxData {
			maxData = l
		}
	}
	blocks := make([][]byte, len(lens))
	idx := 0
	for i := 0; i < maxData; i++ {
		for b := range lens {
			if i < lens[b] {
				blocks[b] = append(blocks[b], codewords[idx])
				idx++
			}
		}
	}
	var data []byte
	for _, b := range blocks {
		data = append(data, b...)
	}

	// Parse the byte-mode header: 4-bit mode, 8-bit count, then count bytes.
	rd := &bitReader{bytes: data}
	if rd.read(4) != 0b0100 {
		t.Fatal("mode indicator is not byte mode")
	}
	n := int(rd.read(8))
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		out[i] = byte(rd.read(8))
	}
	return string(out)
}

type bitReader struct {
	bytes []byte
	pos   int
}

func (r *bitReader) read(n int) uint {
	var v uint
	for i := 0; i < n; i++ {
		byteIdx := r.pos / 8
		bit := uint(0)
		if byteIdx < len(r.bytes) {
			bit = uint(r.bytes[byteIdx]>>(7-uint(r.pos%8))) & 1
		}
		v = v<<1 | bit
		r.pos++
	}
	return v
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	inputs := []string{
		"https://ptr.example.org/checkin?c=ABCD-23",
		"K7Q2-9F",
		"http://127.0.0.1:8137/checkin?c=PR2A-7E",
		"A longer payload that should push selection into a higher version number to exercise multi-block interleaving paths.",
	}
	for _, in := range inputs {
		m, err := Encode(in)
		if err != nil {
			t.Fatalf("Encode(%q): %v", in, err)
		}
		if got := decode(t, m); got != in {
			t.Errorf("round-trip mismatch:\n  in:  %q\n  out: %q (v%d)", in, got, (len(m)-17)/4)
		}
	}
}
