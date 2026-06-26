// Package qr is a small, dependency-free QR Code encoder — just enough to turn
// the lobby check-in URL ("https://ptr.example.org/checkin?c=ABCD1234") into a
// scannable matrix we render as an SVG poster. Same single-binary, zero-new-deps
// stance as internal/otp and internal/pdfgen.
//
// Scope is deliberately narrow: byte mode (UTF-8 bytes), error-correction level
// M (~15% recovery — the sweet spot for a printed poster), versions 1 through 10
// (a v10 symbol holds 271 bytes at level M, far more than any check-in URL). The
// hard, error-prone constants — data-codeword capacities, error-correction block
// layouts, alignment-pattern centres, and the format/version info — come from
// the QR spec (ISO/IEC 18004) and are unit-tested against its canonical worked
// example, so the only "computed" pieces are the Reed-Solomon ECC, the module
// placement, and mask selection.
package qr

import (
	"errors"
	"strings"
)

// ecLevelM is the error-correction level this package emits; its 2-bit format
// indicator is 0b00.
const ecFormatBits = 0 // level M

// version describes one QR version at level M.
type version struct {
	dataCodewords int      // total data codewords (level M)
	ecPerBlock    int      // EC codewords per block
	blocks        [][2]int // groups: {numBlocks, dataCodewordsPerBlock}
	alignment     []int    // alignment-pattern centre coordinates
}

// versionsM is the per-version table for level M, versions 1..10. Totals are
// checked in the tests (data + ec == the version's total codeword count).
var versionsM = []version{
	1:  {16, 10, [][2]int{{1, 16}}, nil},
	2:  {28, 16, [][2]int{{1, 28}}, []int{6, 18}},
	3:  {44, 26, [][2]int{{1, 44}}, []int{6, 22}},
	4:  {64, 18, [][2]int{{2, 32}}, []int{6, 26}},
	5:  {86, 24, [][2]int{{2, 43}}, []int{6, 30}},
	6:  {108, 16, [][2]int{{4, 27}}, []int{6, 34}},
	7:  {124, 18, [][2]int{{4, 31}}, []int{6, 22, 38}},
	8:  {154, 22, [][2]int{{2, 38}, {2, 39}}, []int{6, 24, 42}},
	9:  {182, 22, [][2]int{{3, 36}, {2, 37}}, []int{6, 26, 46}},
	10: {216, 26, [][2]int{{4, 43}, {1, 44}}, []int{6, 28, 50}},
}

// Encode returns the QR matrix for s (true = dark module), choosing the smallest
// version 1..10 that fits. It errors if the payload exceeds version 10 at level M.
func Encode(s string) ([][]bool, error) {
	data := []byte(s)
	ver := chooseVersion(len(data))
	if ver == 0 {
		return nil, errors.New("qr: payload too long for version 10 at level M")
	}
	v := versionsM[ver]

	bits := buildBitstream(data, ver, v.dataCodewords)
	codewords := bitsToBytes(bits)
	final := interleave(codewords, v)

	size := 17 + 4*ver
	mod := newGrid(size)
	fn := newGrid(size) // marks function-pattern modules (kept out of masking/data)
	placeFunctionPatterns(mod, fn, ver, v.alignment)
	placeData(mod, fn, final, size)

	best, mask := applyBestMask(mod, fn, size)
	placeFormatInfo(best, fn, mask, size)
	if ver >= 7 {
		placeVersionInfo(best, ver, size)
	}
	return best, nil
}

func chooseVersion(n int) int {
	for ver := 1; ver <= 10; ver++ {
		// Byte-mode header: 4-bit mode + char-count (8 bits for v1-9, 16 for v10+).
		ccBits := 8
		if ver >= 10 {
			ccBits = 16
		}
		capacityBits := versionsM[ver].dataCodewords * 8
		if 4+ccBits+8*n <= capacityBits {
			return ver
		}
	}
	return 0
}

// buildBitstream assembles the data bit string: mode + count + bytes, terminator,
// byte alignment, then the alternating pad codewords 0xEC / 0x11.
func buildBitstream(data []byte, ver, dataCodewords int) []bool {
	var b bitWriter
	b.writeBits(0b0100, 4) // byte mode
	ccBits := 8
	if ver >= 10 {
		ccBits = 16
	}
	b.writeBits(uint(len(data)), ccBits)
	for _, by := range data {
		b.writeBits(uint(by), 8)
	}

	capacity := dataCodewords * 8
	// Terminator: up to four 0 bits.
	if rem := capacity - len(b.bits); rem > 0 {
		t := 4
		if rem < 4 {
			t = rem
		}
		b.writeBits(0, t)
	}
	// Pad to a byte boundary.
	for len(b.bits)%8 != 0 {
		b.writeBits(0, 1)
	}
	// Fill remaining codewords with the spec's alternating pad bytes.
	for pad := 0; len(b.bits) < capacity; pad++ {
		if pad%2 == 0 {
			b.writeBits(0xEC, 8)
		} else {
			b.writeBits(0x11, 8)
		}
	}
	return b.bits
}

