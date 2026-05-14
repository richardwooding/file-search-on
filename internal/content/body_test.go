package content_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
