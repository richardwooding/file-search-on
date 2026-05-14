package mcpserver

import (
	"path/filepath"
	"testing"
)

// TestExpandHomeDir covers the behaviour matrix from the plan: leading
// ~ / ~/foo expand, everything else (~user, absolute, relative, empty,
// mid-string ~) returns unchanged.
func TestExpandHomeDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	// Windows fallback — os.UserHomeDir reads USERPROFILE on Windows.
	t.Setenv("USERPROFILE", tmp)

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty stays empty", "", ""},
		{"bare tilde → home", "~", tmp},
		{"tilde slash foo", "~/Code", filepath.Join(tmp, "Code")},
		{"tilde slash deep", "~/a/b/c.txt", filepath.Join(tmp, "a", "b", "c.txt")},
		{"~user form unchanged", "~alice/foo", "~alice/foo"},
		{"absolute unchanged", "/abs/path", "/abs/path"},
		{"relative dot unchanged", ".", "."},
		{"relative dotted unchanged", "./rel/path", "./rel/path"},
		{"bare name unchanged", "foo", "foo"},
		{"mid-path tilde unchanged", "/foo/~/bar", "/foo/~/bar"},
		// Defensive: a path that happens to *contain* "~" but not as the
		// first byte must be left alone.
		{"tilde-suffix unchanged", "foo~bar", "foo~bar"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := expandHomeDir(tc.in)
			if err != nil {
				t.Fatalf("expandHomeDir(%q) err=%v want nil", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("expandHomeDir(%q)=%q want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestExpandHomeDirs verifies the slice wrapper expands every entry and
// preserves order.
func TestExpandHomeDirs(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)

	t.Run("nil stays nil", func(t *testing.T) {
		out, err := expandHomeDirs(nil)
		if err != nil {
			t.Fatalf("err=%v", err)
		}
		if out != nil {
			t.Errorf("got %+v want nil", out)
		}
	})

	t.Run("mixed entries", func(t *testing.T) {
		in := []string{"~/a", "/abs/b", "rel/c", "~"}
		want := []string{filepath.Join(tmp, "a"), "/abs/b", "rel/c", tmp}
		out, err := expandHomeDirs(in)
		if err != nil {
			t.Fatalf("err=%v", err)
		}
		if len(out) != len(want) {
			t.Fatalf("len(out)=%d want %d", len(out), len(want))
		}
		for i := range want {
			if out[i] != want[i] {
				t.Errorf("out[%d]=%q want %q", i, out[i], want[i])
			}
		}
	})

	t.Run("input is not mutated", func(t *testing.T) {
		in := []string{"~/preserve"}
		_, _ = expandHomeDirs(in)
		if in[0] != "~/preserve" {
			t.Errorf("expandHomeDirs mutated input: in[0]=%q want \"~/preserve\"", in[0])
		}
	})
}
