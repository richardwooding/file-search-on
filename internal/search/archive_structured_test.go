package search_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// fixtureBytes reads one of the canonical content-type fixtures from
// the testdata tree. Tests for archive-internal structured-document
// body extraction need real PDF / DOCX / EPUB / EML bytes; the
// existing fixtures provide them.
func fixtureBytes(t *testing.T, name string) []byte {
	t.Helper()
	// archive_walk_test.go runs in package search_test, so the test
	// binary's CWD is internal/search. Step up to repo root for the
	// fixtures dir.
	candidates := []string{
		filepath.Join("..", "content", "testdata", "fixtures", name),
		filepath.Join("internal", "content", "testdata", "fixtures", name),
	}
	for _, p := range candidates {
		if data, err := os.ReadFile(p); err == nil {
			return data
		}
	}
	t.Fatalf("fixture %q not found in candidates: %v", name, candidates)
	return nil
}

func writeStructuredZIP(t *testing.T, path string, names []string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range names {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(fixtureBytes(t, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeStructuredTARGz(t *testing.T, path string, names []string) {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for _, name := range names {
		data := fixtureBytes(t, name)
		hdr := &tar.Header{Name: name, Size: int64(len(data)), Mode: 0o644}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestArchive_StructuredBodyExtraction_PDF verifies that a PDF
// inside a ZIP gets its body extracted via the pdfBody pipeline.
// The fixture's canonical body line "Sample PDF Fixture" must be
// findable via body.contains().
func TestArchive_StructuredBodyExtraction_PDF(t *testing.T) {
	tmp := t.TempDir()
	zipPath := filepath.Join(tmp, "with-pdf.zip")
	writeStructuredZIP(t, zipPath, []string{"sample.pdf"})

	result, err := search.WalkArchiveEntries(context.Background(), zipPath, search.ArchiveWalkOptions{
		Expr:        `is_pdf && body.contains("Sample PDF Fixture")`,
		IncludeBody: true,
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("WalkArchiveEntries: %v", err)
	}
	if result.MatchedEntries != 1 {
		t.Errorf("matched=%d, want 1 (PDF body should extract and match)", result.MatchedEntries)
	}
}

// TestArchive_StructuredBodyExtraction_DOCX same for DOCX.
func TestArchive_StructuredBodyExtraction_DOCX(t *testing.T) {
	tmp := t.TempDir()
	zipPath := filepath.Join(tmp, "with-docx.zip")
	writeStructuredZIP(t, zipPath, []string{"sample.docx"})

	result, err := search.WalkArchiveEntries(context.Background(), zipPath, search.ArchiveWalkOptions{
		Expr:        `content_type == "office/docx" && body.contains("Sample Office Fixture")`,
		IncludeBody: true,
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("WalkArchiveEntries: %v", err)
	}
	if result.MatchedEntries != 1 {
		t.Errorf("matched=%d, want 1", result.MatchedEntries)
	}
}

// TestArchive_StructuredBodyExtraction_EPUB same for EPUB.
func TestArchive_StructuredBodyExtraction_EPUB(t *testing.T) {
	tmp := t.TempDir()
	zipPath := filepath.Join(tmp, "with-epub.zip")
	writeStructuredZIP(t, zipPath, []string{"sample.epub"})

	result, err := search.WalkArchiveEntries(context.Background(), zipPath, search.ArchiveWalkOptions{
		Expr:        `is_epub && body.contains("Sample Office Fixture")`,
		IncludeBody: true,
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("WalkArchiveEntries: %v", err)
	}
	if result.MatchedEntries != 1 {
		t.Errorf("matched=%d, want 1", result.MatchedEntries)
	}
}

// TestArchive_StructuredBodyExtraction_EML same for RFC 5322 email.
// The eml fixture has "file-search-on" in its body (per
// internal/content/body_test.go's canonical substring set).
func TestArchive_StructuredBodyExtraction_EML(t *testing.T) {
	tmp := t.TempDir()
	zipPath := filepath.Join(tmp, "with-eml.zip")
	writeStructuredZIP(t, zipPath, []string{"sample.eml"})

	result, err := search.WalkArchiveEntries(context.Background(), zipPath, search.ArchiveWalkOptions{
		Expr:        `is_email && body.contains("file-search-on")`,
		IncludeBody: true,
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("WalkArchiveEntries: %v", err)
	}
	if result.MatchedEntries != 1 {
		t.Errorf("matched=%d, want 1 (eml body should extract)", result.MatchedEntries)
	}
}

// TestArchive_StructuredBodyExtraction_TarGz exercises the same
// pipeline through tar.gz instead of ZIP, with multiple structured
// formats coexisting.
func TestArchive_StructuredBodyExtraction_TarGz(t *testing.T) {
	tmp := t.TempDir()
	tgzPath := filepath.Join(tmp, "mixed.tar.gz")
	writeStructuredTARGz(t, tgzPath, []string{"sample.pdf", "sample.docx", "sample.epub"})

	result, err := search.WalkArchiveEntries(context.Background(), tgzPath, search.ArchiveWalkOptions{
		Expr:        `body.contains("Sample")`,
		IncludeBody: true,
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("WalkArchiveEntries: %v", err)
	}
	if result.MatchedEntries != 3 {
		paths := make([]string, len(result.Entries))
		for i, e := range result.Entries {
			paths[i] = e.Name
		}
		t.Errorf("matched=%d, want 3 (all three structured docs share 'Sample' in their bodies). Got: %v",
			result.MatchedEntries, paths)
	}
}

// TestArchive_BodyInIncludedAttributes verifies that when both
// IncludeBody and IncludeAttributes are set, the extracted body
// appears in the result's Attributes map (the v2 behaviour after
// the body-strip was removed for the wire response).
func TestArchive_BodyInIncludedAttributes(t *testing.T) {
	tmp := t.TempDir()
	zipPath := filepath.Join(tmp, "with-pdf.zip")
	writeStructuredZIP(t, zipPath, []string{"sample.pdf"})

	result, err := search.WalkArchiveEntries(context.Background(), zipPath, search.ArchiveWalkOptions{
		Expr:              `is_pdf`,
		IncludeBody:       true,
		IncludeAttributes: true,
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("WalkArchiveEntries: %v", err)
	}
	if len(result.Entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(result.Entries))
	}
	body, _ := result.Entries[0].Attributes["body"].(string)
	if body == "" {
		t.Errorf("body empty in attributes — IncludeAttributes+IncludeBody should preserve it")
	}
	if !strings.Contains(body, "Sample PDF Fixture") {
		t.Errorf("body doesn't contain canonical fixture content: %q", body)
	}
}

// TestArchive_NonStructuredBinaryEntriesSkip verifies that binary
// content types inside archives surface in the entry list (so an
// agent can SEE them) but don't produce body content (no extractor
// for opaque bytes). Combines with body.contains to confirm filter
// semantics.
func TestArchive_NonStructuredBinaryEntriesSkip(t *testing.T) {
	tmp := t.TempDir()
	zipPath := filepath.Join(tmp, "with-binary.zip")
	// Pack a PNG fixture — image/png, no body extractor.
	writeStructuredZIP(t, zipPath, []string{"sample.png"})

	// Body filter that would match anything textual.
	result, err := search.WalkArchiveEntries(context.Background(), zipPath, search.ArchiveWalkOptions{
		Expr:        `body.contains("anything")`,
		IncludeBody: true,
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("WalkArchiveEntries: %v", err)
	}
	if result.MatchedEntries != 0 {
		t.Errorf("matched=%d, want 0 (no body for image/png)", result.MatchedEntries)
	}

	// Without the body filter, the image should still surface.
	result, err = search.WalkArchiveEntries(context.Background(), zipPath, search.ArchiveWalkOptions{
		Expr: `is_image`,
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("WalkArchiveEntries: %v", err)
	}
	if result.MatchedEntries != 1 {
		t.Errorf("matched=%d, want 1 (image entry should detect)", result.MatchedEntries)
	}
}

// helper for sanity — make sure the test build sees the fixtures.
func TestFixtureSanityCheck(t *testing.T) {
	for _, name := range []string{"sample.pdf", "sample.docx", "sample.epub", "sample.eml", "sample.png"} {
		data := fixtureBytes(t, name)
		if len(data) == 0 {
			t.Errorf("fixture %s has 0 bytes", name)
		}
	}
	_ = io.EOF // keep io import live in case helper grows
}
