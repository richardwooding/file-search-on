package search_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// TestWalk_GracefulDegradation_EveryType is the property test for issue
// #337: a parse failure must DEGRADE (the file still matches `true` with
// whatever attributes survived), never silently DROP the file. It
// generalises the #321 regression (TestWalk_MalformedFilesNotDropped,
// pdf/docx only) to malformed + empty inputs across every parsed family.
//
// Invariant: every regular file written to the corpus — regardless of
// how badly its content-type parser chokes — appears in `search 'true'`.
// A new content type that drops malformed input on the floor will fail
// here, turning "discover it in a dogfood" into "the suite fails".
func TestWalk_GracefulDegradation_EveryType(t *testing.T) {
	dir := t.TempDir()

	// One malformed (well-known extension, garbage/incomplete body) and
	// one empty file per parsed family. The extension drives detection so
	// each family's Attributes() runs and hits its error path; the empty
	// variant exercises the zero-byte edge that several hand-rolled
	// binary header walkers special-case.
	malformed := map[string]string{
		// structured text
		"bad.md":   "\x00\x01 not really\n# but has a heading",
		"bad.json": `{"unterminated": `,
		"bad.xml":  "<root><unclosed>",
		"bad.yaml": "key: [unbalanced\n  - : :",
		"bad.toml": "key = = =\n[unclosed",
		"bad.csv":  "a,b,c\n\"unterminated,quote",
		"bad.html": "<html><body><p>no close",
		// documents
		"bad.pdf":  "%PDF-1.4 then garbage, no xref",
		"bad.docx": "PK\x03\x04 not a real zip body",
		"bad.xlsx": "not a zip",
		"bad.pptx": "not a zip",
		"bad.odt":  "not a zip",
		"bad.epub": "PK\x03\x04 truncated epub",
		// media (magic-ish prefixes then garbage)
		"bad.mp3":  "ID3\x04\x00\x00 garbage frames",
		"bad.flac": "fLaC\x00 garbage",
		"bad.wav":  "RIFF\x00\x00\x00\x00WAVEfmt garbage",
		"bad.ogg":  "OggS\x00 garbage",
		"bad.mp4":  "\x00\x00\x00\x18ftyp garbage box",
		"bad.mkv":  "\x1a\x45\xdf\xa3 garbage ebml",
		"bad.avi":  "RIFF\x00\x00\x00\x00AVI  garbage",
		"bad.webm": "\x1a\x45\xdf\xa3 garbage",
		// images
		"bad.png":  "\x89PNG\r\n\x1a\n garbage chunks",
		"bad.jpg":  "\xff\xd8\xff garbage",
		"bad.gif":  "GIF89a garbage",
		"bad.tiff": "II*\x00 garbage",
		"bad.webp": "RIFF\x00\x00\x00\x00WEBP garbage",
		// archives
		"bad.zip":    "PK\x03\x04 truncated",
		"bad.tar":    "not a tar header block",
		"bad.tar.gz": "\x1f\x8b\x08 garbage deflate",
		"bad.gz":     "\x1f\x8b\x08 garbage",
		// binaries
		"bad_elf":   "\x7fELF garbage",
		"bad_macho": "\xcf\xfa\xed\xfe garbage",
		"bad.exe":   "MZ garbage pe",
		// bytecode
		"bad.class": "\xca\xfe\xba\xbe garbage",
		"bad.pyc":   "\x00\x00\x00\x00 garbage pyc",
		"bad.wasm":  "\x00asm garbage",
		// mail
		"bad.eml":  "From: broken\nno blank line then body",
		"bad.mbox": "From bad\nnot a real mbox",
		// data / db / notebooks / fonts / science
		"bad.sqlite": "SQLite format 3\x00 garbage",
		"bad.ipynb":  `{"cells": [`,
		"bad.ttf":    "\x00\x01\x00\x00 garbage sfnt",
		"bad.otf":    "OTTO garbage",
		"bad.fits":   "SIMPLE  = garbage",
		// source
		"bad.go": "package ??? func (",
		"bad.py": "def (:",
		// no extension at all — detection falls through, still must appear
		"noext_garbage": "\x00\x01\x02\x03 random bytes",
	}

	for name, body := range malformed {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		// Empty companion for each (zero-byte edge).
		empty := "empty_" + name
		if err := os.WriteFile(filepath.Join(dir, empty), nil, 0o644); err != nil {
			t.Fatalf("write %s: %v", empty, err)
		}
	}

	results, err := search.Walk(t.Context(), search.Options{
		Roots: []string{dir},
		Expr:  "true",
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	got := make(map[string]bool, len(results))
	for _, r := range results {
		got[filepath.Base(r.Path)] = true
	}

	wantCount := len(malformed) * 2 // malformed + empty companion each
	if len(got) != wantCount {
		t.Errorf("got %d distinct files, want %d", len(got), wantCount)
	}
	for name := range malformed {
		if !got[name] {
			t.Errorf("%s dropped — parse failure must degrade, not drop (#337)", name)
		}
		if empty := "empty_" + name; !got[empty] {
			t.Errorf("%s dropped — empty file must still appear (#337)", empty)
		}
	}
}