func bitsToBytes(bits []bool) []byte {
	out := make([]byte, len(bits)/8)
	for i := range out {
		var v byte
		for j := 0; j < 8; j++ {
			if bits[i*8+j] {
				v |= 1 << (7 - j)
			}
		}
		out[i] = v
	}
	return out
}

// interleave splits the data codewords into the version's EC blocks, computes
// Reed-Solomon EC codewords per block, and interleaves them per the spec (all
// data codewords column-major across blocks, then all EC codewords).
func interleave(data []byte, v version) []byte {
	type block struct{ data, ec []byte }
	var blocks []block
	off := 0
	for _, g := range v.blocks {
		count, size := g[0], g[1]
		for i := 0; i < count; i++ {
			d := data[off : off+size]
			off += size
			blocks = append(blocks, block{data: d, ec: reedSolomon(d, v.ecPerBlock)})
		}
	}
	var out []byte
	maxData := 0
	for _, bl := range blocks {
		if len(bl.data) > maxData {
			maxData = len(bl.data)
		}
	}
	for i := 0; i < maxData; i++ {
		for _, bl := range blocks {
			if i < len(bl.data) {
				out = append(out, bl.data[i])
			}
		}
	}
	for i := 0; i < v.ecPerBlock; i++ {
		for _, bl := range blocks {
			out = append(out, bl.ec[i])
		}
	}
	return out
}

// ── bit writer ───────────────────────────────────────────────────────────────

type bitWriter struct{ bits []bool }

func (b *bitWriter) writeBits(v uint, n int) {
	for i := n - 1; i >= 0; i-- {
		b.bits = append(b.bits, (v>>uint(i))&1 == 1)
	}
}

// ── grid ─────────────────────────────────────────────────────────────────────

func newGrid(size int) [][]bool {
	g := make([][]bool, size)
	for i := range g {
		g[i] = make([]bool, size)
	}
	return g
}

func cloneGrid(g [][]bool) [][]bool {
	out := make([][]bool, len(g))
	for i := range g {
		out[i] = make([]bool, len(g[i]))
		copy(out[i], g[i])
	}
	return out
}

// ── function patterns ────────────────────────────────────────────────────────

func placeFunctionPatterns(mod, fn [][]bool, ver int, alignment []int) {
	size := len(mod)
	// Three finder patterns + their separators.
	placeFinder(mod, fn, 0, 0)
	placeFinder(mod, fn, size-7, 0)
	placeFinder(mod, fn, 0, size-7)

	// Timing patterns (row/col 6).
	for i := 8; i < size-8; i++ {
		on := i%2 == 0
		setFn(mod, fn, 6, i, on)
		setFn(mod, fn, i, 6, on)
	}

	// Alignment patterns at every centre pair, skipping those overlapping a finder.
	for _, r := range alignment {
		for _, c := range alignment {
			if isFinderArea(r, c, size) {
				continue
			}
			placeAlignment(mod, fn, r, c)
		}
	}

	// Dark module (always set) just above the bottom-left finder.
	setFn(mod, fn, size-8, 8, true)

	// Reserve the format-info regions (filled later) so data placement skips them.
	reserveFormat(fn, size)
	if ver >= 7 {
		reserveVersion(fn, size)
	}
}

func placeFinder(mod, fn [][]bool, r, c int) {
	for dr := -1; dr <= 7; dr++ {
		for dc := -1; dc <= 7; dc++ {
			rr, cc := r+dr, c+dc
			if rr < 0 || cc < 0 || rr >= len(mod) || cc >= len(mod) {
				continue
			}
			on := false
			if dr >= 0 && dr <= 6 && dc >= 0 && dc <= 6 {
				// 7x7 finder: outer ring + 3x3 centre dark.
				if dr == 0 || dr == 6 || dc == 0 || dc == 6 || (dr >= 2 && dr <= 4 && dc >= 2 && dc <= 4) {
					on = true
				}
			}
			setFn(mod, fn, rr, cc, on)
		}
	}
}

func placeAlignment(mod, fn [][]bool, r, c int) {
	for dr := -2; dr <= 2; dr++ {
		for dc := -2; dc <= 2; dc++ {
			on := dr == -2 || dr == 2 || dc == -2 || dc == 2 || (dr == 0 && dc == 0)
			setFn(mod, fn, r+dr, c+dc, on)
		}
	}
}

