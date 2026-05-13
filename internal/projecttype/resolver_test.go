package projecttype_test

import (
	"path/filepath"
	"testing"

	"github.com/richardwooding/file-search-on/internal/projecttype"
)

func TestResolver_FindsNearestProject(t *testing.T) {
	root := t.TempDir()
	// root/proj/go.mod  → 'proj' is a Go project
	// root/proj/cmd/main.go
	// root/proj/inner/Cargo.toml → 'inner' is a Rust project
	// root/proj/inner/src/lib.rs
	mustMkdir(t, filepath.Join(root, "proj", "cmd"))
	mustMkdir(t, filepath.Join(root, "proj", "inner", "src"))
	mustWrite(t, filepath.Join(root, "proj", "go.mod"), "module x\n")
	mustWrite(t, filepath.Join(root, "proj", "cmd", "main.go"), "package main\n")
	mustWrite(t, filepath.Join(root, "proj", "inner", "Cargo.toml"), "[package]\nname=\"x\"\n")
	mustWrite(t, filepath.Join(root, "proj", "inner", "src", "lib.rs"), "")

	resolver := projecttype.NewResolver(root, nil)

	// main.go lives in proj/cmd → nearest ancestor is proj (go).
	matches := resolver.Resolve(filepath.Join(root, "proj", "cmd", "main.go"))
	if len(matches) != 1 || matches[0].Type != "go" {
		t.Errorf("cmd/main.go: matches=%+v, want [go]", matches)
	}

	// lib.rs lives in proj/inner/src → nearest ancestor is inner (rust).
	matches = resolver.Resolve(filepath.Join(root, "proj", "inner", "src", "lib.rs"))
	if len(matches) != 1 || matches[0].Type != "rust" {
		t.Errorf("inner/src/lib.rs: matches=%+v, want [rust] (closer than the outer Go project)", matches)
	}
}

func TestResolver_NoProject(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "loose.txt"), "")
	resolver := projecttype.NewResolver(root, nil)
	if matches := resolver.Resolve(filepath.Join(root, "loose.txt")); matches != nil {
		t.Errorf("file outside any project should return nil, got %+v", matches)
	}
}

func TestResolver_CustomRegistry(t *testing.T) {
	// Use an isolated registry with a single custom type so we can
	// verify the resolver targets the registry it was constructed
	// with, not defaultRegistry.
	reg := projecttype.NewRegistry()
	if err := reg.Register(&projecttype.ProjectType{
		Name: "custom",
		Indicators: []projecttype.Indicator{
			{HasFile: "custom.marker"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "custom.marker"), "")
	mustWrite(t, filepath.Join(root, "go.mod"), "module x\n") // would fire 'go' in defaultRegistry
	mustWrite(t, filepath.Join(root, "file.txt"), "")

	resolver := projecttype.NewResolver(root, reg)
	matches := resolver.Resolve(filepath.Join(root, "file.txt"))
	if len(matches) != 1 || matches[0].Type != "custom" {
		t.Errorf("custom resolver: matches=%+v, want [custom] (NOT [go] from defaultRegistry)", matches)
	}
}
