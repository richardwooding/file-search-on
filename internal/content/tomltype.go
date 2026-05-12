package content

import (
	"context"
	"io/fs"
)

func init() {
	Register(&tomlType{})
}

type tomlType struct{}

func (*tomlType) Name() string         { return "toml" }
func (*tomlType) Extensions() []string { return []string{".toml"} }

// MagicBytes returns nil — TOML has no canonical magic byte. Extension
// detection covers the bulk of real-world TOML (pyproject.toml,
// Cargo.toml, justfile-adjacent configs). Exact-name dispatch in
// manifests.go handles Cargo.toml / Cargo.lock specifically and fires
// is_toml alongside is_cargo_manifest via setTypeFlags.
func (*tomlType) MagicBytes() [][]byte { return nil }

// Attributes returns an empty set today. TOML's root is always a
// table (mapping), so a yaml_kind-style attribute would be degenerate.
// Top-level-key enumeration (similar to csv_columns) is a plausible
// follow-up but not in scope here.
func (*tomlType) Attributes(ctx context.Context, _ fs.FS, _ string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return Attributes{}, nil
}
