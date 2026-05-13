package projecttype_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/richardwooding/file-search-on/internal/projecttype"
)

// withConfigEnv points the project-type discovery at two test-owned
// directories: a user-config root (returned) and a per-project CWD.
// Restores the prior CWD on test exit. Both XDG_CONFIG_HOME and HOME
// are set so the test passes on Linux (XDG) and macOS / Windows
// (HOME-rooted UserConfigDir) without branching.
func withConfigEnv(t *testing.T, configHome, cwd string) string {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("HOME", configHome)
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	// Resolve the platform-specific user-config dir AFTER the env
	// vars are set so tests pin to the actual file location the
	// production code would read from.
	cfg, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestLoadDiscovered_BothLayers(t *testing.T) {
	configHomeRoot := t.TempDir()
	cwd := t.TempDir()
	configHome := withConfigEnv(t, configHomeRoot, cwd)

	// User-wide config — under the platform's UserConfigDir.
	userDir := filepath.Join(configHome, "file-search-on")
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(userDir, "project-types.yaml"), `project_types:
  - name: user-wide-app
    indicators:
      - has_file: user.marker
`)

	// Per-project config in CWD's .file-search-on/.
	projDir := filepath.Join(cwd, ".file-search-on")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(projDir, "project-types.yaml"), `project_types:
  - name: project-local-app
    indicators:
      - cel: '"local.marker" in files'
`)

	reg := projecttype.NewRegistry()
	n, err := reg.LoadDiscovered()
	if err != nil {
		t.Fatalf("LoadDiscovered: %v", err)
	}
	if n != 2 {
		t.Errorf("loaded = %d, want 2 (one user-wide + one per-project)", n)
	}

	// Verify both types are detectable.
	userMatchDir := t.TempDir()
	mustWrite(t, filepath.Join(userMatchDir, "user.marker"), "")
	if matches := reg.Detect(nil, userMatchDir); !containsType(matches, "user-wide-app") {
		t.Errorf("user-wide-app not detected on user.marker dir; matches=%+v", matches)
	}

	localMatchDir := t.TempDir()
	mustWrite(t, filepath.Join(localMatchDir, "local.marker"), "")
	if matches := reg.Detect(nil, localMatchDir); !containsType(matches, "project-local-app") {
		t.Errorf("project-local-app not detected on local.marker dir; matches=%+v", matches)
	}
}

func TestLoadDiscovered_NoConfigs(t *testing.T) {
	// Empty dirs with no config files → loads zero types, no error.
	withConfigEnv(t, t.TempDir(), t.TempDir())
	reg := projecttype.NewRegistry()
	n, err := reg.LoadDiscovered()
	if err != nil {
		t.Fatalf("LoadDiscovered: %v", err)
	}
	if n != 0 {
		t.Errorf("loaded = %d, want 0 (no configs exist)", n)
	}
}

func TestLoadDiscovered_BadConfigSurfaces(t *testing.T) {
	configHomeRoot := t.TempDir()
	cwd := t.TempDir()
	configHome := withConfigEnv(t, configHomeRoot, cwd)

	userDir := filepath.Join(configHome, "file-search-on")
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(userDir, "project-types.yaml"), `project_types:
  - name: broken
    indicators:
      - cel: 'this isnt valid CEL ((('
`)

	reg := projecttype.NewRegistry()
	if _, err := reg.LoadDiscovered(); err == nil {
		t.Errorf("LoadDiscovered: expected compile error from bad CEL, got nil")
	}
}

func TestDiscoveryPaths_OrderUserWideThenCWD(t *testing.T) {
	withConfigEnv(t, t.TempDir(), t.TempDir())
	paths := projecttype.DiscoveryPaths()
	if len(paths) != 2 {
		t.Fatalf("paths = %v, want 2 entries (user-wide + CWD)", paths)
	}
	// User-wide is the first (lowest precedence; CWD layers on top).
	if filepath.Base(filepath.Dir(paths[0])) != "file-search-on" {
		t.Errorf("paths[0] = %q, expected user-wide entry under file-search-on/", paths[0])
	}
	if filepath.Base(filepath.Dir(paths[1])) != ".file-search-on" {
		t.Errorf("paths[1] = %q, expected per-project entry under .file-search-on/", paths[1])
	}
}

func containsType(matches []projecttype.Match, name string) bool {
	for _, m := range matches {
		if m.Type == name {
			return true
		}
	}
	return false
}
