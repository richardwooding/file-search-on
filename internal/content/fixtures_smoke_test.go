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
	"sample.zip":    "archive/zip",
	"sample.jar":    "archive/zip",
	"sample.tar":    "archive/tar",
	"sample.tar.gz": "archive/tar+gzip",
	"sample.gz":     "archive/gzip",
	"sample.elf":    "binary/elf",
	"sample.macho":  "binary/mach-o",
	"sample.exe":    "binary/pe",
	"sample.eml":    "email/rfc822",
	"sample.mbox":   "email/mbox",
	"sample.go":    "source/go",
	"sample.py":    "source/python",
	"sample.rs":    "source/rust",

	"sample.dmg":   "disk-image/dmg",
	"sample.iso":   "disk-image/iso9660",
	"sample.vhd":   "disk-image/vhd",
	"sample.vhdx":  "disk-image/vhdx",
	"sample.vmdk":  "disk-image/vmdk",
	"sample.qcow2": "disk-image/qcow2",
	"sample.wim":   "disk-image/wim",

	"sample.pkg":      "install/pkg",
	"sample.deb":      "install/deb",
	"sample.rpm":      "install/rpm",
	"sample.appimage": "install/appimage",

	"sample.class": "bytecode/jvm",
	"sample.pyc":   "bytecode/python",
	"sample.wasm":  "bytecode/wasm",
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

// binarySpotCheck builds a check fn that asserts a compiled-binary
// fixture has the expected architecture, bitness, format, and binary
// type. The fixtures are stripped, dynamically-linked Go executables
// for x86_64 — so isStripped should be true and isDynamic true on
// every platform.
func binarySpotCheck(wantArch, wantFormat, wantType string, wantBitness int64) func(t *testing.T, a content.Attributes) {
	return func(t *testing.T, a content.Attributes) {
		archs, _ := a["architectures"].([]string)
		if len(archs) != 1 || archs[0] != wantArch {
			t.Errorf("architectures = %v; want [%q]", archs, wantArch)
		}
		if b, _ := a["bitness"].(int64); b != wantBitness {
			t.Errorf("bitness = %v; want %d", a["bitness"], wantBitness)
		}
		if f, _ := a["binary_format"].(string); f != wantFormat {
			t.Errorf("binary_format = %q; want %q", a["binary_format"], wantFormat)
		}
		if bt, _ := a["binary_type"].(string); bt != wantType {
			t.Errorf("binary_type = %q; want %q", a["binary_type"], wantType)
		}
	}
}

