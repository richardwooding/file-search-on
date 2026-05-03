# CEL schema foot-guns

Specific things that catch authors off-guard when extending the CEL attribute schema. Read this before you touch `evaluator.go`.

## The cel-go runtime error

When a CEL variable is referenced by a compiled expression but missing from the activation map, cel-go errors at evaluation time, not compile time. The user sees:

```
search failed: walk: ...: evaluating CEL expression: no such attribute(s): foo
```

This is the failure mode for forgetting place 2 (the activation defaults map) or place 3 (the `attrs.Extra` switch — when the expression explicitly references the attribute and no content type emits it). Compile-only smoke tests don't catch this; the test that catches it is one that actually `Walk`s a real directory.

The guard against this is the activation defaults map: every `cel.Variable(...)` declared in `New` MUST have a key in the activation map literal in `Evaluate`, with a zero value of the right type. The audit script enforces this.

## The renames in the Extra switch

The keys content types emit in their `Attributes()` map are NOT always the same as the CEL variable names. The `attrs.Extra` switch in `Evaluate` is a translation layer.

Current renames (in `internal/celexpr/evaluator.go`):

| Source key (from `Attributes()`) | CEL variable |
| --- | --- |
| `kind` | `json_kind` |
| `width` | `img_width` |
| `height` | `img_height` |

All other keys pass through unchanged (`title` → `title`, `word_count` → `word_count`, etc.).

When adding a new attribute, you can either keep the same name on both sides (preferred) or introduce a rename. Renames are warranted when the source key is a generic word that would clash across content types — `kind` makes sense locally inside `jsonType.Attributes` but `json_kind` is clearer in CEL.

## Type mismatches between zero value and `cel.Variable` type

The zero value in the activation map must match the declared CEL type:

| `cel.Variable` type | Zero value |
| --- | --- |
| `cel.StringType` | `""` |
| `cel.IntType` | `int64(0)` (NOT plain `0`) |
| `cel.BoolType` | `false` |
| `cel.TimestampType` | `time.Time{}` |
| `cel.ListType(cel.StringType)` | `[]string{}` |
| `cel.MapType(cel.StringType, cel.DynType)` | `map[string]any{}` |

cel-go errors with a confusing "unsupported type conversion" message on mismatch. The audit script does not check zero-value types — `go vet` and the existing test suite do.

## Image-family branching

Image content types use a name prefix (`image/jpeg`, `image/png`, …). The `BuildAttributes` function in `evaluator.go` (around line 189) maps any name starting with `image/` to `is_image = true` via:

```go
case strings.HasPrefix(contentTypeName, "image/"):
    isImage = true
```

When adding a new image variant (e.g. `image/avif`):

1. Register it like any other content type — name MUST start with `image/`.
2. The `BuildAttributes` branch picks it up automatically; no edit needed there.

When adding a NEW family with its own `is_*` boolean (e.g. an audio family with `is_audio`), you DO need to edit `BuildAttributes`: add a `case strings.HasPrefix(contentTypeName, "audio/"): isAudio = true` and follow the four-place invariant for the `is_audio` CEL variable.

## When to update the README front-matter table

The README has a hand-maintained front-matter table at the bottom of "Markdown front-matter search". Update it when:

- Adding a new front-matter promoted variable (place 4 row in `schema.Frontmatter`).
- Changing the precedence note (e.g. front-matter > H1 for `title`).

The README does NOT list common attributes (`name`, `size`, …) or type-specific attributes (`page_count`, `img_width`, …) — those live only in `--list` and the MCP `list_attributes` tool. Don't add them to the README.

## What the audit script does NOT catch

The audit script enforces the four-place invariant for CEL attribute *names*. It does not check:

- Whether the CEL variable type matches the zero value's Go type.
- Whether the source key in the Extra switch matches what content types actually emit.
- Whether the `AttributeDoc.Type` string in `Schema()` matches the declared `cel.Variable` type.

Those checks are the job of `go build`, `go vet`, and the existing test suite. The audit catches the one class of error that no compiler will.
