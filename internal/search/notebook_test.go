package search_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// TestWalker_IsNotebookFilter verifies the is_notebook CEL predicate
// fires for both Jupyter (.ipynb) and Zeppelin (.zpln) files, and
// that the per-notebook attributes (cell_count, kernel) flow into
// the CEL activation.
func TestWalker_IsNotebookFilter(t *testing.T) {
	dir := t.TempDir()
	jup := `{
  "nbformat": 4,
  "metadata": {"kernelspec": {"name": "python3", "language": "python"}},
  "cells": [
    {"cell_type": "markdown", "source": "h"},
    {"cell_type": "code", "source": "x = 1"},
    {"cell_type": "code", "source": "y = 2"}
  ]
}`
	zpl := `{
  "name": "Recon",
  "defaultInterpreterGroup": "spark",
  "paragraphs": [
    {"text": "%md\n# Hi", "config": {"editorSetting": {"language": "markdown"}}},
    {"text": "%spark\nval x = 1", "config": {"editorSetting": {"language": "scala"}}}
  ]
}`
	for n, body := range map[string]string{"a.ipynb": jup, "b.zpln": zpl} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Also drop in a plain markdown file to confirm it's excluded.
	if err := os.WriteFile(filepath.Join(dir, "c.md"), []byte("# md\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	results, err := search.Walk(t.Context(), search.Options{
		Root: dir,
		Expr: "is_notebook",
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d notebook matches, want 2", len(results))
	}
}

// TestWalker_NotebookCellCount verifies cell_count is reachable from
// CEL and filters correctly.
func TestWalker_NotebookCellCount(t *testing.T) {
	dir := t.TempDir()
	big := `{
  "nbformat": 4,
  "cells": [{"cell_type":"code"},{"cell_type":"code"},{"cell_type":"code"},{"cell_type":"code"},{"cell_type":"code"}]
}`
	small := `{"nbformat": 4, "cells": [{"cell_type":"code"}]}`
	if err := os.WriteFile(filepath.Join(dir, "big.ipynb"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "small.ipynb"), []byte(small), 0o644); err != nil {
		t.Fatal(err)
	}

	results, err := search.Walk(t.Context(), search.Options{
		Root: dir,
		Expr: "is_notebook && cell_count > 3",
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d matches, want 1 (big.ipynb only)", len(results))
	}
	if !strings.HasSuffix(results[0].Path, "big.ipynb") {
		t.Errorf("got %s, want big.ipynb", results[0].Path)
	}
}

// TestWalker_NotebookKernelFilter verifies the kernel attribute is
// reachable from CEL.
func TestWalker_NotebookKernelFilter(t *testing.T) {
	dir := t.TempDir()
	py := `{
  "nbformat": 4,
  "metadata": {"kernelspec": {"name": "python3", "language": "python"}},
  "cells": [{"cell_type": "code"}]
}`
	r := `{
  "nbformat": 4,
  "metadata": {"kernelspec": {"name": "ir", "language": "R"}},
  "cells": [{"cell_type": "code"}]
}`
	if err := os.WriteFile(filepath.Join(dir, "py.ipynb"), []byte(py), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "r.ipynb"), []byte(r), 0o644); err != nil {
		t.Fatal(err)
	}

	results, err := search.Walk(t.Context(), search.Options{
		Root: dir,
		Expr: `is_notebook && kernel == "python3"`,
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d matches, want 1 (python3 only)", len(results))
	}
}