// archiveSpotCheck builds a check fn that asserts a regular archive
// fixture has the expected entry_count, has_root_dir, and a single
// expected top-level entry name. Reused across the zip / jar / tar /
// tar.gz fixtures since they all hold the same source tree.
func archiveSpotCheck(wantEntries int64, wantHasRoot bool, wantTopEntry string) func(t *testing.T, a content.Attributes) {
	return func(t *testing.T, a content.Attributes) {
		if ec, _ := a["entry_count"].(int64); ec != wantEntries {
			t.Errorf("entry_count = %v; want %d", a["entry_count"], wantEntries)
		}
		if rd, _ := a["has_root_dir"].(bool); rd != wantHasRoot {
			t.Errorf("has_root_dir = %v; want %v", a["has_root_dir"], wantHasRoot)
		}
		tops, _ := a["top_level_entries"].([]string)
		if len(tops) != 1 || tops[0] != wantTopEntry {
			t.Errorf("top_level_entries = %v; want [%q]", tops, wantTopEntry)
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
				if a["json_kind"] != "object" {
					t.Errorf("json_kind = %q; want object", a["json_kind"])
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
				if w, _ := a["img_width"].(int64); w != 16 {
					t.Errorf("img_width = %v; want 16", a["img_width"])
				}
				if h, _ := a["img_height"].(int64); h != 16 {
					t.Errorf("img_height = %v; want 16", a["img_height"])
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
				// MP3 nominal bitrate is the first-frame bitrate index.
				// Fixture is encoded at -b:a 64k so the first frame's
				// table index resolves to 64 kbps.
				if nb, _ := a["nominal_bitrate"].(int64); nb != 64 {
					t.Errorf("nominal_bitrate = %v; want 64", a["nominal_bitrate"])
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
				if bd, _ := a["bit_depth"].(int64); bd != 16 {
					t.Errorf("bit_depth = %v; want 16", a["bit_depth"])
				}
			},
		},
		{
			path: "sample.m4a",
			check: func(t *testing.T, a content.Attributes) {
				if bd, _ := a["bit_depth"].(int64); bd != 16 {
					t.Errorf("bit_depth = %v; want 16 (AAC nominal)", a["bit_depth"])
				}
				// nominal_bitrate from esds is non-zero. The fixture is
				// 1 second of silence encoded at -b:a 64k; ffmpeg writes
				// maxBitrate=64000 (the encoder target) and a much lower
				// avgBitrate (silence compresses to ~1.5 kbps). Our
				// avg-first precedence picks avgBitrate, so the value
				// is small but non-zero — that's the parser working.
				if nb, _ := a["nominal_bitrate"].(int64); nb <= 0 {
					t.Errorf("nominal_bitrate = %v; want > 0 (esds parse)", a["nominal_bitrate"])
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
				// Fixture has a 44.1 kHz mono AAC audio track — the
				// MP4 video parser populates the standard sample_rate
				// and channels attributes from the audio sample entry.
				if sr, _ := a["sample_rate"].(int64); sr != 44100 {
					t.Errorf("sample_rate = %v; want 44100", a["sample_rate"])
				}
				if ch, _ := a["channels"].(int64); ch != 1 {
					t.Errorf("channels = %v; want 1", a["channels"])
				}
				if a["audio_codec"] != "aac" {
					t.Errorf("audio_codec = %q; want aac", a["audio_codec"])
				}
			},
		},
		{
			path: "sample.avi",
			check: func(t *testing.T, a content.Attributes) {
				// AVI fixture has 44.1 kHz mono audio in WAVEFORMATEX.
				if sr, _ := a["sample_rate"].(int64); sr != 44100 {
					t.Errorf("sample_rate = %v; want 44100", a["sample_rate"])
				}
				if ch, _ := a["channels"].(int64); ch != 1 {
					t.Errorf("channels = %v; want 1", a["channels"])
				}
			},
		},
		{
			// All three regular-archive fixtures (zip / tar / tar.gz)
			// were generated from the same source tree: a single
			// top-level dir `sample/` containing two files plus a
			// nested subdir with one file = 5 entries (4 files + 1 dir
			// header for `sample/subdir/`). Numbers should match
			// across all three.
			path: "sample.zip",
			check: archiveSpotCheck(5, true, "sample"),
		},
		{path: "sample.jar", check: archiveSpotCheck(5, true, "sample")},
		{path: "sample.tar", check: archiveSpotCheck(5, true, "sample")},
		{path: "sample.tar.gz", check: archiveSpotCheck(5, true, "sample")},
		{
			// Standalone gzip: entry_count = 1 (gzip carries one stream),
			// no top_level_entries (not a directory archive),
			// has_root_dir = false.
			path: "sample.gz",
			check: func(t *testing.T, a content.Attributes) {
				if ec, _ := a["entry_count"].(int64); ec != 1 {
					t.Errorf("entry_count = %v; want 1", a["entry_count"])
				}
				if rd, _ := a["has_root_dir"].(bool); rd {
					t.Errorf("has_root_dir = true; want false (standalone gzip has no directory)")
				}
			},
		},
		{
			// Regression cover for #51: ffmpeg-emitted MKV files have a
			// populated EBML header (EBMLVersion, DocType, etc.). The
			// parser used to read only the header's id + size and then
			// land on the header's first child instead of the Segment,
			// causing every subsequent attribute read to fail silently.
			path: "sample.mkv",
			check: func(t *testing.T, a content.Attributes) {
				if w, _ := a["video_width"].(int64); w != 64 {
					t.Errorf("video_width = %v; want 64", a["video_width"])
				}
				if h, _ := a["video_height"].(int64); h != 48 {
					t.Errorf("video_height = %v; want 48", a["video_height"])
				}
				if a["video_codec"] != "h264" {
					t.Errorf("video_codec = %q; want h264", a["video_codec"])
				}
				if d, _ := a["duration"].(float64); d <= 0 {
					t.Errorf("duration = %v; want > 0", a["duration"])
				}
				if a["audio_codec"] != "vorbis" {
					t.Errorf("audio_codec = %q; want vorbis", a["audio_codec"])
				}
				if sr, _ := a["sample_rate"].(int64); sr != 44100 {
					t.Errorf("sample_rate = %v; want 44100", a["sample_rate"])
				}
			},
		},
		{
			path: "sample.webm",
			check: func(t *testing.T, a content.Attributes) {
				if w, _ := a["video_width"].(int64); w != 64 {
					t.Errorf("video_width = %v; want 64", a["video_width"])
				}
				if a["video_codec"] != "vp9" {
					t.Errorf("video_codec = %q; want vp9", a["video_codec"])
				}
				if a["audio_codec"] != "opus" {
					t.Errorf("audio_codec = %q; want opus", a["audio_codec"])
				}
				if sr, _ := a["sample_rate"].(int64); sr != 48000 {
					t.Errorf("sample_rate = %v; want 48000", a["sample_rate"])
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
		{
			// Three Go-compiled binaries (linux/darwin/windows × amd64),
			// stripped (-ldflags='-s -w'), dynamically linked. All carry
			// the standard runtime so binary_type is "executable" and
			// is_dynamically_linked / is_stripped are both true.
			path:  "sample.elf",
			check: binarySpotCheck("x86_64", "elf", "executable", 64),
		},
		{
			path:  "sample.macho",
			check: binarySpotCheck("x86_64", "mach-o", "executable", 64),
		},
		{
			path:  "sample.exe",
			check: binarySpotCheck("x86_64", "pe", "executable", 64),
		},
		{
			// Stripping detection: the three Go fixtures are all built
			// with -ldflags='-s -w', so each parser must report
			// is_stripped = true.
			path: "sample.elf",
			check: func(t *testing.T, a content.Attributes) {
				if s, _ := a["is_stripped"].(bool); !s {
					t.Errorf("is_stripped = false; want true (built with -ldflags='-s -w')")
				}
			},
		},
		{
			// Multipart .eml fixture with one attachment. Subject reuses
			// `title`, From reuses `author` (display name preferred when
			// present).
			path: "sample.eml",
			check: func(t *testing.T, a content.Attributes) {
				if a["title"] != "Sample Email Fixture" {
					t.Errorf("title = %q; want Sample Email Fixture", a["title"])
				}
				if a["author"] != "Alice Tester" {
					t.Errorf("author = %q; want Alice Tester", a["author"])
				}
				to, _ := a["email_to"].([]string)
				if len(to) != 2 {
					t.Errorf("email_to = %v; want 2 entries", to)
				}
				if ac, _ := a["attachment_count"].(int64); ac != 1 {
					t.Errorf("attachment_count = %v; want 1", a["attachment_count"])
				}
				if a["email_message_id"] != "fixture-001@example.com" {
					t.Errorf("email_message_id = %q; want angles stripped", a["email_message_id"])
				}
			},
		},
		{
			// 3-message mbox fixture. First message's attributes leak
			// through; email_count carries the archive shape.
			path: "sample.mbox",
			check: func(t *testing.T, a content.Attributes) {
				if c, _ := a["email_count"].(int64); c != 3 {
					t.Errorf("email_count = %v; want 3", a["email_count"])
				}
				if a["title"] != "Sample MBOX First Message" {
					t.Errorf("title = %q; want first-message subject", a["title"])
				}
				if a["author"] != "Alice Tester" {
					t.Errorf("author = %q; want Alice Tester (first-message From)", a["author"])
				}
			},
		},
		{
			// sample.go: 17 lines — 3 line-comment header + 1 package +
			// 1 blank + 5-line block comment + 1 blank + 1 import +
			// 1 blank + 1 line comment + 3 lines of fn body.
			path: "sample.go",
			check: func(t *testing.T, a content.Attributes) {
				if a["language"] != "go" {
					t.Errorf("language = %q; want go", a["language"])
				}
				if v, _ := a["line_count"].(int64); v != 17 {
					t.Errorf("line_count = %v; want 17", a["line_count"])
				}
				if v, _ := a["loc"].(int64); v != 5 {
					t.Errorf("loc = %v; want 5", a["loc"])
				}
				if v, _ := a["comment_loc"].(int64); v != 9 {
					t.Errorf("comment_loc = %v; want 9", a["comment_loc"])
				}
				if v, _ := a["blank_loc"].(int64); v != 3 {
					t.Errorf("blank_loc = %v; want 3", a["blank_loc"])
				}
			},
		},
		{
			path: "sample.py",
			check: func(t *testing.T, a content.Attributes) {
				if a["language"] != "python" {
					t.Errorf("language = %q; want python", a["language"])
				}
				if v, _ := a["loc"].(int64); v != 4 {
					t.Errorf("loc = %v; want 4", a["loc"])
				}
				if v, _ := a["comment_loc"].(int64); v != 4 {
					t.Errorf("comment_loc = %v; want 4", a["comment_loc"])
				}
			},
		},
		{
			path: "sample.rs",
			check: func(t *testing.T, a content.Attributes) {
				if a["language"] != "rust" {
					t.Errorf("language = %q; want rust", a["language"])
				}
				if v, _ := a["loc"].(int64); v != 6 {
					t.Errorf("loc = %v; want 6", a["loc"])
				}
				if v, _ := a["comment_loc"].(int64); v != 4 {
					t.Errorf("comment_loc = %v; want 4 (// + ///)", a["comment_loc"])
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

