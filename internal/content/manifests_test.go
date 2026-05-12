package content_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManifestsDetection(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name     string
		wantType string
	}{
		{"go.mod", "manifest/gomod"},
		{"go.sum", "manifest/gomod"},
		{"package.json", "manifest/node"},
		{"package-lock.json", "manifest/node"},
		{"Cargo.toml", "manifest/cargo"},
		{"Cargo.lock", "manifest/cargo"},
		{"Pipfile", "manifest/pipfile"},
		{"Pipfile.lock", "manifest/pipfile"},
		{"requirements.txt", "manifest/python-reqs"},
		{"Gemfile", "manifest/gemfile"},
		{"Gemfile.lock", "manifest/gemfile"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.name)
			if err := os.WriteFile(path, []byte("placeholder\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			ct := detectAt(path)
			if ct == nil {
				t.Fatalf("Detect %q: got nil, want %s", tc.name, tc.wantType)
			}
			if ct.Name() != tc.wantType {
				t.Errorf("Detect %q: got %q, want %q", tc.name, ct.Name(), tc.wantType)
			}
		})
	}
}

func TestGoModAttributes(t *testing.T) {
	dir := t.TempDir()
	gomod := `module github.com/richardwooding/file-search-on

go 1.26.2

require (
	github.com/alecthomas/kong v1.15.0
	gopkg.in/yaml.v3 v3.0.1
)
`
	path := filepath.Join(dir, "go.mod")
	if err := os.WriteFile(path, []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	ct := detectAt(path)
	if ct == nil || ct.Name() != "manifest/gomod" {
		t.Fatalf("Detect: got %v, want manifest/gomod", ct)
	}
	attrs, err := attributesAt(t.Context(), ct, path)
	if err != nil {
		t.Fatalf("Attributes: %v", err)
	}
	if got, _ := attrs["module"].(string); got != "github.com/richardwooding/file-search-on" {
		t.Errorf("module = %q, want github.com/richardwooding/file-search-on", got)
	}
	if got, _ := attrs["go_version"].(string); got != "1.26.2" {
		t.Errorf("go_version = %q, want 1.26.2", got)
	}
}

// TestGoSumNoAttributes verifies go.sum detects as manifest/gomod but
// surfaces no module/go_version (those only come from go.mod).
func TestGoSumNoAttributes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "go.sum")
	if err := os.WriteFile(path, []byte("gopkg.in/yaml.v3 v3.0.1 h1:fxVm/GzAzEWqLHuvctI91KS9hhNmmWOoWu0XTYJS7CA=\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ct := detectAt(path)
	if ct == nil || ct.Name() != "manifest/gomod" {
		t.Fatalf("Detect: got %v, want manifest/gomod", ct)
	}
	attrs, err := attributesAt(t.Context(), ct, path)
	if err != nil {
		t.Fatalf("Attributes: %v", err)
	}
	if v, ok := attrs["module"]; ok && v != "" {
		t.Errorf("go.sum should not populate module, got %q", v)
	}
	if v, ok := attrs["go_version"]; ok && v != "" {
		t.Errorf("go.sum should not populate go_version, got %q", v)
	}
}

// TestPackageJsonPrecedence verifies package.json detects as
// manifest/node, not as generic json — exact-name takes precedence
// over extension.
func TestPackageJsonPrecedence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "package.json")
	if err := os.WriteFile(path, []byte(`{"name": "x", "version": "1.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	ct := detectAt(path)
	if ct == nil || ct.Name() != "manifest/node" {
		t.Errorf("Detect: got %v, want manifest/node (exact-name should beat .json extension)", ct)
	}
}
