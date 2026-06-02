package gitmeta

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// initRepo creates a fresh git repo in a t.TempDir and returns its
// root. Sets a deterministic identity so commits don't depend on the
// running user's git config. Skips the test when the git binary isn't
// available (CI image without git installed).
func initRepo(t *testing.T) string {
	t.Helper()
	if !HasGitBinary() {
		t.Skip("git binary not on PATH; skipping gitmeta integration test")
	}
	root := t.TempDir()
	runOrSkip(t, root, "init", "-q", "-b", "main")
	runOrSkip(t, root, "config", "user.email", "test@example.com")
	runOrSkip(t, root, "config", "user.name", "Test User")
	runOrSkip(t, root, "config", "commit.gpgsign", "false")
	return root
}

func runOrSkip(t *testing.T, root string, args ...string) {
	t.Helper()
	full := append([]string{"-C", root}, args...)
	cmd := exec.Command("git", full...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeAndCommit(t *testing.T, root, relpath, content, msg string) {
	t.Helper()
	full := filepath.Join(root, relpath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	runOrSkip(t, root, "add", relpath)
	runOrSkip(t, root, "commit", "-q", "-m", msg)
}

// --- New + Lookup ---

func TestNew_SingleCommitPopulatesLookup(t *testing.T) {
	root := initRepo(t)
	writeAndCommit(t, root, "hello.txt", "hi\n", "Add hello")

	c, err := New(context.Background(), root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c == nil {
		t.Fatal("Cache is nil for a real repo")
	}
	if c.RepoRoot() == "" {
		t.Error("RepoRoot empty")
	}
	if c.HeadSHA() == "" {
		t.Error("HeadSHA empty")
	}

	absPath := filepath.Join(root, "hello.txt")
	info, ok := c.Lookup(absPath)
	if !ok {
		t.Fatalf("Lookup(%s) returned ok=false; want true", absPath)
	}
	if info.CommitCount != 1 {
		t.Errorf("CommitCount = %d, want 1", info.CommitCount)
	}
	if info.LastCommitAuthor != "Test User" {
		t.Errorf("LastCommitAuthor = %q, want Test User", info.LastCommitAuthor)
	}
	if info.LastCommitSubject != "Add hello" {
		t.Errorf("LastCommitSubject = %q, want Add hello", info.LastCommitSubject)
	}
	if info.LastCommitTime.IsZero() {
		t.Errorf("LastCommitTime is zero")
	}
	if !info.FirstSeen.Equal(info.LastCommitTime) {
		t.Errorf("FirstSeen %v != LastCommitTime %v (single commit)", info.FirstSeen, info.LastCommitTime)
	}
}

func TestNew_MultipleCommitsAccumulate(t *testing.T) {
	root := initRepo(t)
	writeAndCommit(t, root, "doc.md", "v1\n", "Initial draft")
	// Sleep so commit timestamps are reliably distinct at 1-second granularity.
	time.Sleep(1100 * time.Millisecond)
	writeAndCommit(t, root, "doc.md", "v2\n", "Edit pass")
	time.Sleep(1100 * time.Millisecond)
	writeAndCommit(t, root, "doc.md", "v3\n", "Final pass")

	c, err := New(context.Background(), root)
	if err != nil || c == nil {
		t.Fatalf("New: c=%v err=%v", c, err)
	}
	info, ok := c.Lookup(filepath.Join(root, "doc.md"))
	if !ok {
		t.Fatal("Lookup failed")
	}
	if info.CommitCount != 3 {
		t.Errorf("CommitCount = %d, want 3", info.CommitCount)
	}
	if info.LastCommitSubject != "Final pass" {
		t.Errorf("LastCommitSubject = %q, want Final pass", info.LastCommitSubject)
	}
	if !info.FirstSeen.Before(info.LastCommitTime) {
		t.Errorf("FirstSeen %v should be before LastCommitTime %v", info.FirstSeen, info.LastCommitTime)
	}
}

func TestNew_NonGitTreeReturnsNil(t *testing.T) {
	root := t.TempDir() // no git init
	c, err := New(context.Background(), root)
	if err != nil {
		t.Fatalf("expected nil err for non-git tree, got %v", err)
	}
	if c != nil {
		t.Errorf("expected nil Cache for non-git tree, got %+v", c)
	}
}

// --- IsTracked + IsIgnored ---

func TestIsTracked_OnlyForIndexedFiles(t *testing.T) {
	root := initRepo(t)
	writeAndCommit(t, root, "tracked.txt", "in\n", "Add tracked")
	// Untracked file.
	if err := os.WriteFile(filepath.Join(root, "untracked.txt"), []byte("out\n"), 0o644); err != nil {
		t.Fatalf("write untracked: %v", err)
	}

	c, _ := New(context.Background(), root)
	if !c.IsTracked(filepath.Join(root, "tracked.txt")) {
		t.Error("tracked.txt should be IsTracked")
	}
	if c.IsTracked(filepath.Join(root, "untracked.txt")) {
		t.Error("untracked.txt should NOT be IsTracked")
	}
}

func TestIsIgnored_MatchesGitignore(t *testing.T) {
	root := initRepo(t)
	// Add a .gitignore that excludes *.log, then commit a file that
	// is NOT in the index but matches the ignore rule.
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("*.log\n"), 0o644); err != nil {
		t.Fatalf("write gitignore: %v", err)
	}
	runOrSkip(t, root, "add", ".gitignore")
	runOrSkip(t, root, "commit", "-q", "-m", "Add gitignore")
	if err := os.WriteFile(filepath.Join(root, "build.log"), []byte("noise\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	c, _ := New(context.Background(), root)
	if !c.IsIgnored(filepath.Join(root, "build.log")) {
		t.Error("build.log should be IsIgnored")
	}
	if c.IsIgnored(filepath.Join(root, ".gitignore")) {
		t.Error(".gitignore should NOT be IsIgnored (it's in the index)")
	}
}

// --- Cancellation ---

func TestNew_ContextCancelPropagates(t *testing.T) {
	root := initRepo(t)
	writeAndCommit(t, root, "a.txt", "x\n", "Add")
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel
	c, err := New(ctx, root)
	if err == nil {
		// On extremely fast machines New can complete before noticing
		// ctx was cancelled. Accept both shapes: either err is set, or
		// the cache is nil (the silent-skip path that swallows the
		// rev-parse error caused by ctx cancellation).
		if c != nil {
			t.Fatal("expected nil cache or error on pre-cancelled ctx")
		}
	}
}

// --- Outside-repo path ---

func TestLookup_PathOutsideRepoReturnsNotOk(t *testing.T) {
	root := initRepo(t)
	writeAndCommit(t, root, "in.txt", "x\n", "Add")
	c, _ := New(context.Background(), root)

	other := t.TempDir() // unrelated dir
	if _, ok := c.Lookup(filepath.Join(other, "in.txt")); ok {
		t.Error("Lookup on path outside repo should return ok=false")
	}
}

// --- Nil cache safe ---

func TestNilCache_AllMethodsSafe(t *testing.T) {
	var c *Cache
	if _, ok := c.Lookup("/anything"); ok {
		t.Error("nil Cache Lookup should return ok=false")
	}
	if c.IsTracked("/anything") {
		t.Error("nil Cache IsTracked should return false")
	}
	if c.IsIgnored("/anything") {
		t.Error("nil Cache IsIgnored should return false")
	}
}
