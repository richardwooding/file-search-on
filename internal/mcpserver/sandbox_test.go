package mcpserver

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newSandboxedHandlers makes a *handlers with the given roots applied
// via WithSandbox, so tests exercise the same path real callers walk.
func newSandboxedHandlers(t *testing.T, roots ...string) *handlers {
	t.Helper()
	h := &handlers{}
	WithSandbox(roots)(h)
	return h
}

func TestValidatePath_NoSandboxPassthrough(t *testing.T) {
	h := &handlers{} // empty sandbox
	got, err := h.validatePath("/etc/passwd")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/etc/passwd" {
		t.Errorf("got %q, want /etc/passwd", got)
	}
}

func TestValidatePath_AllowsUnderRoot(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "sub", "file.go")
	if err := os.MkdirAll(filepath.Dir(child), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(child, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	h := newSandboxedHandlers(t, root)
	got, err := h.validatePath(child)
	if err != nil {
		t.Fatalf("expected accept, got err: %v", err)
	}
	if got != filepath.Clean(child) {
		t.Errorf("got %q, want %q", got, filepath.Clean(child))
	}
}

func TestValidatePath_AllowsRootItself(t *testing.T) {
	root := t.TempDir()
	h := newSandboxedHandlers(t, root)
	got, err := h.validatePath(root)
	if err != nil {
		t.Errorf("root itself should be allowed, got err: %v", err)
	}
	if got == "" {
		t.Errorf("expected non-empty result, got empty")
	}
}

func TestValidatePath_RejectsOutsideRoot(t *testing.T) {
	root := t.TempDir()
	h := newSandboxedHandlers(t, root)
	_, err := h.validatePath("/etc/passwd")
	if err == nil {
		t.Fatal("expected reject, got nil")
	}
	if !strings.Contains(err.Error(), "sandbox") {
		t.Errorf("error should mention sandbox, got %v", err)
	}
}

func TestValidatePath_RejectsDotDotEscape(t *testing.T) {
	root := t.TempDir()
	// /tmp/xxx/../../../etc/passwd — resolves outside root via lexical Clean.
	escape := filepath.Join(root, "..", "..", "..", "etc", "passwd")
	h := newSandboxedHandlers(t, root)
	_, err := h.validatePath(escape)
	if err == nil {
		t.Fatalf("expected reject for dot-dot escape %q, got nil", escape)
	}
}

func TestValidatePath_RejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	// Create a symlink inside the sandbox pointing OUTSIDE it (to /etc).
	linkPath := filepath.Join(root, "sneaky")
	if err := os.Symlink("/etc", linkPath); err != nil {
		t.Skipf("cannot create symlink (filesystem may not support): %v", err)
	}

	h := newSandboxedHandlers(t, root)
	_, err := h.validatePath(linkPath)
	if err == nil {
		t.Fatal("expected reject for symlink pointing outside root, got nil")
	}
}

func TestValidatePath_AllowsSymlinkInsideRoot(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.txt")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	h := newSandboxedHandlers(t, root)
	if _, err := h.validatePath(link); err != nil {
		t.Errorf("symlink inside root should be allowed, got err: %v", err)
	}
}

func TestValidatePath_PrefixCollision(t *testing.T) {
	// Two TempDirs that happen to share a prefix would be ideal but
	// t.TempDir uses unique names. Build the collision manually under
	// a parent TempDir.
	parent := t.TempDir()
	root := filepath.Join(parent, "foo")
	collider := filepath.Join(parent, "foo-bar", "file.go")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(collider), 0o755); err != nil {
		t.Fatalf("mkdir collider parent: %v", err)
	}

	h := newSandboxedHandlers(t, root)
	_, err := h.validatePath(collider)
	if err == nil {
		t.Fatalf("expected reject for prefix-collision path %q under root %q", collider, root)
	}
}

func TestValidatePath_MultiRoot(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	h := newSandboxedHandlers(t, rootA, rootB)

	// Both roots accept their own children.
	if _, err := h.validatePath(filepath.Join(rootA, "x")); err != nil {
		t.Errorf("rootA child should be allowed: %v", err)
	}
	if _, err := h.validatePath(filepath.Join(rootB, "y")); err != nil {
		t.Errorf("rootB child should be allowed: %v", err)
	}
	// A third unrelated path is rejected.
	if _, err := h.validatePath("/etc/passwd"); err == nil {
		t.Errorf("expected reject for unrelated path")
	}
}

