# CEL schema foot-guns

Specific things that catch authors off-guard when extending the CEL attribute schema. Read this before you touch the schema files (`env.go` for `cel.Variable` declarations, `activation.go` for resolution, `schema.go` for docs, `typeflags.go` for family predicates).

## The cel-go runtime error

When a CEL variable is referenced by a compiled expression but missing from the activation map, cel-go errors at evaluation time, not compile time. The user sees:

```
search failed: walk: ...: evaluating CEL expression: no such attribute(s): foo
```

This is the failure mode for forgetting to make the attribute *resolvable* in `(*fileAttrsActivation).ResolveName` (`internal/celexpr/activation.go`) — either a `case "foo":` returning a typed field, or a `zeroDefaults` key for an Extra-flowing attribute that no content type emitted on this file. Compile-only smoke tests don't catch this; the test that catches it is one that actually `Walk`s a real directory.

The guard is the `zeroDefaults` map: every `cel.Variable(...)` declared in `New` that resolves through `Extra` MUST have a key in `zeroDefaults`, with a zero value of the right type. (Typed-field attributes resolve via a `ResolveName` case instead and don't need a default.) The audit script enforces `declared == cased ∪ defaulted`.

## No rename layer — emit the final CEL name

**There is no `attrs.Extra` rename switch.** (There used to be one in `Evaluate`; the 2024 activation refactor removed it.) `ResolveName` does a *verbatim* `Extra[name]` lookup, so a content type's `Attributes()` map MUST emit the exact CEL variable name — there is no translation step.

Concretely: `imagetype.go` emits `img_width` / `img_height` directly (not `width` / `height`); a JSON type that wants the `json_kind` CEL variable must put `"json_kind"` into its returned map, not `"kind"`. If the emitted key and the CEL name diverge, the attribute silently stays at its zero value — no error, just wrong.

When adding a new attribute, keep the same name on both sides. If you want a "namespaced" CEL name (`json_kind` rather than a bare `kind`), do the naming inside the content type's `Attributes()` map — that's the only place the name is set now.

## Type mismatches between zero value and `cel.Variable` type

The zero value in the `zeroDefaults` map must match the declared CEL type:

| `cel.Variable` type | Zero value |
| --- | --- |
| `cel.StringType` | `""` |
| `cel.IntType` | `int64(0)` (NOT plain `0`) |
| `cel.BoolType` | `false` |
| `cel.TimestampType` | `time.Time{}` |
| `cel.ListType(cel.StringType)` | `[]string{}` |
| `cel.MapType(cel.StringType, cel.DynType)` | `map[string]any{}` |

cel-go errors with a confusing "unsupported type conversion" message on mismatch. The audit script does not check zero-value types — `go vet` and the existing test suite do.

## Family-prefix branching

Family `is_*` predicates are set by name-prefix blocks in `setTypeFlags` (`internal/celexpr/typeflags.go`). For example, any name starting with `image/` sets `attrs.IsImage = true`:

```go
if strings.HasPrefix(name, "image/") {
    attrs.IsImage = true
}
```

When adding a new image variant (e.g. `image/avif`):

1. Register it like any other content type — name MUST start with `image/`.
2. The `setTypeFlags` prefix block picks it up automatically; no edit needed there.

When adding a NEW family with its own `is_*` boolean (e.g. a 3D-model family with `is_3d_model`), you DO need to:

1. Add the `Is3DModel bool` field to `FileAttributes`.
2. Add a `case "is_3d_model": return a.attrs.Is3DModel, true` to `ResolveName` (activation.go).
3. Add a `if strings.HasPrefix(name, "model3d/") { attrs.Is3DModel = true }` block to `setTypeFlags`.
4. Declare the `is_3d_model` `cel.Variable` and document it in `Schema()`.

(See the `model3d/`, `image/raw-`, and `font/` families for worked examples.)

## You MUST update the README — `schema_docs_test` enforces it

`internal/celexpr/schema_docs_test.go` cross-checks every `AttributeDoc` and `FunctionDoc` in `Schema()` against `README.md`: each documented attribute / function name must appear as a backticked literal somewhere in the README, or the test fails with e.g. `README.md missing TypeSpecific attribute "face_count"`.

So when you add a `schema.Common` / `schema.TypeSpecific` / `schema.Frontmatter` entry, also add the name to the matching README table (the per-family attribute tables under "Available attributes", or the front-matter table). This is not optional — `go test ./internal/celexpr/` will go red otherwise. (Historically the README only listed front-matter attributes; the test now covers the full schema.)

## What the audit script does NOT catch

The audit script enforces the declared / resolvable / documented invariant for CEL attribute *names*. It does not check:

- Whether the CEL variable type matches the zero value's Go type.
- Whether the key a content type actually emits into `Extra` matches the declared CEL name (the verbatim lookup means a typo'd emit key silently yields the zero value — see "No rename layer" above).
- Whether the `AttributeDoc.Type` string in `Schema()` matches the declared `cel.Variable` type.

Those checks are the job of `go build`, `go vet`, and the existing test suite (including `schema_docs_test.go`, which cross-checks `Schema()` against the README). The audit catches the one class of error that no compiler will.
