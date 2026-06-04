package pathguard

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUnder(t *testing.T) {
	sep := string(filepath.Separator)
	root := filepath.Join(sep+"home", "foo")
	cases := []struct {
		name string
		p    string
		want bool
	}{
		{"exact match", root, true},
		{"direct child", filepath.Join(root, "proj"), true},
		{"deep child", filepath.Join(root, "a", "b", "c"), true},
		{"sibling prefix not under", filepath.Join(sep+"home", "foo-bar"), false},
		{"parent not under", filepath.Join(sep + "home"), false},
		{"unrelated", filepath.Join(sep+"etc", "passwd"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Under(tc.p, root); got != tc.want {
				t.Errorf("Under(%q, %q) = %v, want %v", tc.p, root, got, tc.want)
			}
		})
	}
}

func TestCanonical_RelativeBecomesAbsolute(t *testing.T) {
	got := Canonical(".")
	if !filepath.IsAbs(got) {
		t.Errorf("Canonical(\".\") = %q, want an absolute path", got)
	}
}

func TestCanonical_ResolvesSymlinkedAncestor(t *testing.T) {
	// real/sub exists; link → real. Canonical(link/sub/new) should
	// resolve through the symlink to real/sub/new even though "new"
	// doesn't exist.
	tmp := t.TempDir()
	real := filepath.Join(tmp, "real")
	if err := os.MkdirAll(filepath.Join(real, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(tmp, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	got := Canonical(filepath.Join(link, "sub", "new"))
	want := Canonical(filepath.Join(real, "sub", "new"))
	if got != want {
		t.Errorf("Canonical via symlink = %q, want %q", got, want)
	}
	// And the resolved path is genuinely under the real dir.
	if !Under(got, Canonical(real)) {
		t.Errorf("resolved %q not under %q", got, Canonical(real))
	}
}

func TestCanonical_NonExistentFallsBackLexically(t *testing.T) {
	// A fully non-existent absolute path with no resolvable ancestor
	// (other than the filesystem root) should clean to itself.
	p := filepath.Join(string(filepath.Separator), "nonexistent-xyz-123", "deep", "leaf")
	got := Canonical(p)
	if got != filepath.Clean(p) {
		t.Errorf("Canonical(%q) = %q, want %q", p, got, filepath.Clean(p))
	}
}