func isFinderArea(r, c, size int) bool {
	near := func(fr, fc int) bool {
		return r >= fr-2 && r <= fr+2 && c >= fc-2 && c <= fc+2
	}
	return near(3, 3) || near(3, size-4) || near(size-4, 3)
}

func setFn(mod, fn [][]bool, r, c int, on bool) {
	mod[r][c] = on
	fn[r][c] = true
}

func reserveFormat(fn [][]bool, size int) {
	for i := 0; i <= 8; i++ {
		fn[8][i] = true
		fn[i][8] = true
	}
	for i := 0; i < 8; i++ {
		fn[8][size-1-i] = true
		fn[size-1-i][8] = true
	}
}

func reserveVersion(fn [][]bool, size int) {
	for i := 0; i < 6; i++ {
		for j := 0; j < 3; j++ {
			fn[i][size-11+j] = true
			fn[size-11+j][i] = true
		}
	}
}

// ── data placement ───────────────────────────────────────────────────────────

// placeData walks the standard upward/downward zigzag of 2-column strips from
// the bottom-right, skipping the timing column at x=6 and any function module.
func placeData(mod, fn [][]bool, data []byte, size int) {
	bitAt := func(i int) bool {
		if i/8 >= len(data) {
			return false
		}
		return data[i/8]&(1<<(7-uint(i%8))) != 0
	}
	idx := 0
	up := true
	for col := size - 1; col > 0; col -= 2 {
		if col == 6 { // skip the vertical timing column
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
				mod[row][c] = bitAt(idx)
				idx++
			}
		}
		up = !up
	}
}

// ── masking ──────────────────────────────────────────────────────────────────

func maskAt(pattern, r, c int) bool {
	switch pattern {
	case 0:
		return (r+c)%2 == 0
	case 1:
		return r%2 == 0
	case 2:
		return c%3 == 0
	case 3:
		return (r+c)%3 == 0
	case 4:
		return (r/2+c/3)%2 == 0
	case 5:
		return (r*c)%2+(r*c)%3 == 0
	case 6:
		return ((r*c)%2+(r*c)%3)%2 == 0
	case 7:
		return ((r+c)%2+(r*c)%3)%2 == 0
	}
	return false
}

func applyBestMask(mod, fn [][]bool, size int) ([][]bool, int) {
	bestScore := 1 << 30
	var best [][]bool
	bestMask := 0
	for pattern := 0; pattern < 8; pattern++ {
		g := cloneGrid(mod)
		for r := 0; r < size; r++ {
			for c := 0; c < size; c++ {
				if !fn[r][c] && maskAt(pattern, r, c) {
					g[r][c] = !g[r][c]
				}
			}
		}
		// Format info influences the penalty score; place it provisionally.
		placeFormatInfo(g, fn, pattern, size)
		if s := penalty(g); s < bestScore {
			bestScore = s
			best = g
			bestMask = pattern
		}
	}
	return best, bestMask
}

// penalty applies the four QR scoring rules.
func penalty(g [][]bool) int {
	size := len(g)
	score := 0

	// Rule 1: runs of 5+ same-colour modules in each row and column.
	runScore := func(get func(i, j int) bool) int {
		s := 0
		for i := 0; i < size; i++ {
			run := 1
			prev := get(i, 0)
			for j := 1; j < size; j++ {
				cur := get(i, j)
				if cur == prev {
					run++
				} else {
					if run >= 5 {
						s += 3 + (run - 5)
					}
					run = 1
					prev = cur
				}
			}
			if run >= 5 {
				s += 3 + (run - 5)
			}
		}
		return s
	}
	score += runScore(func(i, j int) bool { return g[i][j] })
	score += runScore(func(i, j int) bool { return g[j][i] })

	// Rule 2: 2x2 blocks of the same colour.
	for r := 0; r < size-1; r++ {
		for c := 0; c < size-1; c++ {
			if g[r][c] == g[r][c+1] && g[r][c] == g[r+1][c] && g[r][c] == g[r+1][c+1] {
				score += 3
			}
		}
	}

	// Rule 3: finder-like 1:1:3:1:1 patterns with a 4-module light run on a side.
	pat1 := []bool{true, false, true, true, true, false, true, false, false, false, false}
	pat2 := []bool{false, false, false, false, true, false, true, true, true, false, true}
	matches := func(get func(k int) bool) bool {
		m1, m2 := true, true
		for k := 0; k < 11; k++ {
			if get(k) != pat1[k] {
				m1 = false
			}
			if get(k) != pat2[k] {
				m2 = false
			}
		}
		return m1 || m2
	}
	for r := 0; r < size; r++ {
		for c := 0; c <= size-11; c++ {
			if matches(func(k int) bool { return g[r][c+k] }) {
				score += 40
			}
		}
	}
	for c := 0; c < size; c++ {
		for r := 0; r <= size-11; r++ {
			if matches(func(k int) bool { return g[r+k][c] }) {
				score += 40
			}
		}
	}

	// Rule 4: deviation of dark-module proportion from 50%.
	dark := 0
	for r := 0; r < size; r++ {
		for c := 0; c < size; c++ {
			if g[r][c] {
				dark++
			}
		}
	}
	total := size * size
	percent := dark * 100 / total
	dev := percent - 50
	if dev < 0 {
		dev = -dev
	}
	score += (dev / 5) * 10

	return score
}

