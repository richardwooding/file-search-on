package goresolve

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// writeModule lays down a tiny self-contained Go module (no external deps,
// so go/packages loads it offline) and returns its root.
func writeModule(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	files["go.mod"] = "module example.test\n\ngo 1.26\n"
	for rel, body := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func qualifiedDead(t *testing.T, dir string) []string {
	t.Helper()
	res, ok, err := Resolve(t.Context(), dir)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !ok {
		t.Skip("type resolution unavailable (no go toolchain); skipping")
	}
	var out []string
	for _, d := range res.DeadFuncs() {
		out = append(out, d.Qualified())
	}
	slices.Sort(out)
	return out
}

// TestResolve_CrossPackageUsageCounted: a method used only from another
// package must NOT be reported dead — the case intra-package resolution
// would get wrong and the precision name-based matching can't be sure of.
func TestResolve_CrossPackageUsageCounted(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"a/a.go": "package a\n\ntype A struct{}\n\nfunc (x A) Used() {}\nfunc (x A) Dead() {}\n",
		"b/b.go": "package b\n\nimport \"example.test/a\"\n\nfunc Run() { var v a.A; v.Used() }\n",
	})
	dead := qualifiedDead(t, dir)
	if slices.Contains(dead, "A.Used") {
		t.Errorf("A.Used is called cross-package; must not be dead. dead=%v", dead)
	}
	if !slices.Contains(dead, "A.Dead") {
		t.Errorf("A.Dead is never called; should be dead. dead=%v", dead)
	}
}

// TestResolve_SameNameMethodsDisambiguated: two methods named Foo on
// different types — only the called one is alive. Name-based matching keeps
// both alive (a call to bare "Foo" covers both); type resolution doesn't.
func TestResolve_SameNameMethodsDisambiguated(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"a/a.go": "package a\n\ntype A struct{}\ntype B struct{}\n\nfunc (A) Foo() {}\nfunc (B) Foo() {}\n\nfunc Use() { var x A; x.Foo() }\n",
	})
	dead := qualifiedDead(t, dir)
	if slices.Contains(dead, "A.Foo") {
		t.Errorf("A.Foo is called; must not be dead. dead=%v", dead)
	}
	if !slices.Contains(dead, "B.Foo") {
		t.Errorf("B.Foo is never called; should be dead (name-based can't tell). dead=%v", dead)
	}
}
