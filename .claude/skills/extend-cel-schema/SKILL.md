---
name: extend-cel-schema
description: Extends file-search-on's CEL attribute schema by editing the four call sites that must move together — `cel.Variable(...)` declarations in `celexpr.New`, the activation defaults map in `Evaluate`, the `attrs.Extra` switch in `Evaluate`, and `celexpr.Schema()` — then runs an audit script that catches drift between them. Use when adding a new content-type attribute, promoting a new front-matter key like `summary` or `slug`, or extending an existing content type's attribute set; cel-go errors at runtime (not compile time) when these fall out of sync, so the audit script is the only deterministic guard.
---

# Extend CEL Schema

Every CEL-visible attribute in file-search-on must appear in **four** places. Drift between them produces a runtime error (`no such attribute: foo`) the moment a CEL expression references the missing piece — `go build` will not catch it. This skill makes the four-place invariant explicit and ships an audit script that reports drift.

## The four-place invariant

| # | Where | What goes there | Failure mode if missing |
| - | --- | --- | --- |
| 1 | `internal/celexpr/evaluator.go` — `cel.Variable("foo", cel.<Type>Type)` inside `New` | Declares the variable to cel-go | Compile error: `undeclared reference to 'foo'` |
| 2 | `internal/celexpr/evaluator.go` — activation defaults map literal in `Evaluate` | Zero value used when the matched type didn't emit this attribute | Runtime error: `no such attribute: foo` |
| 3 | `internal/celexpr/evaluator.go` — `attrs.Extra` switch in `Evaluate` | Maps the content-type's emitted key to the CEL variable name (often a rename, e.g. `kind` → `json_kind`) | Attribute is always its zero value — silently wrong, no error |
| 4 | `internal/celexpr/schema.go` — entry in the right `AttributeDoc` slice | Drives both `--list` output and the MCP `list_attributes` tool | Attribute is invisible to humans and to MCP clients |

Place 3 only applies to **type-specific** attributes (anything that comes from a `ContentType.Attributes()` map). The "common" attributes (`name`, `path`, `size`, `is_markdown`, …) are populated directly from the `FileAttributes` struct fields and don't go through the switch. Schema slot:

- `schema.Common` → places 1, 2 (no switch entry)
- `schema.TypeSpecific` → places 1, 2, 3, 4
- `schema.Frontmatter` → places 1, 2, 3, 4

## Quick start

1. Decide the CEL variable name (snake_case) and type. Decide whether it's `Common`, `TypeSpecific`, or `Frontmatter`.
2. Add `cel.Variable("foo", cel.<Type>Type)` in `celexpr.New` (place 1).
3. Add `"foo": <zero-value>` to the activation defaults map in `Evaluate` (place 2).
4. If the value comes from a `ContentType.Attributes()` map, add a `case "<source-key>": activation["foo"] = v` to the `attrs.Extra` switch (place 3).
5. Add `{"foo", "<celtype>", "<description>"}` to the right slice in `celexpr.Schema()` (place 4).
6. **Run** the audit (see Scripts).
7. Update the README front-matter table if the new attribute is front-matter-related.
8. Add a test in `internal/content/frontmatter_test.go` (front-matter case) or the relevant content-type test file.

For a new content type's *registration mechanics* (creating the file in `internal/content/`, implementing `ContentType`), see the `add-content-type` skill — this skill only covers the CEL plumbing.

## Scripts

- **Run** `python scripts/audit_attributes.py` from the repo root — parses `internal/celexpr/evaluator.go` and `internal/celexpr/schema.go`, prints a markdown table of every CEL attribute name with which of the four places it appears in, and exits non-zero on drift. Run before committing any change to those files.

## References

- [references/foot-guns.md](references/foot-guns.md) — the renames in the `attrs.Extra` switch (`kind` → `json_kind`, `width` → `img_width`, …), the cel-go runtime error text, and the image-family branch in `BuildAttributes`.

## Conventions

- CEL variable names are `snake_case`. Match the existing style (`json_kind`, `img_width`, `frontmatter_format`).
- The CEL type for collections is exact: `cel.ListType(cel.StringType)` for `[]string`, `cel.MapType(cel.StringType, cel.DynType)` for `map[string]any`. cel-go errors loudly on type mismatch.
- Zero values in the activation map must match the declared CEL type (e.g. `int64(0)` for `cel.IntType`, never plain `0`).
- Do not edit `cmd/file-search-on/main.go:printHelp` — it's data-driven from `celexpr.Schema()`. Editing place 4 is sufficient.
- If the new attribute crosses content types (e.g. `description` could come from PDF metadata *and* HTML `<meta>` *and* markdown front-matter), document each emitting site in the schema description so callers know what they're filtering on.
