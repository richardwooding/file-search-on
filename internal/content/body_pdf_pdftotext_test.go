package content

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestPDFBody_PdftotextShellout exercises the #333 shell-out path with a
// stub `pdftotext` on PATH (so the test doesn't need real poppler): when
// the binary is present, pdfBody copies the PDF bytes to a temp file,
// runs it, and returns its stdout.
func TestPDFBody_PdftotextShellout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell stub is POSIX")
	}
	bin := t.TempDir()
	stub := filepath.Join(bin, "pdftotext")
	// Ignores its args; emits known text to stdout. Proves wiring + capture.
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nprintf 'word one two three from pdftotext\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	dir := t.TempDir()
	// pdfBody opens the file before shelling out; content is ignored by the stub.
	if err := os.WriteFile(filepath.Join(dir, "x.pdf"), []byte("%PDF-1.4\n%%EOF\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	body, err := pdfBody(context.Background(), os.DirFS(dir), "x.pdf", 0)
	if err != nil {
		t.Fatalf("pdfBody: %v", err)
	}
	if !strings.Contains(body, "word one two three from pdftotext") {
		t.Errorf("expected pdftotext stub output, got %q", body)
	}
}

// TestPDFBody_FallsBackWhenPdftotextFails verifies that a failing/exiting
// pdftotext (non-zero exit) does NOT break extraction — pdfBody falls
// through to the pure-Go path.
func TestPDFBody_FallsBackWhenPdftotextFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell stub is POSIX")
	}
	bin := t.TempDir()
	stub := filepath.Join(bin, "pdftotext")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	// Use the committed ReportLab fixture; the pure-Go fallback extracts it.
	fsys := os.DirFS(filepath.Join("testdata", "fixtures"))
	body, err := pdfBody(context.Background(), fsys, "sample.pdf", 1<<20)
	if err != nil {
		t.Fatalf("pdfBody: %v", err)
	}
	if !strings.Contains(body, "content-type test suite") {
		t.Errorf("fallback path should still extract the fixture body, got %q", body[:min(120, len(body))])
	}
}
