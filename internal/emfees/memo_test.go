package emfees

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
)

// readDocXML fills a memo and returns its word/document.xml as a string.
func readDocXML(t *testing.T, rec Rec, dateStr string) string {
	t.Helper()
	doc, err := FillMemo(rec, dateStr)
	if err != nil {
		t.Fatalf("FillMemo: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(doc), int64(len(doc)))
	if err != nil {
		t.Fatalf("result is not a valid zip/docx: %v", err)
	}
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			rc.Close()
			return string(b)
		}
	}
	t.Fatal("word/document.xml missing from filled memo")
	return ""
}

func TestFillMemoFieldsLandAndBlanksRemain(t *testing.T) {
	rec := Rec{
		Name: "SMITH, JOHN", IDN: "12345", Case: "@100",
		Court: "", // blank Court -> its cluster must remain as em-spaces
		Type:  "ALLIED", Behind: 1300,
	}
	xml := readDocXML(t, rec, "5/31/2026")

	// Every filled value should appear as a preserved text run.
	for _, want := range []string{
		`<w:t xml:space="preserve">5/31/2026</w:t>`,
		`<w:t xml:space="preserve">SMITH, JOHN</w:t>`,
		`<w:t xml:space="preserve">12345</w:t>`,
		`<w:t xml:space="preserve">@100</w:t>`,
		`<w:t xml:space="preserve">ALLIED</w:t>`,
		`<w:t xml:space="preserve">$1,300.00</w:t>`,
	} {
		if !strings.Contains(xml, want) {
			t.Fatalf("filled memo missing %q", want)
		}
	}

	// 7 fields × 5 runs = 35 placeholder runs. Court is blank, so exactly its 5
	// remain; the other 6 fields' first run is replaced and 4 blanked.
	if n := strings.Count(xml, placeholderRun); n != 5 {
		t.Fatalf("remaining placeholder runs = %d, want 5 (Court only)", n)
	}
}

func TestFillMemoEscapesXML(t *testing.T) {
	rec := Rec{Name: "A & B <CO>", IDN: "1", Case: "@1", Court: "Div I", Type: "SCRAM", Behind: 10}
	xml := readDocXML(t, rec, "1/1/2026")
	if !strings.Contains(xml, `<w:t xml:space="preserve">A &amp; B &lt;CO&gt;</w:t>`) {
		t.Fatal("name was not XML-escaped")
	}
	// Court now set -> no placeholder runs remain at all.
	if n := strings.Count(xml, placeholderRun); n != 0 {
		t.Fatalf("remaining placeholder runs = %d, want 0", n)
	}
}

func TestMemosZipLayout(t *testing.T) {
	res := Result{
		AsOf:   time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC),
		Open:   []Rec{{Name: "OPEN, OllY", IDN: "1", Case: "@1", Type: "ALLIED", Behind: 100}},
		Closed: []Rec{{Name: "SHUT, SAM", IDN: "2", Case: "@2", Type: "SCRAM", Behind: 200, Closed: true}},
	}
	z, err := MemosZip(res, "all")
	if err != nil {
		t.Fatalf("MemosZip: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(z), int64(len(z)))
	if err != nil {
		t.Fatalf("zip invalid: %v", err)
	}
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	if !names["Open/OPEN_OllY_1.docx"] || !names["Closed/SHUT_SAM_2.docx"] {
		t.Fatalf("zip layout wrong: %v", names)
	}
}
