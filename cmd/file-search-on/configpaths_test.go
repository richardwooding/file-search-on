package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/richardwooding/projectdetect"
)

// TestPrintConfigPaths_ExistenceMarker verifies the leading marker
// (`*` for present, ` ` for absent) so users can see at a glance
// whether their config is in the right place.
func TestPrintConfigPaths_ExistenceMarker(t *testing.T) {
	tmp := t.TempDir()
	presentDir := filepath.Join(tmp, "present")
	if err := os.MkdirAll(presentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	presentPath := filepath.Join(presentDir, "project-types.yaml")
	if err := os.WriteFile(presentPath, []byte("project_types: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	missingPath := filepath.Join(tmp, "missing", "project-types.yaml")

	entries := []projectdetect.DiscoveryEntry{
		{Scope: "user-wide", Path: presentPath},
		{Scope: "per-project", Path: missingPath},
	}

	var buf bytes.Buffer
	printConfigPaths(&buf, entries)
	got := buf.String()

	if !strings.Contains(got, "* user-wide") {
		t.Errorf("present entry should have `* ` prefix; got:\n%s", got)
	}
	if !strings.Contains(got, "  per-project") {
		t.Errorf("missing entry should have `  ` prefix; got:\n%s", got)
	}
	if !strings.Contains(got, presentPath) || !strings.Contains(got, missingPath) {
		t.Errorf("paths missing from output:\n%s", got)
	}
}

// TestPrintConfigPathsJSON_ShapeAndExists verifies the JSON output
// carries scope + path + exists, suitable for piping to jq.
func TestPrintConfigPathsJSON_ShapeAndExists(t *testing.T) {
	tmp := t.TempDir()
	present := filepath.Join(tmp, "x.yaml")
	if err := os.WriteFile(present, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(tmp, "absent.yaml")

	entries := []projectdetect.DiscoveryEntry{
		{Scope: "user-wide", Path: present},
		{Scope: "per-project", Path: missing},
	}

	var buf bytes.Buffer
	if err := printConfigPathsJSON(&buf, entries); err != nil {
		t.Fatal(err)
	}
	var decoded []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	if len(decoded) != 2 {
		t.Fatalf("got %d entries, want 2", len(decoded))
	}
	if decoded[0]["scope"] != "user-wide" || decoded[0]["exists"] != true {
		t.Errorf("entry 0: scope/exists wrong: %+v", decoded[0])
	}
	if decoded[1]["scope"] != "per-project" || decoded[1]["exists"] != false {
		t.Errorf("entry 1: scope/exists wrong: %+v", decoded[1])
	}
}

// TestPrintConfigPaths_EmptyEntries surfaces a clear message when
// DiscoveryEntries returns empty (no anchor resolvable — never
// happens in practice but worth covering for robustness).
func TestPrintConfigPaths_EmptyEntries(t *testing.T) {
	var buf bytes.Buffer
	printConfigPaths(&buf, nil)
	got := buf.String()
	if !strings.Contains(got, "no discovery paths") {
		t.Errorf("empty entries should print explanatory line; got: %q", got)
	}
}
