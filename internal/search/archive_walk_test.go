package search_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/index"
	"github.com/richardwooding/file-search-on/internal/search"
)

// writeZIP builds a small ZIP at path with the given (name → bytes)
// map. Test helper — fails the test on any write error.
func writeZIP(t *testing.T, path string, files map[string]string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
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

// writeTAR / writeTARGz are the tar-flavoured counterparts.
func writeTAR(t *testing.T, path string, files map[string]string, gzipped bool) {
	t.Helper()
	var buf bytes.Buffer
	var w io.Writer = &buf
	var zw *gzip.Writer
	if gzipped {
		zw = gzip.NewWriter(&buf)
		w = zw
	}
	tw := tar.NewWriter(w)
	for name, body := range files {
		hdr := &tar.Header{
			Name:    name,
			Size:    int64(len(body)),
			Mode:    0o644,
			ModTime: time.Now(),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if gzipped {
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestWalkArchiveEntries_ZIP exercises the basic ZIP iteration path.
func TestWalkArchiveEntries_ZIP(t *testing.T) {
	tmp := t.TempDir()
	archivePath := filepath.Join(tmp, "test.zip")
	writeZIP(t, archivePath, map[string]string{
		"README.md":   "# Project\n\nA test project.\n",
		"src/main.go": "package main\n\nfunc main() {}\n",
		"src/util.go": "package main\n\nfunc util() {}\n",
	})

	result, err := search.WalkArchiveEntries(context.Background(), archivePath, search.ArchiveWalkOptions{}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("WalkArchiveEntries: %v", err)
	}
	if len(result.Entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(result.Entries))
	}
	if result.ScannedEntries != 3 || result.MatchedEntries != 3 {
		t.Errorf("scanned=%d matched=%d, want 3/3", result.ScannedEntries, result.MatchedEntries)
	}
}

// TestWalkArchiveEntries_CELFilter verifies that a per-entry CEL
// expression filters the surface correctly. Walks the same ZIP and
// returns only the Go source files.
func TestWalkArchiveEntries_CELFilter(t *testing.T) {
	tmp := t.TempDir()
	archivePath := filepath.Join(tmp, "test.zip")
	writeZIP(t, archivePath, map[string]string{
		"README.md":   "# Project\n",
		"src/main.go": "package main\n\nfunc main() {}\n",
		"src/util.go": "package main\n\nfunc util() {}\n",
	})

	result, err := search.WalkArchiveEntries(context.Background(), archivePath, search.ArchiveWalkOptions{
		Expr: "is_source && language == \"go\"",
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("WalkArchiveEntries: %v", err)
	}
	if len(result.Entries) != 2 {
		t.Fatalf("got %d entries, want 2 (the Go files)", len(result.Entries))
	}
	for _, e := range result.Entries {
		if e.ContentType != "source/go" {
			t.Errorf("entry %s: content_type=%q, want source/go", e.Name, e.ContentType)
		}
	}
}

// TestWalkArchiveEntries_Glob verifies the glob pre-prune.
func TestWalkArchiveEntries_Glob(t *testing.T) {
	tmp := t.TempDir()
	archivePath := filepath.Join(tmp, "test.zip")
	writeZIP(t, archivePath, map[string]string{
		"README.md":   "# Project\n",
		"src/main.go": "package main\n",
		"src/util.go": "package main\n",
		"NOTES.txt":   "plain notes\n",
	})

	result, err := search.WalkArchiveEntries(context.Background(), archivePath, search.ArchiveWalkOptions{
		Glob: "*.go",
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("WalkArchiveEntries: %v", err)
	}
	if len(result.Entries) != 2 {
		t.Fatalf("glob=*.go: got %d entries, want 2", len(result.Entries))
	}
}

// TestWalkArchiveEntries_TARGz verifies the tar.gz iterator.
func TestWalkArchiveEntries_TARGz(t *testing.T) {
	tmp := t.TempDir()
	archivePath := filepath.Join(tmp, "test.tar.gz")
	writeTAR(t, archivePath, map[string]string{
		"a.md": "# A\n",
		"b.md": "# B\n",
	}, true)

	result, err := search.WalkArchiveEntries(context.Background(), archivePath, search.ArchiveWalkOptions{
		Expr: "is_markdown",
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("WalkArchiveEntries: %v", err)
	}
	if len(result.Entries) != 2 {
		t.Fatalf("tar.gz: got %d entries, want 2", len(result.Entries))
	}
}

// TestWalkArchiveEntries_BodyFilter verifies that include_body=true
// makes body.contains() filters fire against archive entries.
func TestWalkArchiveEntries_BodyFilter(t *testing.T) {
	tmp := t.TempDir()
	archivePath := filepath.Join(tmp, "test.zip")
	writeZIP(t, archivePath, map[string]string{
		"a.md": "# A\n\nThis mentions kerflumpus.\n",
		"b.md": "# B\n\nDifferent content.\n",
	})

	result, err := search.WalkArchiveEntries(context.Background(), archivePath, search.ArchiveWalkOptions{
		Expr:        "is_markdown && body.contains(\"kerflumpus\")",
		IncludeBody: true,
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("WalkArchiveEntries: %v", err)
	}
	if len(result.Entries) != 1 {
		t.Fatalf("body filter: got %d entries, want 1", len(result.Entries))
	}
	if result.Entries[0].Name != "a.md" {
		t.Errorf("got %q, want a.md", result.Entries[0].Name)
	}
}

// TestWalkArchiveEntries_Cache verifies a miss-then-hit cycle.
func TestWalkArchiveEntries_Cache(t *testing.T) {
	tmp := t.TempDir()
	archivePath := filepath.Join(tmp, "test.zip")
	writeZIP(t, archivePath, map[string]string{
		"a.md":   "# A\n",
		"b.md":   "# B\n",
		"main.go": "package main\n",
	})

	idx := index.NewMemory()
	defer func() { _ = idx.Close() }()

	// Miss: populates cache.
	r1, err := search.WalkArchiveEntries(context.Background(), archivePath, search.ArchiveWalkOptions{
		Index: idx,
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if r1.CacheHit {
		t.Errorf("first call: CacheHit=true, want false")
	}
	if len(r1.Entries) != 3 {
		t.Fatalf("first call: %d entries, want 3", len(r1.Entries))
	}

	// Hit: cached records filtered locally.
	r2, err := search.WalkArchiveEntries(context.Background(), archivePath, search.ArchiveWalkOptions{
		Index: idx,
		Expr:  "is_markdown",
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if !r2.CacheHit {
		t.Errorf("second call: CacheHit=false, want true")
	}
	if len(r2.Entries) != 2 {
		t.Fatalf("cached + filter: %d entries, want 2 (only markdown)", len(r2.Entries))
	}
}

// TestReadFileInArchive_Found verifies the happy path.
func TestReadFileInArchive_Found(t *testing.T) {
	tmp := t.TempDir()
	archivePath := filepath.Join(tmp, "test.zip")
	writeZIP(t, archivePath, map[string]string{
		"README.md":  "# Project\n",
		"main.go":    "package main\n\nfunc main() {}\n",
	})

	r, err := search.ReadFileInArchive(context.Background(), archivePath, "main.go", 1024, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("ReadFileInArchive: %v", err)
	}
	if r.Name != "main.go" {
		t.Errorf("Name=%q, want main.go", r.Name)
	}
	if string(r.Content) != "package main\n\nfunc main() {}\n" {
		t.Errorf("Content=%q, unexpected", r.Content)
	}
	if r.ContentType != "source/go" {
		t.Errorf("ContentType=%q, want source/go", r.ContentType)
	}
	if r.Truncated {
		t.Errorf("Truncated=true unexpected for small file")
	}
}

// TestReadFileInArchive_NotFound verifies ErrArchiveEntryNotFound.
func TestReadFileInArchive_NotFound(t *testing.T) {
	tmp := t.TempDir()
	archivePath := filepath.Join(tmp, "test.zip")
	writeZIP(t, archivePath, map[string]string{"only.txt": "hello"})

	_, err := search.ReadFileInArchive(context.Background(), archivePath, "missing.txt", 1024, content.DefaultRegistry())
	if !errors.Is(err, search.ErrArchiveEntryNotFound) {
		t.Errorf("err=%v, want ErrArchiveEntryNotFound", err)
	}
}

// TestReadFileInArchive_Truncated verifies the max-bytes cap.
func TestReadFileInArchive_Truncated(t *testing.T) {
	tmp := t.TempDir()
	archivePath := filepath.Join(tmp, "test.zip")
	big := bytes.Repeat([]byte("x"), 4096)
	writeZIP(t, archivePath, map[string]string{"big.txt": string(big)})

	r, err := search.ReadFileInArchive(context.Background(), archivePath, "big.txt", 100, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("ReadFileInArchive: %v", err)
	}
	if !r.Truncated {
		t.Errorf("Truncated=false, want true")
	}
	if len(r.Content) != 100 {
		t.Errorf("len(Content)=%d, want 100", len(r.Content))
	}
}
