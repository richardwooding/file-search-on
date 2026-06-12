package arch

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// maxImportsPerFile is the import fan-out ceiling for a single first-party Go
// source file (issue #388). It's a TRIPWIRE, not a hard rule: it sits just
// above today's worst offender so silent coupling creep trips it, but a file
// that legitimately needs more imports is fine — bump this by the smallest
// amount with a one-line note in the PR. The point is that crossing it is a
// conscious decision, not accidental.
//
// Current leaders (go test -run TestImportFanOut -v prints the live top-10):
// internal/monitor/server.go (21), internal/celexpr/evaluator.go (17),
// internal/search/walker.go (16). Ceiling = 24 leaves ~3 of headroom.
const maxImportsPerFile = 24

// TestImportFanOut walks the module's own first-party Go source (excluding
// test files, vendor, and testdata) and fails if any file imports more than
// maxImportsPerFile packages — operationalising the #388 watch on fan-out so
// coupling growth surfaces in CI instead of a manual code_graph run.
func TestImportFanOut(t *testing.T) {
	root := moduleRoot(t)
	fset := token.NewFileSet()

	type fileFanOut struct {
		path  string
		count int
	}
	var all []fileFanOut

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "vendor", "testdata", ".git", "dist":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if perr != nil {
			// A file we can't parse isn't this guard's concern (the build
			// would already fail); skip rather than error.
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		all = append(all, fileFanOut{path: rel, count: len(f.Imports)})
		return nil
	})
	if err != nil {
		t.Fatalf("walk module root %s: %v", root, err)
	}
	if len(all) == 0 {
		t.Fatal("no first-party Go files found — module-root detection likely broke")
	}

	sort.Slice(all, func(i, j int) bool {
		if all[i].count != all[j].count {
			return all[i].count > all[j].count
		}
		return all[i].path < all[j].path
	})

	top := all
	if len(top) > 10 {
		top = top[:10]
	}
	for _, f := range top {
		t.Logf("fan-out %2d  %s", f.count, f.path)
	}

	for _, f := range all {
		if f.count > maxImportsPerFile {
			t.Errorf("%s imports %d packages, over the fan-out ceiling of %d (issue #388). "+
				"Split the file by responsibility, or — if the coupling is genuinely warranted — "+
				"raise maxImportsPerFile by the smallest amount with a justification.",
				f.path, f.count, maxImportsPerFile)
		}
	}
}

// moduleRoot walks up from this test's source file to the directory holding
// go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(self)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found walking up from " + self)
		}
		dir = parent
	}
}
