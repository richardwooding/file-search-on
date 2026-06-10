package search_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// buildGraphFixture lays down a small Go tree with known import and
// definition edges:
//
//	a/a.go — imports fmt + strings; defines type Widget, func Alpha
//	b/b.go — imports fmt;          defines type Gadget, func Alpha, func Beta
//	c/c.go — no imports;           defines func Gamma
//
// "fmt" is imported by two files; "Alpha" is defined in two files.
func buildGraphFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a/a.go", "package a\n\nimport (\n\t\"fmt\"\n\t\"strings\"\n)\n\ntype Widget struct{}\n\nfunc Alpha() { fmt.Println(strings.ToUpper(\"x\")) }\n")
	write("b/b.go", "package b\n\nimport \"fmt\"\n\ntype Gadget struct{}\n\nfunc Alpha() {}\nfunc Beta()  { fmt.Println(\"b\") }\n")
	write("c/c.go", "package c\n\nfunc Gamma() {}\n")
	return dir
}

func mustBuildGraph(t *testing.T, dir string) *search.CodeGraph {
	t.Helper()
	g, err := search.BuildCodeGraph(t.Context(), search.Options{
		Root: dir,
		Expr: `is_source && language == "go"`,
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("BuildCodeGraph: %v", err)
	}
	if g.Cancelled {
		t.Fatalf("unexpected cancellation: %s", g.CancellationReason)
	}
	return g
}

func importerPaths(imps []search.Importer) []string {
	out := make([]string, len(imps))
	for i, im := range imps {
		out[i] = filepath.Base(filepath.Dir(im.Path)) + "/" + filepath.Base(im.Path)
	}
	return out
}

func TestBuildCodeGraph_TotalFiles(t *testing.T) {
	g := mustBuildGraph(t, buildGraphFixture(t))
	if g.TotalFiles != 3 {
		t.Errorf("TotalFiles=%d want 3", g.TotalFiles)
	}
}

func TestCodeGraph_ImportedBy_Exact(t *testing.T) {
	g := mustBuildGraph(t, buildGraphFixture(t))
	imps, err := g.ImportedBy("fmt", "exact")
	if err != nil {
		t.Fatal(err)
	}
	got := importerPaths(imps)
	want := []string{"a/a.go", "b/b.go"}
	if len(got) != len(want) {
		t.Fatalf("ImportedBy(fmt)=%v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ImportedBy(fmt)[%d]=%q want %q (%v)", i, got[i], want[i], got)
		}
	}
	for _, im := range imps {
		if im.Language != "go" {
			t.Errorf("importer language=%q want go", im.Language)
		}
	}
}

func TestCodeGraph_ImportedBy_Prefix(t *testing.T) {
	g := mustBuildGraph(t, buildGraphFixture(t))
	imps, err := g.ImportedBy("str", "prefix") // matches "strings"
	if err != nil {
		t.Fatal(err)
	}
	got := importerPaths(imps)
	if len(got) != 1 || got[0] != "a/a.go" {
		t.Errorf("ImportedBy(str, prefix)=%v want [a/a.go]", got)
	}
}

func TestCodeGraph_ImportedBy_Regex(t *testing.T) {
	g := mustBuildGraph(t, buildGraphFixture(t))
	imps, err := g.ImportedBy("^(fmt|strings)$", "regex")
	if err != nil {
		t.Fatal(err)
	}
	// fmt → a,b ; strings → a ; deduped → a,b.
	if got := importerPaths(imps); len(got) != 2 {
		t.Errorf("ImportedBy(regex)=%v want 2 deduped importers", got)
	}
}

func TestCodeGraph_ImportedBy_BadMode(t *testing.T) {
	g := mustBuildGraph(t, buildGraphFixture(t))
	if _, err := g.ImportedBy("fmt", "bogus"); err == nil {
		t.Error("expected error for unknown mode")
	}
	if _, err := g.ImportedBy("(", "regex"); err == nil {
		t.Error("expected error for invalid regex")
	}
}

func TestCodeGraph_FindDefinition(t *testing.T) {
	g := mustBuildGraph(t, buildGraphFixture(t))

	alpha := g.FindDefinition("Alpha", "function")
	if len(alpha) != 2 {
		t.Fatalf("FindDefinition(Alpha, function)=%d defs want 2: %+v", len(alpha), alpha)
	}
	for _, d := range alpha {
		if d.Kind != "function" {
			t.Errorf("kind=%q want function", d.Kind)
		}
	}

	widget := g.FindDefinition("Widget", "")
	if len(widget) != 1 || widget[0].Kind != "type" {
		t.Fatalf("FindDefinition(Widget)=%+v want one type def", widget)
	}

	// kind filter excludes the type when asking for functions.
	if got := g.FindDefinition("Widget", "function"); len(got) != 0 {
		t.Errorf("FindDefinition(Widget, function)=%+v want empty", got)
	}

	if got := g.FindDefinition("DoesNotExist", ""); len(got) != 0 {
		t.Errorf("FindDefinition(missing)=%+v want empty", got)
	}
}

