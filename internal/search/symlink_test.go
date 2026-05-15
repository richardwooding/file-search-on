package search_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// symlinks need real OS paths — we can't commit them to git portably
// (Windows test runs would explode), so every test here creates its
// own tree under t.TempDir() and skips on platforms where os.Symlink
// requires elevated permissions (Windows non-admin).
func mustSymlink(t *testing.T, oldname, newname string) {
	t.Helper()
	if err := os.Symlink(oldname, newname); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("os.Symlink requires SeCreateSymbolicLinkPrivilege on Windows: %v", err)
		}
		t.Fatalf("os.Symlink(%q, %q): %v", oldname, newname, err)
	}
}

// findResult returns the first Result whose Path matches the absolute
// equivalent of want. Returns nil if no such result exists.
func findResult(results []search.Result, want string) *search.Result {
	for i, r := range results {
		if r.Path == want {
			return &results[i]
		}
	}
	return nil
}

// TestSymlink_FileLink_IsSymlinkPredicate verifies that a symlink to
// a regular file surfaces in search results with is_symlink=true and
// target_path populated, while still carrying the target's content_type
// (the most common workflow: "find every Markdown file, including
// symlinks").
func TestSymlink_FileLink_IsSymlinkPredicate(t *testing.T) {
	tmp := t.TempDir()
	realPath := filepath.Join(tmp, "real.md")
	linkPath := filepath.Join(tmp, "link.md")
	if err := os.WriteFile(realPath, []byte("# Hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustSymlink(t, realPath, linkPath)

	results, err := search.Walk(context.Background(), search.Options{
		Root:               tmp,
		Expr:               "is_markdown",
		IncludeAttributes:  true,
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2 (real + link)", len(results))
	}

	real := findResult(results, realPath)
	link := findResult(results, linkPath)
	if real == nil || link == nil {
		t.Fatalf("missing result: real=%v link=%v", real, link)
	}
	if real.Attrs.IsSymlink {
		t.Errorf("real.md: IsSymlink=true, want false")
	}
	if !link.Attrs.IsSymlink {
		t.Errorf("link.md: IsSymlink=false, want true")
	}
	if got := link.Attrs.Extra["target_path"]; got != realPath {
		t.Errorf("link.md target_path = %v, want %q", got, realPath)
	}
	if link.Attrs.IsBrokenSymlink {
		t.Errorf("link.md: IsBrokenSymlink=true, want false")
	}
	// Both should pick up the markdown content_type — the target's,
	// since fs.Stat follows the link.
	if real.ContentType != "markdown" {
		t.Errorf("real.md content_type = %q, want markdown", real.ContentType)
	}
	if link.ContentType != "markdown" {
		t.Errorf("link.md content_type = %q, want markdown", link.ContentType)
	}
}

// TestSymlink_BrokenLink_SurfacesAsEntry verifies that a dangling
// symlink doesn't error the walk — it surfaces as an entry with
// is_symlink=true AND is_broken_symlink=true, with empty content_type
// (no target to detect against).
func TestSymlink_BrokenLink_SurfacesAsEntry(t *testing.T) {
	tmp := t.TempDir()
	linkPath := filepath.Join(tmp, "broken.md")
	mustSymlink(t, filepath.Join(tmp, "nonexistent.md"), linkPath)

	results, err := search.Walk(context.Background(), search.Options{
		Root:              tmp,
		Expr:              "is_symlink",
		IncludeAttributes: true,
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1 (broken link)", len(results))
	}
	r := results[0]
	if !r.Attrs.IsSymlink || !r.Attrs.IsBrokenSymlink {
		t.Errorf("broken: IsSymlink=%v IsBrokenSymlink=%v; want both true",
			r.Attrs.IsSymlink, r.Attrs.IsBrokenSymlink)
	}
	if r.ContentType != "" {
		t.Errorf("broken: content_type=%q, want empty (no target to detect)", r.ContentType)
	}
}

// TestSymlink_DirLink_LeafByDefault verifies that a symlink to a
// directory is surfaced as a single leaf entry (is_symlink=true) and
// its contents are NOT walked when FollowSymlinks=false (default).
func TestSymlink_DirLink_LeafByDefault(t *testing.T) {
	tmp := t.TempDir()
	realDir := filepath.Join(tmp, "real-dir")
	if err := os.Mkdir(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realDir, "child.md"), []byte("# child\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	linkDir := filepath.Join(tmp, "link-dir")
	mustSymlink(t, realDir, linkDir)

	results, err := search.Walk(context.Background(), search.Options{
		Root:              tmp,
		Expr:              "true",
		IncludeAttributes: true,
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	// Expected:
	//   real-dir/child.md       — non-symlink markdown
	//   link-dir                — symlink leaf entry (NOT walked)
	if len(results) != 2 {
		paths := []string{}
		for _, r := range results {
			paths = append(paths, r.Path)
		}
		t.Fatalf("got %d results: %v; want 2 (child + dir-symlink-leaf)", len(results), paths)
	}
	link := findResult(results, linkDir)
	if link == nil {
		t.Fatalf("dir symlink missing from results")
	}
	if !link.Attrs.IsSymlink {
		t.Errorf("link-dir: IsSymlink=false, want true")
	}
	if got := link.Attrs.Extra["target_path"]; got != realDir {
		t.Errorf("link-dir target_path = %v, want %q", got, realDir)
	}
}

// TestSymlink_DirLink_FollowsWhenEnabled verifies that with
// FollowSymlinks=true the walker descends into the resolved target
// AND the original symlink-anchored paths surface in the results.
func TestSymlink_DirLink_FollowsWhenEnabled(t *testing.T) {
	tmp := t.TempDir()
	realDir := filepath.Join(tmp, "real-dir")
	if err := os.Mkdir(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realDir, "child.md"), []byte("# child\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	linkDir := filepath.Join(tmp, "link-dir")
	mustSymlink(t, realDir, linkDir)

	results, err := search.Walk(context.Background(), search.Options{
		Root:              tmp,
		Expr:              "is_markdown",
		FollowSymlinks:    true,
		IncludeAttributes: true,
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	// Two markdown results: real-dir/child.md AND link-dir/child.md
	// (the symlink itself is NOT a markdown — it's a dir-symlink and
	// the descent walks the target's contents under the link path).
	if len(results) != 2 {
		paths := []string{}
		for _, r := range results {
			paths = append(paths, r.Path)
		}
		t.Fatalf("got %d markdown results: %v; want 2 (descended)", len(results), paths)
	}
	want := []string{
		filepath.Join(tmp, "link-dir", "child.md"),
		filepath.Join(tmp, "real-dir", "child.md"),
	}
	got := []string{}
	for _, r := range results {
		got = append(got, r.Path)
	}
	// Walk doesn't promise ordering between roots; sort for stable
	// comparison.
	if !equalStringSlicesIgnoringOrder(got, want) {
		t.Errorf("paths = %v, want %v", got, want)
	}
}

func equalStringSlicesIgnoringOrder(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	count := make(map[string]int)
	for _, s := range a {
		count[s]++
	}
	for _, s := range b {
		count[s]--
	}
	for _, v := range count {
		if v != 0 {
			return false
		}
	}
	return true
}
