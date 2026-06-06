package search_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	contentpkg "github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/gitmeta"
	"github.com/richardwooding/file-search-on/internal/search"
)

// initRepoForTest seeds a t.TempDir with a one-commit git repo. Skips
// when git isn't on PATH (CI image without git installed).
func initRepoForTest(t *testing.T) string {
	t.Helper()
	if !gitmeta.HasGitBinary() {
		t.Skip("git binary not on PATH; skipping WithGit integration test")
	}
	root := t.TempDir()
	mustGit(t, root, "init", "-q", "-b", "main")
	mustGit(t, root, "config", "user.email", "test@example.com")
	mustGit(t, root, "config", "user.name", "Walker Test")
	mustGit(t, root, "config", "commit.gpgsign", "false")
	// Add a tracked file...
	if err := os.WriteFile(filepath.Join(root, "tracked.md"), []byte("# Tracked\n"), 0o644); err != nil {
		t.Fatalf("write tracked: %v", err)
	}
	mustGit(t, root, "add", "tracked.md")
	mustGit(t, root, "commit", "-q", "-m", "Initial: add tracked.md")
	// ...and an untracked file alongside it.
	if err := os.WriteFile(filepath.Join(root, "scratch.md"), []byte("# Untracked\n"), 0o644); err != nil {
		t.Fatalf("write scratch: %v", err)
	}
	return root
}

func mustGit(t *testing.T, root string, args ...string) {
	t.Helper()
	full := append([]string{"-C", root}, args...)
	cmd := exec.Command("git", full...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// TestWalk_WithGit_PopulatesAttrs runs a Walk over a real git tree with
// WithGit=true and asserts the resulting matches carry git_* metadata.
// The Extra map is the surface available to test code (the typed
// FileAttributes fields are private to celexpr) but the integration
// test still confirms the wiring lands the data on Match by checking
// the CEL filter fires.
func TestWalk_WithGit_FiltersByCommitTime(t *testing.T) {
	root := initRepoForTest(t)

	// Filter for tracked Markdown — should match tracked.md but NOT
	// scratch.md (which is on disk but not in the index).
	opts := search.Options{
		Root:    root,
		Expr:    "is_markdown && is_git_tracked",
		Workers: 1,
		WithGit: true,
	}
	results, err := search.Walk(context.Background(), opts, contentpkg.DefaultRegistry())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1 (tracked.md only)", len(results))
	}
	if filepath.Base(results[0].Path) != "tracked.md" {
		t.Errorf("matched %s, want tracked.md", filepath.Base(results[0].Path))
	}
}

func TestWalk_WithGit_OffLeavesAttrsEmpty(t *testing.T) {
	root := initRepoForTest(t)
	// Same expr but WithGit:false — is_git_tracked is always false, so
	// nothing matches.
	opts := search.Options{
		Root:    root,
		Expr:    "is_markdown && is_git_tracked",
		Workers: 1,
		WithGit: false,
	}
	results, err := search.Walk(context.Background(), opts, contentpkg.DefaultRegistry())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("WithGit=false should produce no matches against is_git_tracked filter; got %d", len(results))
	}
}

func TestWalk_WithGit_NonGitTreeSilentNoop(t *testing.T) {
	if !gitmeta.HasGitBinary() {
		t.Skip("git binary not on PATH")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "x.md"), []byte("# Hi\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Plain is_markdown should match regardless of WithGit (gitmeta
	// returns nil cache for non-git trees → git_* fields stay zero
	// but other attributes work normally).
	opts := search.Options{
		Root:    root,
		Expr:    "is_markdown",
		Workers: 1,
		WithGit: true,
	}
	results, err := search.Walk(context.Background(), opts, contentpkg.DefaultRegistry())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("non-git tree with WithGit=true should still match plain expr; got %d results", len(results))
	}
}

func TestWalk_WithGit_AuthorAndCountPopulated(t *testing.T) {
	root := initRepoForTest(t)
	// Make a second commit on tracked.md so commit_count == 2.
	if err := os.WriteFile(filepath.Join(root, "tracked.md"), []byte("# Tracked v2\n"), 0o644); err != nil {
		t.Fatalf("write v2: %v", err)
	}
	mustGit(t, root, "add", "tracked.md")
	mustGit(t, root, "commit", "-q", "-m", "Update")

	opts := search.Options{
		Root: root,
		// Filter on author + commit_count to confirm both populate.
		Expr:    `is_markdown && git_last_commit_author == "Walker Test" && git_commit_count >= 2`,
		Workers: 1,
		WithGit: true,
	}
	results, err := search.Walk(context.Background(), opts, contentpkg.DefaultRegistry())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected tracked.md (commit_count=2, author='Walker Test'); got %d results", len(results))
	}
}
