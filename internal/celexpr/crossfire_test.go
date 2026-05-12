package celexpr_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/richardwooding/file-search-on/internal/celexpr"
	"github.com/richardwooding/file-search-on/internal/content"
)

// TestCrossPredicateFiring verifies that exact-name content types also
// fire the predicate of their underlying syntactic format —
// is_node_manifest + is_json for package.json, is_cargo_manifest +
// is_toml for Cargo.toml, etc. The predicates aren't mutually
// exclusive, mirroring how is_image / is_jpeg coexist for image
// formats today.
func TestCrossPredicateFiring(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	reg := content.DefaultRegistry()

	cases := []struct {
		name string
		body string
		// flag-name → expected value. We assert each named flag.
		want map[string]bool
	}{
		{
			name: "package.json",
			body: `{"name":"x","version":"1.0.0"}`,
			want: map[string]bool{
				"IsNodeManifest": true,
				"IsManifest":     true,
				"IsJSON":         true,
			},
		},
		{
			name: "package-lock.json",
			body: `{"name":"x","lockfileVersion":3}`,
			want: map[string]bool{
				"IsNodeManifest": true,
				"IsManifest":     true,
				"IsJSON":         true,
			},
		},
		{
			name: "Cargo.toml",
			body: "[package]\nname = \"x\"\nversion = \"0.1.0\"\n",
			want: map[string]bool{
				"IsCargoManifest": true,
				"IsManifest":      true,
				"IsTOML":          true,
			},
		},
		{
			name: "Cargo.lock",
			body: "[[package]]\nname = \"x\"\nversion = \"0.1.0\"\n",
			want: map[string]bool{
				"IsCargoManifest": true,
				"IsManifest":      true,
				"IsTOML":          true,
			},
		},
		{
			name: "requirements.txt",
			body: "requests==2.31.0\nclick>=8.0\n",
			want: map[string]bool{
				"IsPythonReqs": true,
				"IsManifest":   true,
				"IsText":       true,
			},
		},
		{
			name: "LICENSE",
			body: "MIT License\n\nCopyright (c) 2026\n",
			want: map[string]bool{
				"IsLicense":  true,
				"IsRepoMeta": true,
				"IsText":     true,
			},
		},
		{
			name: "CHANGELOG",
			body: "v1.0.0 - Initial release\n",
			want: map[string]bool{
				"IsChangelog": true,
				"IsRepoMeta":  true,
				"IsText":      true,
			},
		},
		{
			name: "CONTRIBUTING",
			body: "Thanks for contributing!\n",
			want: map[string]bool{
				"IsContributing": true,
				"IsRepoMeta":     true,
				"IsText":         true,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Each case gets its own subdir to avoid collisions between
			// cases sharing a filename (e.g. multiple package.json runs).
			sub := t.TempDir()
			path := filepath.Join(sub, tc.name)
			if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			abs, err := filepath.Abs(path)
			if err != nil {
				t.Fatal(err)
			}
			attrs, err := celexpr.BuildAttributes(ctx, os.DirFS(filepath.Dir(abs)), filepath.Base(abs), abs, reg)
			if err != nil {
				t.Fatalf("BuildAttributes: %v", err)
			}
			for flagName, wantSet := range tc.want {
				got := readBoolField(t, attrs, flagName)
				if got != wantSet {
					t.Errorf("%s: %s = %v, want %v", tc.name, flagName, got, wantSet)
				}
			}
		})
	}
	_ = dir
}

// readBoolField reflects a named bool field off FileAttributes.
// Kept small and inline so the test stays self-contained.
func readBoolField(t *testing.T, attrs *celexpr.FileAttributes, name string) bool {
	t.Helper()
	switch name {
	case "IsJSON":
		return attrs.IsJSON
	case "IsTOML":
		return attrs.IsTOML
	case "IsText":
		return attrs.IsText
	case "IsManifest":
		return attrs.IsManifest
	case "IsRepoMeta":
		return attrs.IsRepoMeta
	case "IsNodeManifest":
		return attrs.IsNodeManifest
	case "IsCargoManifest":
		return attrs.IsCargoManifest
	case "IsPythonReqs":
		return attrs.IsPythonReqs
	case "IsLicense":
		return attrs.IsLicense
	case "IsChangelog":
		return attrs.IsChangelog
	case "IsContributing":
		return attrs.IsContributing
	}
	t.Fatalf("unhandled flag name in test: %s", name)
	return false
}
