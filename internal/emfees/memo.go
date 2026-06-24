package emfees

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// memoTemplate is the user's own past-due memo (assets/memo_template.docx, copied
// verbatim from the canonical skill). Embedding it keeps the single-binary deploy
// story — no external file to ship to ptr1 — and guarantees the letter format is
// reused, not recreated.
//
//go:embed assets/memo_template.docx
var memoTemplate []byte

// emSpace (U+2002) is the template's placeholder character. Each fillable field is
// a Word FORMTEXT form field padded with exactly 5 consecutive runs of this char.
const emSpace = " "

// placeholderRun is the exact <w:t> element of one placeholder run. It appears 35
// times (7 fields × 5 runs) and nowhere else in the document, so a plain ordered
// scan locates every field unambiguously.
const placeholderRun = "<w:t>" + emSpace + "</w:t>"

var reNonFilename = regexp.MustCompile(`[^A-Za-z0-9_\-]`)

// MemoFilename builds "LAST_FIRST_MIDDLE_IDN.docx" (port of safe_filename).
func MemoFilename(rec Rec) string {
	parts := strings.Fields(strings.ReplaceAll(rec.Name, ",", " "))
	safe := reNonFilename.ReplaceAllString(strings.Join(parts, "_"), "")
	if len(safe) > 80 {
		safe = safe[:80]
	}
	return safe + "_" + rec.IDN + ".docx"
}

// DateString formats the as-of date as "M/D/YYYY" (no zero padding) — the exact
// format the skill prints on the Date line.
func DateString(res Result) string {
	d := res.AsOf
	return fmt.Sprintf("%d/%d/%d", int(d.Month()), d.Day(), d.Year())
}

// FillMemo renders one filled-in memo .docx for a record. The seven fields are
// filled in document order: Date, Court, Defendant, IDN, Warrant/Docket, GPS Type,
// Arrearage — exactly the order generate_memos.py fills paragraphs 10/13/17. An
// empty value (e.g. Court or a placeholder case number) leaves the field as
// em-spaces for the officer to complete by hand, matching the Python.
func FillMemo(rec Rec, dateStr string) ([]byte, error) {
	values := []string{
		dateStr,           // P10 cluster 1 — Date
		rec.Court,         // P10 cluster 2 — Court
		rec.Name,          // P13 cluster 1 — Defendant
		rec.IDN,           // P13 cluster 2 — IDN
		rec.Case,          // P13 cluster 3 — Warrant/Docket
		rec.Type,          // P17 cluster 1 — GPS Type
		Money(rec.Behind), // P17 cluster 2 — Arrearage
	}
	return fillTemplate(values)
}

func fillTemplate(values []string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(memoTemplate), int64(len(memoTemplate)))
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		if f.Name == "word/document.xml" {
			filled, ferr := fillClusters(string(data), values)
			if ferr != nil {
				return nil, ferr
			}
			data = []byte(filled)
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: f.Name, Method: zip.Deflate})
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// fillClusters replaces each cluster of 5 consecutive placeholder runs with the
// next value: the first run gets the (XML-escaped) value, the other 4 are blanked.
// A blank value leaves the whole cluster untouched (officer fills by hand). This is
// a direct port of generate_memos.fill_clusters operating on the document XML.
//
// #19 — The FORMTEXT mapping is positional: cluster N receives values[N]. If a Word
// re-save changes the placeholder-run count (adds/removes a field, or splits a run),
// the values silently misalign — the dollar arrearage could land in the GPS-type
// blank. To make that loud instead of silent, the template MUST contain exactly
// 5*len(values) placeholder runs forming exactly len(values) full 5-run clusters;
// any deviation returns a descriptive error rather than a mis-filled memo. (Blank
// values still consume their 5-run cluster, so the count check holds regardless.)
func fillClusters(xml string, values []string) (string, error) {
	if got := strings.Count(xml, placeholderRun); got != 5*len(values) {
		return "", fmt.Errorf("memo template FORMTEXT mismatch: found %d placeholder runs, want %d (%d fields × 5); the template's form fields no longer align — re-export memo_template.docx",
			got, 5*len(values), len(values))
	}
	var b strings.Builder
	b.Grow(len(xml) + 256)
	i := 0
	cluster := 0
	posInCluster := 0
	for {
		idx := strings.Index(xml[i:], placeholderRun)
		if idx < 0 {
			b.WriteString(xml[i:])
			break
		}
		abs := i + idx
		b.WriteString(xml[i:abs])

		val := ""
		if cluster < len(values) {
			val = values[cluster]
		}
		switch {
		case val == "":
			b.WriteString(placeholderRun) // leave placeholder as-is
		case posInCluster == 0:
			b.WriteString(`<w:t xml:space="preserve">` + escapeXML(val) + `</w:t>`)
		default:
			b.WriteString(`<w:t></w:t>`) // blank the trailing runs of a filled field
		}

		i = abs + len(placeholderRun)
		posInCluster++
		if posInCluster == 5 {
			posInCluster = 0
			cluster++
		}
	}
	// Total-run count is exact (checked above) and clusters are whole multiples of 5,
	// so exactly len(values) clusters are consumed. Assert it to fail loudly if the
	// run-walk logic itself ever regresses.
	if cluster != len(values) || posInCluster != 0 {
		return "", fmt.Errorf("memo template FORMTEXT mismatch: consumed %d clusters (+%d partial), want %d full", cluster, posInCluster, len(values))
	}
	return b.String(), nil
}

func escapeXML(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return r.Replace(s)
}

// Money formats a dollar amount as "$1,234.00" (port of f"${v:,.2f}").
func Money(v float64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	s := strconv.FormatFloat(v, 'f', 2, 64) // "1234.00"
	dot := strings.IndexByte(s, '.')
	intPart, frac := s[:dot], s[dot:]
	var b strings.Builder
	n := len(intPart)
	for i := 0; i < n; i++ {
		if i > 0 && (n-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteByte(intPart[i])
	}
	out := "$" + b.String() + frac
	if neg {
		out = "-" + out
	}
	return out
}

// MemosZip builds a single .zip with Open/ and Closed/ subfolders, one filled memo
// per record — the same layout the skill writes to disk. Pass kind "open", "closed",
// or "all".
func MemosZip(res Result, kind string) ([]byte, error) {
	dateStr := DateString(res)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(folder string, recs []Rec) error {
		for _, rec := range recs {
			doc, err := FillMemo(rec, dateStr)
			if err != nil {
				return err
			}
			// docx is already compressed; store to avoid double-deflation.
			w, err := zw.CreateHeader(&zip.FileHeader{Name: folder + "/" + MemoFilename(rec), Method: zip.Store})
			if err != nil {
				return err
			}
			if _, err := w.Write(doc); err != nil {
				return err
			}
		}
		return nil
	}
	if kind == "open" || kind == "all" {
		if err := add("Open", res.Open); err != nil {
			return nil, err
		}
	}
	if kind == "closed" || kind == "all" {
		if err := add("Closed", res.Closed); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
