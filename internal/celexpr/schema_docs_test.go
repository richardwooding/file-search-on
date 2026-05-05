package celexpr_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/richardwooding/file-search-on/internal/celexpr"
)

// TestSchemaCoverageInDocs guards against the silent README drift that
// hit v0.10.0: every attribute and function in celexpr.Schema() should
// appear literally in README.md so the human-readable summary stays in
// sync with --list and the MCP list_attributes tool.
//
// If a new attribute fails this check, the fix is to add a row to the
// matching family table in README.md under "Available attributes" —
// see CLAUDE.md's "Adding a new content type" / "Adding a CEL function"
// checklists.
func TestSchemaCoverageInDocs(t *testing.T) {
	root := repoRoot(t)
	readme, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	readmeText := string(readme)

	schema := celexpr.Schema()
	groups := []struct {
		name string
		docs []celexpr.AttributeDoc
	}{
		{"Common", schema.Common},
		{"TypeSpecific", schema.TypeSpecific},
		{"Frontmatter", schema.Frontmatter},
	}
	for _, g := range groups {
		for _, doc := range g.docs {
			if !strings.Contains(readmeText, "`"+doc.Name+"`") {
				t.Errorf("README.md missing %s attribute %q (expected backticked literal `%s`)",
					g.name, doc.Name, doc.Name)
			}
		}
	}
	for _, fn := range schema.Functions {
		if !strings.Contains(readmeText, "`"+fn.Name+"(") &&
			!strings.Contains(readmeText, "`"+fn.Name+"`") {
			t.Errorf("README.md missing function %q (expected `%s(...)` or `%s` literal)",
				fn.Name, fn.Name, fn.Name)
		}
	}
}

// repoRoot walks up from the calling test file to the directory
// containing go.mod. Tests run with cwd set to the package directory,
// so README.md isn't accessible via a relative path; runtime.Caller +
// upward walk is the cleanest way to find the module root.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found walking up from " + file)
		}
		dir = parent
	}
}
