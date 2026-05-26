---
name: extend-cel-schema
description: Extends file-search-on's CEL attribute schema by editing the call sites that must move together — `cel.Variable(...)` declarations in `celexpr.New` (evaluator.go), the `ResolveName` switch + `zeroDefaults` map in `activation.go`, and `celexpr.Schema()` — then runs an audit script that catches drift between them. Use when adding a new content-type attribute, promoting a new front-matter key like `summary` or `slug`, or extending an existing content type's attribute set; cel-go errors at runtime (not compile time) when these fall out of sync, so the audit script is the only deterministic guard.
---

# Extend CEL Schema

Every CEL-visible attribute in file-search-on must be **declared**, **resolvable**, and **documented** — three call sites that must move together. Drift between them produces a runtime error (`no such attribute: foo`) the moment a CEL expression references the missing piece — `go build` will not catch it. This skill makes the invariant explicit and ships an audit script that reports drift.

## The invariant

A CEL-visible attribute must be **declared**, **resolvable**, and **documented**. Drift produces a runtime error (`no such attribute: foo`) the moment a CEL expression references the missing piece — `go build` will not catch it.

| Where | What goes there | Failure mode if missing |
| --- | --- | --- |
| `internal/celexpr/evaluator.go` — `cel.Variable("foo", cel.<Type>Type)` inside `New` | Declares the variable to cel-go | Compile error: `undeclared reference to 'foo'` |
| `internal/celexpr/activation.go` — resolution in `(*fileAttrsActivation).ResolveName`, via **either** a `case "foo":` returning a typed `FileAttributes` field **or** a key in the `zeroDefaults` map | How the attribute's value is produced at evaluation time | Runtime error: `no such attribute: foo` |
| `internal/celexpr/schema.go` — entry in the right `AttributeDoc` slice | Drives both `--list` output and the MCP `list_attributes` tool | Attribute is invisible to humans and to MCP clients |

**How resolution works (post-2024 refactor).** `ResolveName(name)` is a single switch:

1. **Typed fields** — common scalars (`name`, `path`, `size`, …) and every `is_*` predicate / `md5` / `similarity` / … resolve via a `case "foo": return a.attrs.Foo, true` that reads a `FileAttributes` struct field. No default needed; the field is always present.
2. **Type-specific / front-matter attributes** — fall through the switch to a verbatim `Extra[name]` lookup, then to `zeroDefaults[name]`. There is **no rename switch anymore**: the content type's `Attributes()` map must emit the *final CEL name* directly (e.g. emit `img_width`, not `width`; emit `json_kind`, not `kind`). The `zeroDefaults` entry supplies the value for files that didn't emit the key.

Schema slot → what you must add:

- `schema.Common` → `cel.Variable` + a typed `ResolveName` case (struct field). No `zeroDefaults` entry (it's never absent).
- `schema.TypeSpecific` / `schema.Frontmatter` → `cel.Variable` + a `zeroDefaults` entry + `Attributes()` emitting the key verbatim into `Extra`.

## Quick start

1. Decide the CEL variable name (snake_case) and type, and whether it's `Common`, `TypeSpecific`, or `Frontmatter`.
2. Add `cel.Variable("foo", cel.<Type>Type)` in `celexpr.New` (evaluator.go).
3. Make it resolve in `activation.go`:
   - **Common / typed predicate**: add a `case "foo": return a.attrs.Foo, true` to `ResolveName` (and the backing `FileAttributes` field).
   - **Type-specific / front-matter**: add `"foo": <zero-value>` to the `zeroDefaults` map, and make the content type's `Attributes()` put `"foo"` into its returned map verbatim (it flows through the `Extra[name]` fallthrough).
4. Add `{"foo", "<celtype>", "<description>"}` to the right slice in `celexpr.Schema()`.
5. **Run** the audit (see Scripts).
6. Update the README front-matter / attribute tables (the `schema_docs` test enforces this).
7. Add a test in `internal/content/frontmatter_test.go` (front-matter case) or the relevant content-type test file.

For a new content type's *registration mechanics* (creating the file in `internal/content/`, implementing `ContentType`), see the `add-content-type` skill — this skill only covers the CEL plumbing.

## Scripts

- **Run** `python scripts/audit_attributes.py` from the repo root — parses `cel.Variable` declarations from `internal/celexpr/evaluator.go`, the `ResolveName` switch cases + `zeroDefaults` map from `internal/celexpr/activation.go`, and the `AttributeDoc` slices from `internal/celexpr/schema.go`. Prints a markdown table (Declared / Cased / Defaulted / Documented per attribute) and exits non-zero on drift — every declared var must be cased OR defaulted, and every documented type-specific/front-matter attr must have a default. Run before committing any change to those files.

## References

- [references/foot-guns.md](references/foot-guns.md) — the verbatim `Extra[name]` resolution (the content type must emit the final CEL name, e.g. `img_width` not `width`; there is no longer a rename switch), the cel-go runtime error text, and the image-family branch in `BuildAttributes`.

## Conventions

- CEL variable names are `snake_case`. Match the existing style (`json_kind`, `img_width`, `frontmatter_format`).
- The CEL type for collections is exact: `cel.ListType(cel.StringType)` for `[]string`, `cel.ListType(cel.DoubleType)` for `[]float64`, `cel.MapType(cel.StringType, cel.DynType)` for `map[string]any`. cel-go errors loudly on type mismatch.
- Zero values in the `zeroDefaults` map must match the declared CEL type (e.g. `int64(0)` for `cel.IntType`, `[]string{}` for `cel.ListType(cel.StringType)`, never plain `0`).
- Do not edit `cmd/file-search-on/main.go:printHelp` — it's data-driven from `celexpr.Schema()`. Editing the `AttributeDoc` slice is sufficient.
- The `schema_docs` test (`internal/celexpr/schema_docs_test.go`) additionally requires every documented attribute to appear in `README.md` — update the README's attribute / front-matter tables in the same change or that test fails.
- If the new attribute crosses content types (e.g. `description` could come from PDF metadata *and* HTML `<meta>` *and* markdown front-matter), document each emitting site in the schema description so callers know what they're filtering on.
