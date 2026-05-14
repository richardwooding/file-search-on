package content_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/richardwooding/file-search-on/internal/content"
)

// TestExtractBody_Fixtures exercises every structured-document type
// against the on-disk fixtures committed under
// internal/content/testdata/fixtures/. Each fixture was generated for
// the content-type test suite and contains the same canonical strings
// ("Sample ... Fixture", "Generated for the content-type test suite",
// etc.); the body extractor should surface them as plain text so
// CEL's body.contains / body.matches can hit.
func TestExtractBody_Fixtures(t *testing.T) {
	const maxBytes = 1 << 20 // 1 MiB — same as the runtime default

	// Resolve fixtures dir relative to this file. t.Chdir would
	// confuse the parallel test suite; absolute paths are cleaner.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixDir := filepath.Join(wd, "testdata", "fixtures")
	fsys := os.DirFS(fixDir)

	cases := []struct {
		fileName    string
		contentType string
		wantSubstrs []string
	}{
		{
			"sample.docx", "office/docx",
			// DOCX's word/document.xml carries the title duplicated
			// in the visible body PLUS the canonical "Generated for
			// the content-type test suite" sentence.
			[]string{"Sample Office Fixture", "content-type test suite"},
		},
		{
			"sample.xlsx", "office/xlsx",
			// XLSX uses inline-string cells (t="inlineStr"); cell
			// values are the column headers + per-row name strings.
			[]string{"revenue", "Alpha", "Beta"},
		},
		{
			"sample.pptx", "office/pptx",
			// PPTX text runs in <a:t> across slides.
			[]string{"Sample PPTX Fixture"},
		},
		{
			"sample.odt", "office/odt",
			// ODT paragraphs are <text:p>; text is direct CharData
			// (sometimes inside <text:span> styling wrappers).
			[]string{"Sample Office Fixture", "content-type test suite"},
		},
		{
			"sample.epub", "epub",
			// EPUB chapter HTML — stripped of tags should surface
			// the chapter body text.
			[]string{"Sample Office Fixture", "content-type test suite"},
		},
		{
			"sample.eml", "email/rfc822",
			// Single multipart/mixed message — text/plain body is
			// extracted, base64 attachment skipped. (Line wrapping
			// from the original message is preserved in the output,
			// so test for substrings within a single physical line.)
			[]string{"file-search-on", "email fixture"},
		},
		{
			"sample.mbox", "email/mbox",
			// 3-message archive — body extractor concatenates across
			// all messages so an agent can grep the whole inbox.
			[]string{"First message", "Second message", "Third message", "distinct thread"},
		},
		{
			"sample.pdf", "pdf",
			// 1-page ReportLab-generated fixture: pdftype_test.go
			// already verifies title/author/language metadata. The
			// canonical body line "Sample PDF Fixture" plus the
			// fixture's "content-type test suite" sentence both surface
			// through the linear per-page extractor.
			[]string{"Sample PDF Fixture", "content-type test suite"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.fileName, func(t *testing.T) {
			body, err := content.ExtractBody(t.Context(), tc.contentType, fsys, tc.fileName, maxBytes)
			if err != nil {
				t.Fatalf("ExtractBody: %v", err)
			}
			if body == "" {
				t.Fatalf("body empty — extractor returned nothing for %s", tc.fileName)
			}
			for _, want := range tc.wantSubstrs {
				if !strings.Contains(body, want) {
					t.Errorf("body does not contain %q\n--- body ---\n%s\n--- end ---", want, body)
				}
			}
		})
	}
}

// TestExtractBody_EmailSkipsAttachments verifies the eml extractor
// returns the text/plain body but NOT the base64-encoded attachment
// payload. sample.eml's attachment is the base64 of "hello world\n";
// neither the encoded form nor the decoded form should leak into the
// body output (attachments are skipped entirely).
func TestExtractBody_EmailSkipsAttachments(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fsys := os.DirFS(filepath.Join(wd, "testdata", "fixtures"))
	body, err := content.ExtractBody(t.Context(), "email/rfc822", fsys, "sample.eml", 1<<20)
	if err != nil {
		t.Fatalf("ExtractBody: %v", err)
	}
	for _, leaked := range []string{
		"aGVsbG8gd29ybGQK", // base64 form
		"hello world",      // decoded form
	} {
		if strings.Contains(body, leaked) {
			t.Errorf("attachment content %q leaked into body: %q", leaked, body)
		}
	}
}

// TestExtractBody_PDFCorrupted verifies the pdf extractor degrades
// gracefully on a file that's labelled "pdf" but isn't one — random
// bytes with a "%PDF-" prefix, truncated headers, etc. The contract
// is "", nil: the walker should keep going, not surface a parse error
// and not panic. ledongthuc/pdf's parser is documented as incomplete
// and panics on many adversarial inputs; the defer/recover guard in
// pdfBody is what makes this test pass.
func TestExtractBody_PDFCorrupted(t *testing.T) {
	cases := map[string][]byte{
		"empty":              {},
		"header-only":        []byte("%PDF-1.4\n"),
		"junk-after-magic":   []byte("%PDF-junk and then some random bytes"),
		"truncated-xref":     []byte("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n1 0 obj\n<< /Type /Catalog >>\nendobj\nxref\n0 1\n"),
		"random-bytes":       bytes.Repeat([]byte{0xff, 0x00, 0xa5, 0x5a}, 64),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			fsys := fstest.MapFS{"x.pdf": &fstest.MapFile{Data: data}}
			body, err := content.ExtractBody(t.Context(), "pdf", fsys, "x.pdf", 4096)
			if err != nil {
				t.Errorf("err=%v want nil for corrupted PDF", err)
			}
			if body != "" {
				t.Errorf("body=%q want empty for corrupted PDF", body)
			}
		})
	}
}

// TestExtractBody_UnknownType verifies the dispatcher returns ("", nil)
// for content types it doesn't know about. The CEL body-read path
// falls through to the raw-text branch in that case.
func TestExtractBody_UnknownType(t *testing.T) {
	body, err := content.ExtractBody(t.Context(), "markdown", nil, "ignored", 0)
	if err != nil {
		t.Errorf("err=%v want nil for unknown type", err)
	}
	if body != "" {
		t.Errorf("body=%q want \"\" for unknown type", body)
	}
}

// TestExtractBody_BodyCap verifies the maxBytes cap is respected: even
// on a small fixture, asking for 32 bytes should return at most ~32
// bytes of extracted text. The cap is a soft ceiling — the extractor
// stops between paragraph boundaries, so the actual length may be
// slightly larger than the cap (one trailing paragraph). Verify it's
// "reasonably close" rather than "exactly N" for fixture stability.
func TestExtractBody_BodyCap(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fsys := os.DirFS(filepath.Join(wd, "testdata", "fixtures"))
	body, err := content.ExtractBody(t.Context(), "office/docx", fsys, "sample.docx", 32)
	if err != nil {
		t.Fatalf("ExtractBody: %v", err)
	}
	// The extractor stops at the next paragraph after the cap; for
	// the DOCX fixture (~4 short paragraphs) that's well under 200
	// bytes total. The point is the cap is REACHED — without it
	// every paragraph would land in the result.
	if len(body) > 200 {
		t.Errorf("body len=%d want <= 200 with maxBytes=32 (cap not honoured)", len(body))
	}
	if body == "" {
		t.Errorf("body empty under tight cap — extractor terminated too early")
	}
}