// ── format & version information ─────────────────────────────────────────────

func placeFormatInfo(g, fn [][]bool, mask, size int) {
	bits := formatBits(mask)
	// Around the top-left finder.
	for i := 0; i <= 5; i++ {
		g[8][i] = bit(bits, 14-i)
	}
	g[8][7] = bit(bits, 8)
	g[8][8] = bit(bits, 7)
	g[7][8] = bit(bits, 6)
	for i := 0; i <= 5; i++ {
		g[i][8] = bit(bits, i)
	}
	// The split copy along the bottom and right edges.
	for i := 0; i < 8; i++ {
		g[size-1-i][8] = bit(bits, i)
	}
	for i := 0; i < 7; i++ {
		g[8][size-7+i] = bit(bits, 8+i)
	}
	_ = fn
}

func placeVersionInfo(g [][]bool, ver, size int) {
	bits := versionBits(ver)
	for i := 0; i < 18; i++ {
		b := bit(bits, i)
		r, c := i/3, i%3
		g[r][size-11+c] = b
		g[size-11+c][r] = b
	}
}

func bit(v uint, i int) bool { return (v>>uint(i))&1 == 1 }

// formatBits returns the 15-bit format string (data = level || mask) with BCH
// error correction and the standard 0x5412 masking, MSB at index 14.
func formatBits(mask int) uint {
	data := uint(ecFormatBits<<3 | mask) // 5 bits
	rem := data << 10
	gen := uint(0b10100110111) // x^10 + x^8 + x^5 + x^4 + x^2 + x + 1
	for i := 14; i >= 10; i-- {
		if rem&(1<<uint(i)) != 0 {
			rem ^= gen << uint(i-10)
		}
	}
	return ((data << 10) | rem) ^ 0b101010000010010
}

// versionBits returns the 18-bit version string (6 data + 12 BCH) for v>=7.
func versionBits(ver int) uint {
	data := uint(ver)
	rem := data << 12
	gen := uint(0b1111100100101) // x^12 + ... (0x1F25)
	for i := 17; i >= 12; i-- {
		if rem&(1<<uint(i)) != 0 {
			rem ^= gen << uint(i-12)
		}
	}
	return (data << 12) | rem
}

// ── Reed-Solomon over GF(256) (primitive 0x11D) ──────────────────────────────

var (
	gfExp [512]byte
	gfLog [256]byte
)

func init() {
	x := 1
	for i := 0; i < 255; i++ {
		gfExp[i] = byte(x)
		gfLog[x] = byte(i)
		x <<= 1
		if x&0x100 != 0 {
			x ^= 0x11D
		}
	}
	for i := 255; i < 512; i++ {
		gfExp[i] = gfExp[i-255]
	}
}

func gfMul(a, b byte) byte {
	if a == 0 || b == 0 {
		return 0
	}
	return gfExp[int(gfLog[a])+int(gfLog[b])]
}

// rsGenerator returns the generator polynomial of degree n (coefficients,
// highest-degree first implicit leading 1 dropped — stored as full n+1 then used).
func rsGenerator(n int) []byte {
	g := []byte{1}
	for i := 0; i < n; i++ {
		// multiply g by (x - alpha^i)
		next := make([]byte, len(g)+1)
		for j := 0; j < len(g); j++ {
			next[j] ^= g[j]
			next[j+1] ^= gfMul(g[j], gfExp[i])
		}
		g = next
	}
	return g
}

// reedSolomon returns n EC codewords for the data block.
func reedSolomon(data []byte, n int) []byte {
	gen := rsGenerator(n)
	rem := make([]byte, n)
	for _, d := range data {
		factor := d ^ rem[0]
		copy(rem, rem[1:])
		rem[n-1] = 0
		for j := 0; j < n; j++ {
			rem[j] ^= gfMul(gen[j+1], factor)
		}
	}
	return rem
}

// ── SVG rendering ────────────────────────────────────────────────────────────

// SVG renders the matrix as a crisp, scalable QR image with a quiet zone. module
// is the size of one module in the SVG's user units; quiet is the border in
// modules (4 is the spec minimum). The returned SVG scales to any print size.
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
