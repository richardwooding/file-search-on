package content_test

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/content/testdata"
)

// expectedTypes maps fixture filename -> the content-type Name() the
// registry should resolve to. Entries here drive both the detection
// assertions and the per-format coverage check below.
var expectedTypes = map[string]string{
	"sample.md":   "markdown",
	"sample.html": "html",
	"sample.xml":  "xml",
	"sample.json": "json",
	"sample.csv":  "csv",
	"sample.tsv":  "csv",
	"sample.txt":  "text",
	"sample.svg":  "image/svg+xml",
	"sample.jpg":  "image/jpeg",
	"sample.png":  "image/png",
	"sample.gif":  "image/gif",
	"sample.webp": "image/webp",
	"sample.tiff": "image/tiff",
	"sample.bmp":  "image/bmp",
	"sample.heic": "image/heic",
	"sample.mp3":  "audio/mpeg",
	"sample.m4a":  "audio/mp4",
	"sample.flac": "audio/flac",
	"sample.ogg":  "audio/ogg",
	"sample.mp4":  "video/mp4",
	"sample.mov":  "video/quicktime",
	"sample.mkv":  "video/x-matroska",
	"sample.webm": "video/webm",
	"sample.avi":  "video/x-msvideo",
	"sample.pdf":  "pdf",
	"sample.epub": "epub",
	"sample.docx": "office/docx",
	"sample.xlsx": "office/xlsx",
	"sample.pptx": "office/pptx",
	"sample.odt":  "office/odt",
}

// TestFixturesDetect walks the embedded fixture bank and asserts every
// fixture is detected as the expected content type. This is the single
// most concentrated regression check on the registry's extension and
// magic-byte logic.
func TestFixturesDetect(t *testing.T) {
	fsys := testdata.Fixtures
	registry := content.DefaultRegistry()

	seen := map[string]bool{}
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if strings.EqualFold(path, "README.md") {
			return nil // documentation, not a content fixture
		}
		want, known := expectedTypes[path]
		if !known {
			t.Errorf("fixture %q not listed in expectedTypes — add it or remove the file", path)
			return nil
		}
		seen[path] = true
		ct := registry.Detect(fsys, path)
		if ct == nil {
			t.Errorf("Detect(%q) = nil; want %q", path, want)
			return nil
		}
		if ct.Name() != want {
			t.Errorf("Detect(%q).Name() = %q; want %q", path, ct.Name(), want)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk fixtures: %v", err)
	}
	for path := range expectedTypes {
		if !seen[path] {
			t.Errorf("fixture %q listed in expectedTypes but not present in embed.FS — regenerate?", path)
		}
	}
}

// TestFixturesAttributes drives Attributes() against every fixture and
// asserts the call returns a non-nil map without error. Spot-checks key
// fields per family. This is the smoke test for the new fs.FS plumbing
// — every Attributes implementation has to handle embed.FS input.
func TestFixturesAttributes(t *testing.T) {
	fsys := testdata.Fixtures
	registry := content.DefaultRegistry()
	ctx := t.Context()

	for path, wantType := range expectedTypes {
		t.Run(path, func(t *testing.T) {
			ct := registry.Detect(fsys, path)
			if ct == nil {
				t.Fatalf("Detect returned nil")
			}
			if ct.Name() != wantType {
				t.Fatalf("Detect.Name() = %q; want %q", ct.Name(), wantType)
			}
			attrs, err := ct.Attributes(ctx, fsys, path)
			if err != nil {
				t.Fatalf("Attributes: %v", err)
			}
			if attrs == nil {
				t.Fatalf("Attributes returned nil map")
			}
		})
	}
}

