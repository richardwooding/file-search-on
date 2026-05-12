package projecttype_test

import (
	"path/filepath"
	"testing"

	"github.com/richardwooding/file-search-on/internal/projecttype"
)

// configLoadTests use an isolated registry (via NewRegistry) so that
// custom types declared in YAML don't pollute the package-level
// defaultRegistry that the other tests detect against.

func TestLoadFromFile_CELIndicator(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "types.yaml")
	mustWrite(t, configPath, `project_types:
  - name: my-app
    description: Internal Foo app
    indicators:
      - cel: '"services" in subdirs && "foo.yaml" in files'
`)
	reg := projecttype.NewRegistry()
	n, err := reg.LoadFromFile(configPath)
	if err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}
	if n != 1 {
		t.Errorf("registered = %d, want 1", n)
	}

	// Directory matching the declared CEL: needs a `services`
	// subdirectory AND a foo.yaml.
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, "services"))
	mustWrite(t, filepath.Join(dir, "foo.yaml"), "x: 1\n")

	matches := reg.Detect(nil, dir)
	var foundMyApp bool
	for _, m := range matches {
		if m.Type == "my-app" {
			foundMyApp = true
			break
		}
	}
	if !foundMyApp {
		t.Errorf("Detect did not fire my-app on a directory matching its CEL; matches=%+v", matches)
	}

	// A directory MISSING the `services` subdir should NOT match.
	other := t.TempDir()
	mustWrite(t, filepath.Join(other, "foo.yaml"), "x: 1\n")
	for _, m := range reg.Detect(nil, other) {
		if m.Type == "my-app" {
			t.Errorf("my-app fired on dir missing 'services' subdir; matches=%+v", m)
		}
	}
}

func TestLoadFromFile_MixedIndicators(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "types.yaml")
	mustWrite(t, configPath, `project_types:
  - name: helm-chart
    indicators:
      - has_file: Chart.yaml
  - name: tf-stack
    indicators:
      - has_glob: "*.tf"
      - cel: '"main.tf" in files'
`)
	reg := projecttype.NewRegistry()
	n, err := reg.LoadFromFile(configPath)
	if err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}
	if n != 2 {
		t.Errorf("registered = %d, want 2", n)
	}

	chartDir := t.TempDir()
	mustWrite(t, filepath.Join(chartDir, "Chart.yaml"), "name: x\n")
	hasHelm := false
	for _, m := range reg.Detect(nil, chartDir) {
		if m.Type == "helm-chart" {
			hasHelm = true
		}
	}
	if !hasHelm {
		t.Errorf("Helm chart not detected by has_file indicator")
	}

	// tf-stack should match a *.tf directory.
	tfDir := t.TempDir()
	mustWrite(t, filepath.Join(tfDir, "main.tf"), "")
	hasTF := false
	for _, m := range reg.Detect(nil, tfDir) {
		if m.Type == "tf-stack" {
			hasTF = true
		}
	}
	if !hasTF {
		t.Errorf("tf-stack not detected; matches=%+v", reg.Detect(nil, tfDir))
	}
}

func TestLoadFromFile_BadCEL(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "bad.yaml")
	mustWrite(t, configPath, `project_types:
  - name: broken
    indicators:
      - cel: 'this is not valid cel ((('
`)
	reg := projecttype.NewRegistry()
	if _, err := reg.LoadFromFile(configPath); err == nil {
		t.Errorf("LoadFromFile: expected compile error, got nil")
	}
}

func TestLoadFromFile_MissingIndicators(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "missing.yaml")
	mustWrite(t, configPath, `project_types:
  - name: missing
`)
	reg := projecttype.NewRegistry()
	if _, err := reg.LoadFromFile(configPath); err == nil {
		t.Errorf("LoadFromFile: expected error for missing indicators, got nil")
	}
}
