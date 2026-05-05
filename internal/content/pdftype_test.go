package content_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

)

// pdfFixture builds a minimal valid PDF byte slice with the given metadata.
// extraCatalog is appended to the catalog dict (without leading space).
// extraObjects is appended verbatim before the xref table; offsetsExtra holds
// their byte offsets so the xref can index them.
type pdfFixture struct {
	title        string
	author       string
	catalogLang  string // empty → no /Lang
	xmpLanguage  string // empty → no /Metadata stream; non-empty adds an XMP stream object
}

func (fx pdfFixture) build() []byte {
	var buf bytes.Buffer
	var offsets []int

	// Header (with binary marker so libraries treat the file as binary).
	buf.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")

	// Object 1: catalog. /Lang and /Metadata are optional.
	offsets = append(offsets, buf.Len())
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R")
	if fx.catalogLang != "" {
		fmt.Fprintf(&buf, " /Lang (%s)", fx.catalogLang)
	}
	if fx.xmpLanguage != "" {
		buf.WriteString(" /Metadata 5 0 R")
	}
	buf.WriteString(" >>\nendobj\n")

	// Object 2: pages.
	offsets = append(offsets, buf.Len())
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")

	// Object 3: a single empty page.
	offsets = append(offsets, buf.Len())
	buf.WriteString("3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>\nendobj\n")

	// Object 4: /Info dict with title + author.
	offsets = append(offsets, buf.Len())
	fmt.Fprintf(&buf, "4 0 obj\n<< /Title (%s) /Author (%s) >>\nendobj\n", fx.title, fx.author)

	// Optional object 5: XMP metadata stream.
	if fx.xmpLanguage != "" {
		xmp := fmt.Sprintf(`<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/" x:xmptk="test">
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<rdf:Description xmlns:dc="http://purl.org/dc/elements/1.1/">
<dc:language><rdf:Bag><rdf:li>%s</rdf:li></rdf:Bag></dc:language>
</rdf:Description>
</rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`, fx.xmpLanguage)
		offsets = append(offsets, buf.Len())
		fmt.Fprintf(&buf, "5 0 obj\n<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream\nendobj\n", len(xmp), xmp)
	}

	// xref table.
	xrefOffset := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(offsets)+1)
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}

	// Trailer + startxref + EOF.
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R /Info 4 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		len(offsets)+1, xrefOffset)

	return buf.Bytes()
}

func writePDF(t *testing.T, dir, name string, fx pdfFixture) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, fx.build(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPDFCatalogLang(t *testing.T) {
	dir := t.TempDir()
	path := writePDF(t, dir, "doc.pdf", pdfFixture{
		title:       "Hello PDF",
		author:      "Jane Doe",
		catalogLang: "en-US",
	})

	ct := detectAt(path)
	if ct == nil || ct.Name() != "pdf" {
		t.Fatalf("Detect: got %v, want pdf", ct)
	}
	attrs, err := attributesAt(t.Context(), ct, path)
	if err != nil {
		t.Fatalf("Attributes: %v", err)
	}
	if got := attrs["title"]; got != "Hello PDF" {
		t.Errorf("title = %q, want Hello PDF", got)
	}
	if got := attrs["author"]; got != "Jane Doe" {
		t.Errorf("author = %q, want Jane Doe", got)
	}
	if got := attrs["language"]; got != "en-US" {
		t.Errorf("language = %q, want en-US", got)
	}
	if got := attrs["page_count"]; got != int64(1) {
		t.Errorf("page_count = %v, want 1", got)
	}
}

func TestPDFXMPLanguageFallback(t *testing.T) {
	dir := t.TempDir()
	// No catalog /Lang; XMP <dc:language> says fr.
	path := writePDF(t, dir, "doc.pdf", pdfFixture{
		title:       "Bonjour",
		author:      "Jules",
		xmpLanguage: "fr",
	})

	ct := detectAt(path)
	attrs, err := attributesAt(t.Context(), ct, path)
	if err != nil {
		t.Fatalf("Attributes: %v", err)
	}
	if got := attrs["language"]; got != "fr" {
		t.Errorf("language = %q, want fr (from XMP fallback)", got)
	}
}

func TestPDFCatalogLangBeatsXMP(t *testing.T) {
	dir := t.TempDir()
	// Catalog says en, XMP says de — catalog wins.
	path := writePDF(t, dir, "doc.pdf", pdfFixture{
		title:       "x",
		author:      "y",
		catalogLang: "en",
		xmpLanguage: "de",
	})

	ct := detectAt(path)
	attrs, err := attributesAt(t.Context(), ct, path)
	if err != nil {
		t.Fatalf("Attributes: %v", err)
	}
	if got := attrs["language"]; got != "en" {
		t.Errorf("language = %q, want en (catalog should win over XMP)", got)
	}
}

func TestPDFNoLanguage(t *testing.T) {
	dir := t.TempDir()
	path := writePDF(t, dir, "doc.pdf", pdfFixture{title: "x", author: "y"})

	ct := detectAt(path)
	attrs, err := attributesAt(t.Context(), ct, path)
	if err != nil {
		t.Fatalf("Attributes: %v", err)
	}
	if v, ok := attrs["language"]; ok && v != "" {
		t.Errorf("language present when neither catalog nor XMP set it: %v", v)
	}
}

func TestPDFRespectsCancellation(t *testing.T) {
	dir := t.TempDir()
	path := writePDF(t, dir, "doc.pdf", pdfFixture{title: "x", author: "y", catalogLang: "en"})

	ct := detectAt(path)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := attributesAt(ctx, ct, path)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Attributes(cancelled ctx): err = %v, want context.Canceled", err)
	}
}
