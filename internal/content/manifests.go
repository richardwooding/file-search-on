package content

import (
	"context"
	"io/fs"

	"golang.org/x/mod/modfile"
)

func init() {
	Register(&gomodType{})
	Register(&nodeManifestType{})
	Register(&cargoManifestType{})
	Register(&pipfileType{})
	Register(&pythonReqsType{})
	Register(&gemfileType{})
}

// gomodType matches go.mod and go.sum. For go.mod, surfaces the
// declared module path and the Go toolchain version directive
// (e.g. "1.26.2") via the official golang.org/x/mod/modfile parser.
// go.sum is detected for type-classification only — it has no
// human-meaningful attributes to surface.
type gomodType struct{}

func (*gomodType) Name() string         { return "manifest/gomod" }
func (*gomodType) Extensions() []string { return nil }
func (*gomodType) MagicBytes() [][]byte { return nil }
func (*gomodType) Filenames() []string  { return []string{"go.mod", "go.sum"} }

func (*gomodType) Attributes(ctx context.Context, fsys fs.FS, p string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Only parse go.mod files; go.sum is a checksum database that
	// doesn't fit the same shape and would just error out below.
	if !endsWithBasename(p, "go.mod") {
		return Attributes{}, nil
	}
	data, err := readAll(fsys, p)
	if err != nil {
		return nil, err
	}
	// modfile.Parse is lenient on minor errors and returns a partial
	// File even on issues. Errors here are real syntax problems —
	// degrade silently with empty attrs (don't fail the walk).
	mf, err := modfile.Parse(p, data, nil)
	if err != nil {
		return Attributes{}, nil
	}
	attrs := Attributes{}
	if mf.Module != nil && mf.Module.Mod.Path != "" {
		attrs["module"] = mf.Module.Mod.Path
	}
	if mf.Go != nil && mf.Go.Version != "" {
		attrs["go_version"] = mf.Go.Version
	}
	return attrs, nil
}

// endsWithBasename reports whether p's basename equals name. Used so
// the go.mod parse path runs for both "go.mod" and "./go.mod" /
// "vendor/go.mod" inputs without an extra strings import.
func endsWithBasename(p, name string) bool {
	if len(p) < len(name) {
		return false
	}
	if p[len(p)-len(name):] != name {
		return false
	}
	if len(p) == len(name) {
		return true
	}
	c := p[len(p)-len(name)-1]
	return c == '/' || c == '\\'
}

// nodeManifestType matches the Node.js ecosystem's manifest +
// lockfile. Detection-only for now (parsing name/version/scripts/
// dependencies is a follow-up). Note: precedence — exact-name
// dispatch runs before extension matching, so package.json detects
// here rather than as generic json. Same for package-lock.json.
type nodeManifestType struct{}

func (*nodeManifestType) Name() string         { return "manifest/node" }
func (*nodeManifestType) Extensions() []string { return nil }
func (*nodeManifestType) MagicBytes() [][]byte { return nil }
func (*nodeManifestType) Filenames() []string {
	return []string{"package.json", "package-lock.json"}
}
func (*nodeManifestType) Attributes(ctx context.Context, _ fs.FS, _ string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return Attributes{}, nil
}

// cargoManifestType matches Rust's Cargo manifest + lock files.
type cargoManifestType struct{}

func (*cargoManifestType) Name() string         { return "manifest/cargo" }
func (*cargoManifestType) Extensions() []string { return nil }
func (*cargoManifestType) MagicBytes() [][]byte { return nil }
func (*cargoManifestType) Filenames() []string  { return []string{"Cargo.toml", "Cargo.lock"} }
func (*cargoManifestType) Attributes(ctx context.Context, _ fs.FS, _ string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return Attributes{}, nil
}

// pipfileType matches Python's Pipenv manifest + lockfile.
type pipfileType struct{}

func (*pipfileType) Name() string         { return "manifest/pipfile" }
func (*pipfileType) Extensions() []string { return nil }
func (*pipfileType) MagicBytes() [][]byte { return nil }
func (*pipfileType) Filenames() []string  { return []string{"Pipfile", "Pipfile.lock"} }
func (*pipfileType) Attributes(ctx context.Context, _ fs.FS, _ string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return Attributes{}, nil
}

// pythonReqsType matches Python's pip requirements files. Standard
// requirements.txt convention. Other variants (constraints.txt,
// dev-requirements.txt) are out of scope; the exact-name match keeps
// the surface predictable.
type pythonReqsType struct{}

func (*pythonReqsType) Name() string         { return "manifest/python-reqs" }
func (*pythonReqsType) Extensions() []string { return nil }
func (*pythonReqsType) MagicBytes() [][]byte { return nil }
func (*pythonReqsType) Filenames() []string  { return []string{"requirements.txt"} }
func (*pythonReqsType) Attributes(ctx context.Context, _ fs.FS, _ string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return Attributes{}, nil
}

// gemfileType matches Ruby's Bundler manifest + lockfile.
type gemfileType struct{}

func (*gemfileType) Name() string         { return "manifest/gemfile" }
func (*gemfileType) Extensions() []string { return nil }
func (*gemfileType) MagicBytes() [][]byte { return nil }
func (*gemfileType) Filenames() []string  { return []string{"Gemfile", "Gemfile.lock"} }
func (*gemfileType) Attributes(ctx context.Context, _ fs.FS, _ string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return Attributes{}, nil
}
