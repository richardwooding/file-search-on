package content_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
)

// TestJupyterAttributes seeds a minimal but realistic .ipynb file
// and asserts the per-notebook attribute extraction.
func TestJupyterAttributes(t *testing.T) {
	dir := t.TempDir()
	body := `{
  "nbformat": 4,
  "nbformat_minor": 5,
  "metadata": {
    "kernelspec": {"name": "python3", "display_name": "Python 3", "language": "python"},
    "language_info": {"name": "python", "version": "3.11.0"}
  },
  "cells": [
    {"cell_type": "markdown", "source": "# Notebook title"},
    {"cell_type": "code", "source": "x = 1"},
    {"cell_type": "code", "source": "print(x)"},
    {"cell_type": "raw", "source": "raw bytes"}
  ]
}`
	path := filepath.Join(dir, "demo.ipynb")
	writeTemp2(t, path, body)

	attrs := notebookAttrs(t, "notebook/jupyter", path)
	if v, _ := attrs["cell_count"].(int64); v != 4 {
		t.Errorf("cell_count=%v want 4", attrs["cell_count"])
	}
	if v, _ := attrs["code_cell_count"].(int64); v != 2 {
		t.Errorf("code_cell_count=%v want 2", attrs["code_cell_count"])
	}
	if v, _ := attrs["markdown_cell_count"].(int64); v != 1 {
		t.Errorf("markdown_cell_count=%v want 1", attrs["markdown_cell_count"])
	}
	if v, _ := attrs["kernel"].(string); v != "python3" {
		t.Errorf("kernel=%q want python3", attrs["kernel"])
	}
	if v, _ := attrs["language"].(string); v != "python" {
		t.Errorf("language=%q want python", attrs["language"])
	}
}

// TestZeppelinAttributes seeds a minimal .zpln file (Zeppelin's JSON
// note format) and asserts the per-notebook attribute extraction.
func TestZeppelinAttributes(t *testing.T) {
	dir := t.TempDir()
	body := `{
  "name": "Spark Recon",
  "defaultInterpreterGroup": "spark",
  "paragraphs": [
    {"text": "%md\n# Header", "config": {"editorSetting": {"language": "markdown"}}},
    {"text": "%spark\nval x = 1", "config": {"editorSetting": {"language": "scala"}}},
    {"text": "%pyspark\nprint('hi')", "config": {"editorSetting": {"language": "python"}}}
  ]
}`
	path := filepath.Join(dir, "note.zpln")
	writeTemp2(t, path, body)

	attrs := notebookAttrs(t, "notebook/zeppelin", path)
	if v, _ := attrs["cell_count"].(int64); v != 3 {
		t.Errorf("cell_count=%v want 3", attrs["cell_count"])
	}
	if v, _ := attrs["code_cell_count"].(int64); v != 2 {
		t.Errorf("code_cell_count=%v want 2", attrs["code_cell_count"])
	}
	if v, _ := attrs["markdown_cell_count"].(int64); v != 1 {
		t.Errorf("markdown_cell_count=%v want 1", attrs["markdown_cell_count"])
	}
	if v, _ := attrs["kernel"].(string); v != "spark" {
		t.Errorf("kernel=%q want spark", attrs["kernel"])
	}
	if v, _ := attrs["title"].(string); v != "Spark Recon" {
		t.Errorf("title=%q want \"Spark Recon\"", attrs["title"])
	}
}

// TestNotebookMalformedDegrades verifies that broken JSON doesn't
// fail the walk — the file is dropped with empty attrs, same
// degradation pattern as other content types.
func TestNotebookMalformedDegrades(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.ipynb")
	writeTemp2(t, path, `{"cells": [`) // truncated JSON

	attrs := notebookAttrs(t, "notebook/jupyter", path)
	if len(attrs) != 0 {
		t.Errorf("malformed notebook should yield empty attrs; got %v", attrs)
	}
}

// notebookAttrs is the test helper: locates a registered content
// type by name and invokes its Attributes against a real file path
// via os.DirFS.
func notebookAttrs(t *testing.T, name, path string) content.Attributes {
	t.Helper()
	for _, ct := range content.DefaultRegistry().Types() {
		if ct.Name() == name {
			a, err := attributesAt(t.Context(), ct, path)
			if err != nil {
				t.Fatalf("Attributes(%s): %v", path, err)
			}
			return a
		}
	}
	t.Fatalf("content type %q not registered", name)
	return nil
}

func writeTemp2(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
