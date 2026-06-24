package search_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// mkGitTree builds a tree with a root .git/, a nested (submodule-style)
// sub/.git/, and ordinary source files, and returns the root.
func mkGitTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("main.go", "package main\n\nfunc main() {}\n")
	write("sub/lib.go", "package sub\n")
	write(".git/config", "[core]\n")
	write(".git/refs/heads/main", "deadbeef\n")
	write("sub/.git/config", "[core]\n") // nested .git (submodule-style)
	return root
}

func walkPaths(t *testing.T, opts search.Options) []string {
	t.Helper()
	if opts.Expr == "" {
		opts.Expr = "true"
	}
	res, err := search.Walk(t.Context(), opts, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	out := make([]string, 0, len(res))
	for _, r := range res {
		out = append(out, filepath.ToSlash(r.Path))
	}
	return out
}

// anyUnderGit reports whether any path has a `.git` path component — i.e. it
// lives inside a .git directory. Wrapping the (slash-normalised) path in
// separators lets one Contains catch a `.git` segment at the start, middle,
// or end. Works for absolute or relative paths.
func anyUnderGit(paths []string) bool {
	for _, p := range paths {
		if strings.Contains("/"+filepath.ToSlash(p)+"/", "/.git/") {
			return true
		}
	}
	return false
}

// TestWalk_SkipsGitByDefault: .git (root and nested) is pruned by default,
// while ordinary files are still walked.
func TestWalk_SkipsGitByDefault(t *testing.T) {
	root := mkGitTree(t)
	paths := walkPaths(t, search.Options{Root: root})

	if anyUnderGit(paths) {
		t.Errorf(".git contents leaked into default walk: %v", paths)
	}
	// Sanity: the ordinary files ARE present.
	var hasMain, hasLib bool
	for _, p := range paths {
		switch filepath.Base(p) {
		case "main.go":
			hasMain = true
		case "lib.go":
			hasLib = true
		}
	}
	if !hasMain || !hasLib {
		t.Errorf("expected main.go and sub/lib.go in results; got %v", paths)
	}
}

// TestWalk_IncludeGitDir: with IncludeGitDir, .git contents are walked.
func TestWalk_IncludeGitDir(t *testing.T) {
	root := mkGitTree(t)
	paths := walkPaths(t, search.Options{Root: root, IncludeGitDir: true})
	if !anyUnderGit(paths) {
		t.Errorf("IncludeGitDir=true should surface .git contents; got %v", paths)
	}
}

// TestWalk_GitRootExempt: pointing the walk root directly at a .git dir still
// walks it (the root is exempt from the basename prune) without the flag.
func TestWalk_GitRootExempt(t *testing.T) {
	root := mkGitTree(t)
	paths := walkPaths(t, search.Options{Root: filepath.Join(root, ".git")})
	if len(paths) == 0 {
		t.Fatalf("walking a .git root directly yielded nothing; want its contents")
	}
	var hasConfig bool
	for _, p := range paths {
		if filepath.Base(p) == "config" {
			hasConfig = true
		}
	}
	if !hasConfig {
		t.Errorf("expected .git/config when root is the .git dir; got %v", paths)
	}
}
