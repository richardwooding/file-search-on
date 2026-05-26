package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/richardwooding/file-search-on/internal/celexpr"
	"github.com/richardwooding/file-search-on/internal/search"
)

func TestResolveToken(t *testing.T) {
	mod := time.Date(2024, 7, 15, 10, 30, 0, 0, time.UTC)
	taken := time.Date(2019, 3, 2, 8, 0, 0, 0, time.UTC)
	r := search.Result{
		Path:        "/photos/IMG_1234.HEIC",
		ContentType: "image/heic",
		Size:        4096,
		Attrs: &celexpr.FileAttributes{
			ModTime: mod,
			Extra: map[string]any{
				"camera_make": "Canon",
				"taken_at":    taken,
			},
		},
	}
	cases := []struct {
		token, want string
	}{
		{"basename", "IMG_1234.HEIC"},
		{"stem", "IMG_1234"},
		{"ext", "HEIC"},
		{"dir", "photos"},
		{"content_type", "image/heic"}, // sanitised by caller, not here
		{"size", "4096"},
		{"mtime_year", "2024"},
		{"mtime_month", "2024-07"},
		{"mtime_day", "2024-07-15"},
		{"taken_at_year", "2019"},
		{"camera_make", "Canon"},
		{"nonexistent", ""},
	}
	for _, tc := range cases {
		t.Run(tc.token, func(t *testing.T) {
			if got := resolveToken(tc.token, r); got != tc.want {
				t.Errorf("resolveToken(%q) = %q, want %q", tc.token, got, tc.want)
			}
		})
	}
}

func TestSanitizeComponent(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Canon", "Canon"},
		{"", "unknown"},
		{"   ", "unknown"},
		{"image/heic", "image-heic"},
		{".", "unknown"},
		{"..", "unknown"},
		{"a/b/c", "a-b-c"},
	}
	for _, tc := range cases {
		if got := sanitizeComponent(tc.in); got != tc.want {
			t.Errorf("sanitizeComponent(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRenderTarget(t *testing.T) {
	o := &OrganizeCmd{LinkInto: "/out/{camera_make}/{mtime_year}/{basename}"}
	r := search.Result{
		Path: "/photos/shot.jpg",
		Attrs: &celexpr.FileAttributes{
			ModTime: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			Extra:   map[string]any{"camera_make": "Nikon"},
		},
	}
	got, err := o.renderTarget(r)
	if err != nil {
		t.Fatal(err)
	}
	want := "/out/Nikon/2024/shot.jpg"
	if got != want {
		t.Errorf("renderTarget = %q, want %q", got, want)
	}

	// Missing camera_make → "unknown" bucket.
	r2 := search.Result{Path: "/photos/x.jpg", Attrs: &celexpr.FileAttributes{ModTime: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), Extra: map[string]any{}}}
	got2, _ := o.renderTarget(r2)
	if got2 != "/out/unknown/2024/x.jpg" {
		t.Errorf("renderTarget (missing) = %q, want /out/unknown/2024/x.jpg", got2)
	}
}

// TestOrganizeEndToEnd walks a synthetic tree and confirms the symlink
// view is created with the templated structure.
func TestOrganizeEndToEnd(t *testing.T) {
	src := t.TempDir()
	// Two markdown files, one JSON.
	mustWrite(t, filepath.Join(src, "a.md"), "# A\n")
	mustWrite(t, filepath.Join(src, "b.md"), "# B\n")
	mustWrite(t, filepath.Join(src, "c.json"), `{"x":1}`)

	out := t.TempDir()
	o := &OrganizeCmd{
		Expr:       "is_markdown",
		LinkInto:   filepath.Join(out, "{content_type}", "{basename}"),
		Dir:        []string{src},
		OnConflict: "skip",
		Output:     "default",
	}
	if err := o.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Two markdown files should be linked under out/markdown/.
	for _, name := range []string{"a.md", "b.md"} {
		link := filepath.Join(out, "markdown", name)
		fi, err := os.Lstat(link)
		if err != nil {
			t.Errorf("expected symlink %s: %v", link, err)
			continue
		}
		if fi.Mode()&os.ModeSymlink == 0 {
			t.Errorf("%s is not a symlink", link)
		}
		// Resolves back to the original.
		resolved, err := filepath.EvalSymlinks(link)
		if err != nil {
			t.Errorf("EvalSymlinks(%s): %v", link, err)
			continue
		}
		wantSrc, _ := filepath.EvalSymlinks(filepath.Join(src, name))
		if resolved != wantSrc {
			t.Errorf("%s resolves to %s, want %s", link, resolved, wantSrc)
		}
	}

	// The JSON file should NOT be organized (filtered out by is_markdown).
	if _, err := os.Lstat(filepath.Join(out, "json", "c.json")); err == nil {
		t.Errorf("c.json should not have been organized")
	}
}

func TestOrganizeDryRunCreatesNothing(t *testing.T) {
	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "a.md"), "# A\n")
	out := t.TempDir()

	o := &OrganizeCmd{
		Expr:     "is_markdown",
		LinkInto: filepath.Join(out, "{basename}"),
		Dir:      []string{src},
		DryRun:   true,
		Output:   "default",
	}
	if err := o.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(out, "a.md")); err == nil {
		t.Errorf("dry-run should not create the symlink")
	}
}

func TestOrganizeCopyInstead(t *testing.T) {
	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "a.md"), "# hello\n")
	out := t.TempDir()

	o := &OrganizeCmd{
		Expr:     "is_markdown",
		LinkInto: filepath.Join(out, "{basename}"),
		Dir:      []string{src},
		Copy:     true,
		Output:   "default",
	}
	if err := o.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	target := filepath.Join(out, "a.md")
	fi, err := os.Lstat(target)
	if err != nil {
		t.Fatalf("expected copied file: %v", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Errorf("--copy-instead produced a symlink, want a regular file")
	}
	data, _ := os.ReadFile(target)
	if string(data) != "# hello\n" {
		t.Errorf("copied content = %q, want %q", data, "# hello\n")
	}
}

func TestOrganizeConflictNumber(t *testing.T) {
	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "a.md"), "# A\n")
	out := t.TempDir()
	// Pre-create the target so the first link collides.
	collide := filepath.Join(out, "a.md")
	mustWrite(t, collide, "existing\n")

	o := &OrganizeCmd{
		Expr:       "is_markdown",
		LinkInto:   filepath.Join(out, "{basename}"),
		Dir:        []string{src},
		OnConflict: "number",
		Output:     "default",
	}
	if err := o.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Original untouched; numbered variant created.
	if data, _ := os.ReadFile(collide); string(data) != "existing\n" {
		t.Errorf("original target was clobbered")
	}
	if _, err := os.Lstat(filepath.Join(out, "a (1).md")); err != nil {
		t.Errorf("expected numbered variant 'a (1).md': %v", err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
