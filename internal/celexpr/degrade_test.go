package celexpr_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/richardwooding/file-search-on/internal/celexpr"
	"github.com/richardwooding/file-search-on/internal/content"
)

// TestBuildAttributesWith_MalformedFileDegrades is the regression for
// issue #321: a file that detects as a parseable type (by extension)
// but whose ContentType.Attributes() errors (truncated PDF, non-zip
// docx, garbage with a valid extension) must NOT propagate the error —
// that drops the file from the walk entirely and hard-errors `attrs`.
// It should degrade to a basic record: detected content_type + stat,
// no type-specific attributes.
func TestBuildAttributesWith_MalformedFileDegrades(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()

	cases := []struct {
		name, body, wantCT string
	}{
		{"broken.pdf", "this is not a pdf at all\n", "pdf"},                 // pdfBody/pagecount errors
		{"empty.pdf", "", "pdf"},                                           // "invalid header"
		{"notreally.docx", "this is plainly not a zip archive\n", "office/docx"}, // zip open errors
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(dir, tc.name)
			mustWrite(t, p, tc.body)
			abs, _ := filepath.Abs(p)
			a, err := celexpr.BuildAttributesWith(ctx, os.DirFS(dir), tc.name, abs, content.DefaultRegistry(), celexpr.BuildOptions{})
			if err != nil {
				t.Fatalf("malformed %s must not error (#321): %v", tc.name, err)
			}
			if a == nil {
				t.Fatalf("%s: nil attrs", tc.name)
			}
			if a.ContentType != tc.wantCT {
				t.Errorf("%s: content_type = %q, want %q (detected by extension; parse failed but file still surfaced)", tc.name, a.ContentType, tc.wantCT)
			}
			if a.Path != abs {
				t.Errorf("%s: Path = %q, want %q", tc.name, a.Path, abs)
			}
		})
	}
}
