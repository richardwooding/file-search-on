package search

import "testing"

func TestResolveRelativePythonImport(t *testing.T) {
	cases := []struct {
		imp, from, want string
		ok              bool
	}{
		{".", "a.b", "a.b", true},                        // current package
		{".sub", "a.b", "a.b.sub", true},                 // submodule / subpackage
		{".ctx", "flask", "flask.ctx", true},             // within a top-level package
		{"..util", "a.b", "a.util", true},                // parent's sibling
		{"..", "a.b", "a", true},                         // parent package
		{"..x", "flask", "x", true},                      // sibling of a top-level package
		{"...deep", "a.b", "deep", true},                 // 2 levels up from a.b = root level → top-level "deep"
		{"...deep", "a", "", false},                      // climbs above the import root
		{".svc.service", "a.b", "a.b.svc.service", true}, // dotted remainder
	}
	for _, c := range cases {
		got, ok := resolveRelativePythonImport(c.imp, c.from)
		if got != c.want || ok != c.ok {
			t.Errorf("resolveRelativePythonImport(%q, %q) = (%q, %v), want (%q, %v)",
				c.imp, c.from, got, ok, c.want, c.ok)
		}
	}
}
