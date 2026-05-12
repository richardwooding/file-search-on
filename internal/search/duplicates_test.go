package search_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/index"
	"github.com/richardwooding/file-search-on/internal/search"
)

// TestFindDuplicates_Basic seeds two byte-identical files plus
// one unique file and asserts the duplicates output groups them
// correctly.
func TestFindDuplicates_Basic(t *testing.T) {
	dir := t.TempDir()
	body := strings.Repeat("abc\n", 64) // 256 bytes — large enough to escape MinSize defaults
	mustWriteFile(t, filepath.Join(dir, "a.txt"), body)
	mustWriteFile(t, filepath.Join(dir, "b.txt"), body)
	mustWriteFile(t, filepath.Join(dir, "c.txt"), "different\n")

	dups, err := search.FindDuplicates(t.Context(), search.Options{
		Root: dir,
		Expr: "true",
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("FindDuplicates: %v", err)
	}
	if dups.DuplicateGroups != 1 {
		t.Fatalf("DuplicateGroups=%d want 1; %+v", dups.DuplicateGroups, dups.Duplicates)
	}
	if dups.Duplicates[0].Count != 2 {
		t.Errorf("group Count=%d want 2", dups.Duplicates[0].Count)
	}
	if dups.Duplicates[0].WastedBytes != int64(len(body)) {
		t.Errorf("WastedBytes=%d want %d (one extra copy of %d bytes)",
			dups.Duplicates[0].WastedBytes, len(body), len(body))
	}
	for _, p := range dups.Duplicates[0].Paths {
		base := filepath.Base(p)
		if base != "a.txt" && base != "b.txt" {
			t.Errorf("unexpected path in duplicates group: %s", p)
		}
	}
}

// TestFindDuplicates_SizeBucketingSkipsUniqueSizes verifies the
// optimization: a file with a unique size cannot be a duplicate,
// so it should not be hashed. We can't directly observe "didn't
// open the file" but we can confirm it's absent from the output.
func TestFindDuplicates_SizeBucketingSkipsUniqueSizes(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "uniq-1.txt"), "alpha\n")     // 6 bytes
	mustWriteFile(t, filepath.Join(dir, "uniq-2.txt"), "beta+gamma\n") // 11 bytes
	body := strings.Repeat("z", 64)
	mustWriteFile(t, filepath.Join(dir, "dup-a.txt"), body)
	mustWriteFile(t, filepath.Join(dir, "dup-b.txt"), body)

	dups, err := search.FindDuplicates(t.Context(), search.Options{
		Root: dir,
		Expr: "true",
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("FindDuplicates: %v", err)
	}
	if dups.DuplicateGroups != 1 {
		t.Fatalf("DuplicateGroups=%d want 1", dups.DuplicateGroups)
	}
	// The unique-sized files must not appear anywhere.
	for _, g := range dups.Duplicates {
		for _, p := range g.Paths {
			if strings.Contains(p, "uniq-") {
				t.Errorf("unique-size file %s leaked into duplicates", p)
			}
		}
	}
}

// TestFindDuplicates_MinSizeFilters skips files smaller than the
// threshold from both passes.
func TestFindDuplicates_MinSizeFilters(t *testing.T) {
	dir := t.TempDir()
	small := "x\n" // 2 bytes
	big := strings.Repeat("y", 100)
	mustWriteFile(t, filepath.Join(dir, "small-a.txt"), small)
	mustWriteFile(t, filepath.Join(dir, "small-b.txt"), small)
	mustWriteFile(t, filepath.Join(dir, "big-a.txt"), big)
	mustWriteFile(t, filepath.Join(dir, "big-b.txt"), big)

	dups, err := search.FindDuplicates(t.Context(), search.Options{
		Root:    dir,
		Expr:    "true",
		MinSize: 50,
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("FindDuplicates: %v", err)
	}
	if dups.DuplicateGroups != 1 {
		t.Fatalf("DuplicateGroups=%d want 1 (only the >=50-byte pair)", dups.DuplicateGroups)
	}
	for _, p := range dups.Duplicates[0].Paths {
		if strings.Contains(p, "small-") {
			t.Errorf("small file %s leaked despite MinSize=50", p)
		}
	}
}

// TestFindDuplicates_IndexCachesHash verifies the cache write-back.
// Run twice with an in-memory Index; the second run shouldn't have
// to compute hashes (we can't observe the read directly, but we
// can check that the cached entries have non-empty Hash).
func TestFindDuplicates_IndexCachesHash(t *testing.T) {
	dir := t.TempDir()
	body := strings.Repeat("h", 256)
	mustWriteFile(t, filepath.Join(dir, "a.txt"), body)
	mustWriteFile(t, filepath.Join(dir, "b.txt"), body)

	idx := index.NewMemory()
	defer func() { _ = idx.Close() }()

	// First run populates the cache (including Hash fields).
	if _, err := search.FindDuplicates(t.Context(), search.Options{
		Root:  dir,
		Expr:  "true",
		Index: idx,
	}, content.DefaultRegistry()); err != nil {
		t.Fatalf("first FindDuplicates: %v", err)
	}

	// After the first run, the cache should have the entries with
	// hashes. Probe via Lookup using the actual on-disk mtime.
	for _, name := range []string{"a.txt", "b.txt"} {
		fpath := filepath.Join(dir, name)
		info, err := os.Stat(fpath)
		if err != nil {
			t.Fatal(err)
		}
		entry, ok := idx.Lookup(fpath, info.Size(), info.ModTime())
		if !ok {
			t.Fatalf("cache miss for %s after first run", fpath)
		}
		if entry.Hash == "" {
			t.Errorf("entry for %s has empty Hash after first run; cache write-back didn't fire", fpath)
		}
	}
}
