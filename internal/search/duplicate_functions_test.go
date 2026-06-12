package search_test

import (
	"path/filepath"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/index"
	"github.com/richardwooding/file-search-on/internal/search"
)

func TestFindDuplicateFunctions(t *testing.T) {
	dir := t.TempDir()

	// A non-trivial function, copied verbatim into two files.
	dup := "func process(items []int) int {\n" +
		"\ttotal := 0\n" +
		"\tfor _, x := range items {\n" +
		"\t\ttotal += x * 2\n" +
		"\t}\n" +
		"\treturn total\n" +
		"}\n"
	mustWriteFile(t, filepath.Join(dir, "a.go"), "package a\n\n"+dup)
	mustWriteFile(t, filepath.Join(dir, "b.go"), "package b\n\n"+dup)
	// A genuinely different function — should NOT cluster.
	mustWriteFile(t, filepath.Join(dir, "c.go"), "package c\n\n"+
		"func greet(name string) string {\n"+
		"\tmsg := \"hello, \" + name\n"+
		"\tmsg += \" — welcome\"\n"+
		"\tmsg += \"!\"\n"+
		"\treturn msg\n"+
		"}\n")
	// Below the 5-line floor — must be filtered out, not scanned.
	mustWriteFile(t, filepath.Join(dir, "d.go"), "package d\n\nfunc tiny() int { return 1 }\n")

	idx := index.NewMemory()
	defer func() { _ = idx.Close() }()

	dups, err := search.FindDuplicateFunctions(t.Context(), search.Options{
		Roots: []string{dir},
		Index: idx,
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("FindDuplicateFunctions: %v", err)
	}

	if dups.FunctionsScanned != 3 { // process ×2 + greet; tiny filtered
		t.Errorf("FunctionsScanned = %d, want 3 (tiny() should be filtered by min_lines)", dups.FunctionsScanned)
	}
	if dups.GroupCount != 1 {
		t.Fatalf("GroupCount = %d, want 1: %+v", dups.GroupCount, dups.Groups)
	}
	g := dups.Groups[0]
	if g.Count != 2 {
		t.Fatalf("group has %d members, want 2: %+v", g.Count, g.Members)
	}
	for _, m := range g.Members {
		if m.Symbol != "process" {
			t.Errorf("clustered the wrong function: %q (expected only 'process')", m.Symbol)
		}
		if m.Lines != 7 {
			t.Errorf("member %s lines = %d, want 7", m.Symbol, m.Lines)
		}
	}
}

func TestFindDuplicateFunctions_NoDuplicates(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "x.go"), "package x\n\n"+
		"func alpha() int {\n\ta := 1\n\tb := 2\n\tc := 3\n\treturn a + b + c\n}\n"+
		"func beta(s string) int {\n\tn := len(s)\n\tfor range s {\n\t\tn--\n\t}\n\treturn n\n}\n")
	idx := index.NewMemory()
	defer func() { _ = idx.Close() }()

	dups, err := search.FindDuplicateFunctions(t.Context(), search.Options{Roots: []string{dir}, Index: idx}, content.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	if dups.GroupCount != 0 {
		t.Errorf("distinct functions should not cluster, got %d group(s): %+v", dups.GroupCount, dups.Groups)
	}
}