func TestValidatePath_NonExistentPathStillEvaluated(t *testing.T) {
	root := t.TempDir()
	h := newSandboxedHandlers(t, root)
	// Path doesn't exist but is lexically under root → allowed.
	// EvalSymlinks errors silently; lexical Abs+Clean is enough.
	if _, err := h.validatePath(filepath.Join(root, "does", "not", "exist.txt")); err != nil {
		t.Errorf("non-existent under-root path should still be accepted (walker errors later): %v", err)
	}
	// Lexically outside → rejected even if it doesn't exist.
	if _, err := h.validatePath("/tmp/this/does/not/exist.txt"); err == nil {
		t.Errorf("non-existent outside-root path should be rejected at validate time")
	}
}

func TestValidatePath_EmptyInputPassthrough(t *testing.T) {
	root := t.TempDir()
	h := newSandboxedHandlers(t, root)
	got, err := h.validatePath("")
	if err != nil {
		t.Errorf("empty input should pass through (handler default applies), got err: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestValidatePath_TrailingSlashRoot(t *testing.T) {
	tmp := t.TempDir()
	rootWithSlash := tmp + string(filepath.Separator)
	h := newSandboxedHandlers(t, rootWithSlash)

	// WithSandbox calls filepath.Clean on the input; the canonical root
	// should be the no-slash form, and a child under it must be allowed.
	if _, err := h.validatePath(filepath.Join(tmp, "child")); err != nil {
		t.Errorf("child should be allowed despite trailing slash on root: %v", err)
	}
}

func TestValidatePaths_FirstViolationFails(t *testing.T) {
	root := t.TempDir()
	h := newSandboxedHandlers(t, root)
	_, err := h.validatePaths([]string{filepath.Join(root, "a"), "/etc/passwd"})
	if err == nil {
		t.Fatal("expected error on second path, got nil")
	}
}

func TestValidatePaths_AllowAll(t *testing.T) {
	root := t.TempDir()
	h := newSandboxedHandlers(t, root)
	got, err := h.validatePaths([]string{
		filepath.Join(root, "a"),
		filepath.Join(root, "b"),
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
}

func TestValidatePaths_Passthrough(t *testing.T) {
	// Empty sandbox → input returned unchanged (even paths that would
	// be rejected if sandbox were active).
	h := &handlers{}
	got, err := h.validatePaths([]string{"/etc/passwd", "/var/log"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
}

func TestCheckFollowSymlinks_PassthroughWhenSandboxOff(t *testing.T) {
	h := &handlers{}
	if err := h.checkFollowSymlinks(true); err != nil {
		t.Errorf("follow_symlinks should be allowed when sandbox is off: %v", err)
	}
}

func TestCheckFollowSymlinks_AllowsFalseUnderSandbox(t *testing.T) {
	h := newSandboxedHandlers(t, t.TempDir())
	if err := h.checkFollowSymlinks(false); err != nil {
		t.Errorf("follow_symlinks=false should be allowed under sandbox: %v", err)
	}
}

func TestCheckFollowSymlinks_RejectsTrueUnderSandbox(t *testing.T) {
	h := newSandboxedHandlers(t, t.TempDir())
	err := h.checkFollowSymlinks(true)
	if !errors.Is(err, errSandboxFollowSymlinksUnsupported) {
		t.Errorf("expected errSandboxFollowSymlinksUnsupported, got %v", err)
	}
}

func TestWithSandbox_DropsEmptyAndBadAbs(t *testing.T) {
	h := &handlers{}
	WithSandbox([]string{"", "/tmp/valid"})(h)
	if len(h.sandbox) != 1 {
		t.Fatalf("len = %d, want 1 (empty string dropped)", len(h.sandbox))
	}
	if h.sandbox[0] != "/tmp/valid" {
		t.Errorf("got %q, want /tmp/valid", h.sandbox[0])
	}
}

func TestWithSandbox_NilLeavesSandboxEmpty(t *testing.T) {
	h := &handlers{}
	WithSandbox(nil)(h)
	if len(h.sandbox) != 0 {
		t.Errorf("nil roots should leave sandbox empty, got %v", h.sandbox)
	}
}

func TestPathUnder_ExactMatch(t *testing.T) {
	if !pathUnder("/a/b", "/a/b") {
		t.Errorf("exact match should be 'under'")
	}
}

func TestPathUnder_SeparatorAware(t *testing.T) {
	if pathUnder("/a/foo-bar", "/a/foo") {
		t.Errorf("/a/foo-bar should NOT be under /a/foo (prefix collision)")
	}
	if !pathUnder("/a/foo/x", "/a/foo") {
		t.Errorf("/a/foo/x should be under /a/foo")
	}
}