// TestFixturesAttributeSpotChecks verifies a curated set of canonical
// attributes survives end-to-end. Each fixture was generated with known
// metadata; if any of these fail it likely indicates a regression in
// the corresponding parser.
func TestFixturesAttributeSpotChecks(t *testing.T) {
	fsys := testdata.Fixtures
	registry := content.DefaultRegistry()
	ctx := t.Context()

	cases := []struct {
		path  string
		check func(t *testing.T, attrs content.Attributes)
	}{
		{
			path: "sample.md",
			check: func(t *testing.T, a content.Attributes) {
				if a["title"] != "Sample Markdown Fixture" {
					t.Errorf("title = %q; want %q", a["title"], "Sample Markdown Fixture")
				}
				if a["author"] != "file-search-on test fixtures" {
					t.Errorf("author = %q; want generator", a["author"])
				}
				if a["frontmatter_format"] != "yaml" {
					t.Errorf("frontmatter_format = %q; want yaml", a["frontmatter_format"])
				}
				tags, ok := a["tags"].([]string)
				if !ok || len(tags) != 2 {
					t.Errorf("tags = %v; want 2 entries", a["tags"])
				}
			},
		},
		{
			path: "sample.html",
			check: func(t *testing.T, a content.Attributes) {
				if a["title"] != "Sample HTML Fixture" {
					t.Errorf("title = %q", a["title"])
				}
				if a["language"] != "en" {
					t.Errorf("language = %q; want en", a["language"])
				}
			},
		},
		{
			path: "sample.json",
			check: func(t *testing.T, a content.Attributes) {
				if a["kind"] != "object" {
					t.Errorf("kind = %q; want object", a["kind"])
				}
			},
		},
		{
			path: "sample.csv",
			check: func(t *testing.T, a content.Attributes) {
				if a["column_count"] != int64(4) {
					t.Errorf("column_count = %v; want 4", a["column_count"])
				}
			},
		},
		{
			path: "sample.xml",
			check: func(t *testing.T, a content.Attributes) {
				if a["root_element"] != "library" {
					t.Errorf("root_element = %q; want library", a["root_element"])
				}
			},
		},
		{
			path: "sample.png",
			check: func(t *testing.T, a content.Attributes) {
				if w, _ := a["width"].(int64); w != 16 {
					t.Errorf("width = %v; want 16", a["width"])
				}
				if h, _ := a["height"].(int64); h != 16 {
					t.Errorf("height = %v; want 16", a["height"])
				}
			},
		},
		{
			path: "sample.mp3",
			check: func(t *testing.T, a content.Attributes) {
				if a["title"] != "Sample MP3 Fixture" {
					t.Errorf("title = %q", a["title"])
				}
				if a["artist"] != "file-search-on" {
					t.Errorf("artist = %q", a["artist"])
				}
				if d, _ := a["duration"].(float64); d <= 0 {
					t.Errorf("duration = %v; want > 0", a["duration"])
				}
			},
		},
		{
			path: "sample.flac",
			check: func(t *testing.T, a content.Attributes) {
				if d, _ := a["duration"].(float64); d <= 0 {
					t.Errorf("duration = %v; want > 0", a["duration"])
				}
				if sr, _ := a["sample_rate"].(int64); sr != 44100 {
					t.Errorf("sample_rate = %v; want 44100", a["sample_rate"])
				}
			},
		},
		{
			path: "sample.mp4",
			check: func(t *testing.T, a content.Attributes) {
				if w, _ := a["video_width"].(int64); w != 64 {
					t.Errorf("video_width = %v; want 64", a["video_width"])
				}
				if h, _ := a["video_height"].(int64); h != 48 {
					t.Errorf("video_height = %v; want 48", a["video_height"])
				}
			},
		},
		{
			path: "sample.docx",
			check: func(t *testing.T, a content.Attributes) {
				if a["title"] != "Sample Office Fixture" {
					t.Errorf("title = %q", a["title"])
				}
				if a["language"] != "en" {
					t.Errorf("language = %q; want en", a["language"])
				}
			},
		},
		{
			path: "sample.epub",
			check: func(t *testing.T, a content.Attributes) {
				if a["title"] != "Sample Office Fixture" {
					t.Errorf("title = %q", a["title"])
				}
			},
		},
		{
			path: "sample.pdf",
			check: func(t *testing.T, a content.Attributes) {
				if a["title"] != "Sample PDF Fixture" {
					t.Errorf("title = %q", a["title"])
				}
				if pc, _ := a["page_count"].(int64); pc < 1 {
					t.Errorf("page_count = %v; want >= 1", a["page_count"])
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			ct := registry.Detect(fsys, c.path)
			if ct == nil {
				t.Fatalf("Detect returned nil for %q", c.path)
			}
			attrs, err := ct.Attributes(ctx, fsys, c.path)
			if err != nil {
				t.Fatalf("Attributes: %v", err)
			}
			c.check(t, attrs)
		})
	}
}