// TestCodeGraph_FindDefinition_DedupesSameFile guards the (path, kind)
// dedup: a file defining the same function name twice (two Go methods
// named String on different receivers) must return its path once.
func TestCodeGraph_FindDefinition_DedupesSameFile(t *testing.T) {
	dir := t.TempDir()
	src := "package p\n\ntype A struct{}\ntype B struct{}\n\nfunc (A) String() string { return \"a\" }\nfunc (B) String() string { return \"b\" }\n"
	if err := os.WriteFile(filepath.Join(dir, "p.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	g := mustBuildGraph(t, dir)
	defs := g.FindDefinition("String", "function")
	if len(defs) != 1 {
		t.Fatalf("FindDefinition(String) returned %d defs, want 1 (deduped): %+v", len(defs), defs)
	}
}

// buildCallFixture has clear intra-repo call edges:
//
//	a.go    — func Helper(); func Used() { Helper() }   (Helper is called)
//	b.go    — func Orphan()                              (never called)
//	main.go — func main() { Used() }                     (calls Used)
func buildCallFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mk := func(rel, body string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("a/a.go", "package a\n\nfunc Helper() {}\nfunc Used() { Helper() }\n")
	mk("b/b.go", "package b\n\nfunc Orphan() {}\n")
	mk("m/main.go", "package main\n\nfunc main() { Used() }\n")
	return dir
}

func TestCodeGraph_WhoCalls(t *testing.T) {
	g := mustBuildGraph(t, buildCallFixture(t))
	callers := g.WhoCalls("Helper")
	if len(callers) != 1 || filepath.Base(callers[0].Path) != "a.go" {
		t.Fatalf("WhoCalls(Helper)=%+v want [a/a.go]", callers)
	}
	if used := g.WhoCalls("Used"); len(used) != 1 || filepath.Base(used[0].Path) != "main.go" {
		t.Fatalf("WhoCalls(Used)=%+v want [m/main.go]", used)
	}
	if none := g.WhoCalls("Orphan"); len(none) != 0 {
		t.Errorf("WhoCalls(Orphan)=%+v want empty", none)
	}
}

func TestCodeGraph_Calls(t *testing.T) {
	g := mustBuildGraph(t, buildCallFixture(t))
	// Used() { Helper() } → Used calls Helper.
	if got := g.Calls("Used"); len(got) != 1 || got[0] != "Helper" {
		t.Fatalf("Calls(Used)=%v want [Helper]", got)
	}
	// main() { Used() } → main calls Used.
	if got := g.Calls("main"); len(got) != 1 || got[0] != "Used" {
		t.Fatalf("Calls(main)=%v want [Used]", got)
	}
	// Helper() {} calls nothing.
	if got := g.Calls("Helper"); len(got) != 0 {
		t.Errorf("Calls(Helper)=%v want empty", got)
	}
	if got := g.Calls("DoesNotExist"); len(got) != 0 {
		t.Errorf("Calls(missing)=%v want empty", got)
	}
}

func TestCodeGraph_DeadCode(t *testing.T) {
	g := mustBuildGraph(t, buildCallFixture(t))
	dead := map[string]bool{}
	for _, d := range g.DeadCode() {
		dead[d.Symbol] = true
	}
	if !dead["Orphan"] {
		t.Errorf("DeadCode should flag Orphan: %v", dead)
	}
	if dead["Helper"] {
		t.Error("DeadCode wrongly flagged Helper (it's called by Used)")
	}
	if dead["Used"] {
		t.Error("DeadCode wrongly flagged Used (it's called by main)")
	}
}

// TestCodeGraph_DeadCode_SkipsNonRefLanguages ensures a definition in a
// language without reference extraction (e.g. Python) is NOT reported as
// dead — we don't scan those for calls, so everything would look unused.
func TestCodeGraph_DeadCode_SkipsNonRefLanguages(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.py"), []byte("def lonely():\n    pass\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	g := mustBuildGraph(t, dir)
	for _, d := range g.DeadCode() {
		if d.Symbol == "lonely" {
			t.Errorf("DeadCode flagged a Python symbol (no ref extraction): %+v", d)
		}
	}
}

func TestCodeGraph_Overview(t *testing.T) {
	g := mustBuildGraph(t, buildGraphFixture(t))
	ov := g.Overview(10)

	if ov.TotalFiles != 3 {
		t.Errorf("TotalFiles=%d want 3", ov.TotalFiles)
	}
	if ov.DistinctModules != 2 { // fmt, strings
		t.Errorf("DistinctModules=%d want 2", ov.DistinctModules)
	}
	if len(ov.ImportHubs) == 0 || ov.ImportHubs[0].Module != "fmt" || ov.ImportHubs[0].Count != 2 {
		t.Errorf("ImportHubs[0]=%+v want {fmt 2}", ov.ImportHubs)
	}
	if len(ov.Languages) != 1 || ov.Languages[0].Language != "go" || ov.Languages[0].Files != 3 {
		t.Errorf("Languages=%+v want [{go 3}]", ov.Languages)
	}

	var foundAlpha bool
	for _, d := range ov.DuplicateDefs {
		if d.Symbol == "Alpha" && d.Kind == "function" {
			foundAlpha = true
			if len(d.Paths) != 2 {
				t.Errorf("Alpha duplicate paths=%v want 2", d.Paths)
			}
		}
	}
	if !foundAlpha {
		t.Errorf("DuplicateDefs missing Alpha/function: %+v", ov.DuplicateDefs)
	}
}
