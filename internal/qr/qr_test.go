package qr

import "testing"

// The canonical ISO/IEC 18004 worked example: the 16 data codewords of
// "01234567" at version 1, level M, and the 10 Reed-Solomon EC codewords the
// spec says they must produce. Reproducing these exactly exercises the GF(256)
// tables, the generator polynomial and the polynomial division — the parts of
// the encoder that aren't a hand-transcribed lookup table.
func TestReedSolomonCanonical(t *testing.T) {
	data := []byte{0x10, 0x20, 0x0C, 0x56, 0x61, 0x80, 0xEC, 0x11, 0xEC, 0x11, 0xEC, 0x11, 0xEC, 0x11, 0xEC, 0x11}
	want := []byte{0xA5, 0x24, 0xD4, 0xC1, 0xED, 0x36, 0xC7, 0x87, 0x2C, 0x55}
	got := reedSolomon(data, 10)
	if len(got) != len(want) {
		t.Fatalf("EC length = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("EC[%d] = %#02x, want %#02x (full %#x)", i, got[i], want[i], got)
		}
	}
}

// formatBits must match the published level-M format strings (BCH + 0x5412 mask).
func TestFormatBits(t *testing.T) {
	cases := map[int]uint{
		0: 0b101010000010010,
		5: 0b100000011001110,
	}
	for mask, want := range cases {
		if got := formatBits(mask); got != want {
			t.Errorf("formatBits(%d) = %015b, want %015b", mask, got, want)
		}
	}
}

// versionBits must match the published value for version 7 (0x07C94).
func TestVersionBits(t *testing.T) {
	if got := versionBits(7); got != 0x07C94 {
		t.Errorf("versionBits(7) = %#x, want 0x07C94", got)
	}
}

// Each version's data + EC codeword totals must equal the QR total-codeword
// count for that version (a transcription check on the hardcoded block tables).
func TestBlockTotals(t *testing.T) {
	totals := map[int]int{1: 26, 2: 44, 3: 70, 4: 100, 5: 134, 6: 172, 7: 196, 8: 242, 9: 292, 10: 346}
	for ver := 1; ver <= 10; ver++ {
		v := versionsM[ver]
		dataSum, blocks := 0, 0
		for _, g := range v.blocks {
			dataSum += g[0] * g[1]
			blocks += g[0]
		}
		if dataSum != v.dataCodewords {
			t.Errorf("v%d: block data sum %d != dataCodewords %d", ver, dataSum, v.dataCodewords)
		}
		if got := dataSum + blocks*v.ecPerBlock; got != totals[ver] {
			t.Errorf("v%d: total codewords %d, want %d", ver, got, totals[ver])
		}
	}
}

// Encode picks the right version and lays down valid finder + timing patterns.
func TestEncodeStructure(t *testing.T) {
	m, err := Encode("HTTPS://EXAMPLE.COM/CHECKIN?C=ABCD1234")
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 29 { // version 3 → 17 + 4*3
		t.Fatalf("size = %d, want 29 (version 3)", len(m))
	}
	// Finder pattern centres are dark, surrounding ring corners dark.
	for _, p := range [][2]int{{0, 0}, {0, len(m) - 7}, {len(m) - 7, 0}} {
		r, c := p[0], p[1]
		if !m[r][c] || !m[r+3][c+3] || m[r+1][c+1] {
			t.Errorf("finder at (%d,%d) malformed", r, c)
		}
	}
	// Timing row alternates starting dark at column 8.
	for c := 8; c < len(m)-8; c++ {
		want := c%2 == 0
		if m[6][c] != want {
			t.Errorf("timing[6][%d] = %v, want %v", c, m[6][c], want)
		}
	}
}

func TestEncodeTooLong(t *testing.T) {
	long := make([]byte, 300)
	for i := range long {
		long[i] = 'A'
	}
	if _, err := Encode(string(long)); err == nil {
		t.Error("expected error for over-capacity payload")
	}
}
