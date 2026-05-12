package search_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// TestWalker_MultiDirAggregates verifies Roots []string walks every
// listed root and joins matches into one result set. The two roots
// are separate temp dirs — there's no parent containing both, so
// the walker has to honour Roots and not fall back to Root.
func TestWalker_MultiDirAggregates(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	if err := os.WriteFile(filepath.Join(dirA, "a.md"), []byte("# a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirB, "b.md"), []byte("# b\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	results, err := search.Walk(t.Context(), search.Options{
		Roots: []string{dirA, dirB},
		Expr:  "is_markdown",
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d matches, want 2 across the two roots; paths: %v", len(results), paths(results))
	}
	got := map[string]bool{}
	for _, r := range results {
		got[filepath.Base(r.Path)] = true
	}
	if !got["a.md"] || !got["b.md"] {
		t.Errorf("expected matches in both roots; got %v", got)
	}
}

// TestWalker_MultiDirPerRootGitignore verifies that each root's
// .gitignore is honoured independently — a pattern in dirA's
// .gitignore must NOT exclude a same-named file in dirB.
func TestWalker_MultiDirPerRootGitignore(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	// dirA ignores secrets.md
	if err := os.WriteFile(filepath.Join(dirA, ".gitignore"), []byte("secrets.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{
		filepath.Join(dirA, "secrets.md"),
		filepath.Join(dirA, "kept.md"),
		filepath.Join(dirB, "secrets.md"),
	} {
		if err := os.WriteFile(p, []byte("# x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	results, err := search.Walk(t.Context(), search.Options{
		Roots:            []string{dirA, dirB},
		Expr:             "is_markdown",
		RespectGitignore: true,
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	// dirA/secrets.md is pruned; dirA/kept.md and dirB/secrets.md
	// both pass.
	var aSecrets, bSecrets, aKept bool
	for _, r := range results {
		switch {
		case strings.HasPrefix(r.Path, dirA) && strings.HasSuffix(r.Path, "secrets.md"):
			aSecrets = true
		case strings.HasPrefix(r.Path, dirB) && strings.HasSuffix(r.Path, "secrets.md"):
			bSecrets = true
		case strings.HasPrefix(r.Path, dirA) && strings.HasSuffix(r.Path, "kept.md"):
			aKept = true
		}
	}
	if aSecrets {
		t.Errorf("dirA/secrets.md leaked through despite dirA/.gitignore")
	}
	if !bSecrets {
		t.Errorf("dirB/secrets.md was pruned — but dirA's .gitignore shouldn't affect dirB")
	}
	if !aKept {
		t.Errorf("dirA/kept.md missing from results")
	}
}

// TestWalker_RootStillWorks verifies the single-root path is
// untouched when Roots is empty.
func TestWalker_RootStillWorks(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("# a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	results, err := search.Walk(t.Context(), search.Options{
		Root: dir,
		Expr: "is_markdown",
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d, want 1 (single-root back-compat)", len(results))
	}
}
