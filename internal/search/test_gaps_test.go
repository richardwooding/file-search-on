package search_test

import (
	"path/filepath"
	"testing"

	"github.com/richardwooding/file-search-on/internal/search"
)

func TestCodeGraph_TestGaps(t *testing.T) {
	dir := t.TempDir()
	// prod.go: Tested is called from a test; Untested is not.
	mustWriteFile(t, filepath.Join(dir, "prod.go"),
		"package p\n\nfunc Tested() {}\nfunc Untested() {}\n")
	mustWriteFile(t, filepath.Join(dir, "prod_test.go"),
		"package p\n\nimport \"testing\"\n\nfunc TestTested(t *testing.T) { Tested() }\n")
	// orphan.go: nothing references Lonely — fully untested.
	mustWriteFile(t, filepath.Join(dir, "orphan.go"),
		"package p\n\nfunc Lonely() {}\n")

	g := mustBuildGraph(t, dir)
	byPath := map[string]search.TestGap{}
	for _, gp := range g.TestGaps() {
		byPath[filepath.Base(gp.Path)] = gp
	}

	prod, ok := byPath["prod.go"]
	if !ok {
		t.Fatalf("prod.go should be a gap (Untested is never tested): %+v", byPath)
	}
	if prod.FunctionCount != 2 || prod.UntestedCount != 1 || prod.FullyUntested {
		t.Errorf("prod.go gap = %+v, want function_count 2, untested 1, not fully", prod)
	}
	if len(prod.UntestedFunctions) != 1 || prod.UntestedFunctions[0] != "Untested" {
		t.Errorf("prod.go untested = %v, want [Untested] (Tested is referenced from a test)", prod.UntestedFunctions)
	}

	orphan, ok := byPath["orphan.go"]
	if !ok || !orphan.FullyUntested {
		t.Errorf("orphan.go should be a fully-untested gap: %+v", orphan)
	}

	// The test file itself must never be reported as a gap.
	if _, bad := byPath["prod_test.go"]; bad {
		t.Errorf("a *_test.go file must not be reported as a test gap")
	}
}

// TestCodeGraph_TestGaps_ExcludesRegisteredHandlers pins issue #506: a
// function registered as a VALUE (the AddTool / HandleFunc handler pattern) is
// exercised by tests through the tool, never by name, so the name-based
// "untested" signal is a guaranteed false positive — it must be excluded. A
// normal untested function in the same file must still be reported, and must
// not be dragged into a false fully_untested by the exclusion accounting.
func TestCodeGraph_TestGaps_ExcludesRegisteredHandlers(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "handler.go"),
		"package p\n\nfunc searchHandler() {}\nfunc plainHelper() {}\n")
	// register.go passes searchHandler as a value — the handler pattern.
	mustWriteFile(t, filepath.Join(dir, "register.go"),
		"package p\n\nfunc reg() { AddTool(searchHandler) }\n")

	g := mustBuildGraph(t, dir)
	byPath := map[string]search.TestGap{}
	for _, gp := range g.TestGaps() {
		byPath[filepath.Base(gp.Path)] = gp
	}

	gap, ok := byPath["handler.go"]
	if !ok {
		t.Fatalf("handler.go should still be a gap (plainHelper is untested): %+v", byPath)
	}
	for _, f := range gap.UntestedFunctions {
		if f == "searchHandler" {
			t.Errorf("searchHandler is a value-registered handler — must be excluded (#506): %v", gap.UntestedFunctions)
		}
	}
	if gap.FunctionCount != 1 || gap.UntestedCount != 1 || len(gap.UntestedFunctions) != 1 || gap.UntestedFunctions[0] != "plainHelper" {
		t.Errorf("handler.go gap = %+v, want only plainHelper (handler excluded from count + list)", gap)
	}
}
