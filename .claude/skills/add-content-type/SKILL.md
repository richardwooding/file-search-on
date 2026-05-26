---
name: add-content-type
description: Registers a new file-format content type in file-search-on by creating a file under `internal/content/` that implements the four-method `ContentType` interface (`Name`, `Extensions`, `MagicBytes`, `Attributes(ctx, fsys, path)`), self-registers via `init()` calling `content.Register(...)`, and for image variants relies on the `image/` prefix block in `setTypeFlags` (`internal/celexpr/evaluator.go`) — covers CSV, YAML, plain-text, EPUB, new audio/image families, and similar formats. Use when adding support for a new file format so the search recognises it and reports a `content_type`; does NOT cover CEL attribute wiring for type-specific attributes, which is the `extend-cel-schema` skill's job.
---

# Add Content Type

A "content type" is anything `Detect()` can identify and `Walk` can tag. Registering one is mostly mechanical: drop a file in `internal/content/`, implement four methods, register it, done. The wrinkle is the `Attributes(ctx, fsys, path)` method — if it returns *any* type-specific keys, you also need the `extend-cel-schema` workflow to make those keys CEL-visible.

## What gets registered

A type is identified by its `Name()` (e.g. `"json"`, `"image/png"`) and detected by either:

- **Extension match** — `.json`, `.md`, etc. Tried first.
- **Magic-byte prefix** — first 512 bytes of the file. Used as a fallback when the extension doesn't match.

Either or both can be empty (extension-only or magic-only types are valid). Registration is a side-effect of `init()` calling `content.Register(...)` — there is no central list to edit.

## Quick start (non-image, no attributes)

For a type like CSV that you want detected but where you don't (yet) care about per-file attributes:

1. **Copy the template:**
   ```sh
   cp .claude/skills/add-content-type/templates/content_type.go.tmpl internal/content/csv.go
   ```
2. Fill in `csv.go`: replace `<typename>`, `<TypeName>`, `<name>`, `<.ext>`, magic bytes (or `nil`).
3. Implement `Attributes` — for now, just `return Attributes{}, nil`.
4. **Run** `go build ./...` and `go test ./...` — no test edits required for a no-attribute type.
5. **Run** `go run ./cmd/file-search-on --list` — your new type appears under "Registered content types".

## Quick start (with attributes)

If `Attributes(ctx, fsys, path)` returns type-specific keys (e.g. `csv_columns`, `csv_rows`):

1. Steps 1–3 as above, but make `Attributes` return the keys.
2. **Then** run the `extend-cel-schema` skill for *each* new key — declare the CEL variable, add to the activation defaults, add a switch case, and document in `Schema()`.
3. **Run** `python .claude/skills/extend-cel-schema/scripts/audit_attributes.py` — must pass.
4. **Run** `go test ./...`.
5. Add a unit test in `internal/content/csv_test.go` exercising the new type.

## The `ContentType` interface

```go
type ContentType interface {
    Name() string                                                                 // stable identifier; image family must use "image/<subtype>"
    Extensions() []string                                                         // lowercase, with leading dot, e.g. []string{".csv"}
    MagicBytes() [][]byte                                                         // each entry is a prefix to match against the first 512 bytes; nil if not used
    Attributes(ctx context.Context, fsys fs.FS, path string) (Attributes, error)  // called per matching file; read via fsys, not os; return a map[string]any, never nil
}
```

Semantic notes:

- **`Name()`** — used by `setTypeFlags` (in `internal/celexpr/evaluator.go`) to set the `is_*` flag and the `content_type` CEL attribute. For an image family, the name MUST start with `image/` (e.g. `image/avif`); for the office family, MUST start with `office/`. The corresponding `strings.HasPrefix(name, ...)` block in `setTypeFlags` turns `is_image` / `is_office` on automatically.
- **`Extensions()`** — lowercase, dotted. The detector matches case-insensitively against `filepath.Ext(path)` lower-cased.
- **`MagicBytes()`** — return `nil` (not `[][]byte{}`) if the type is detected by extension only. Each `[]byte` is a prefix; the detector matches if any prefix is a prefix of the first 512 bytes.
- **`Attributes(ctx, fsys, path)`** — called *per matching file* during the walk. **Read the file through `fsys`, not `os`** — open with `fsys.Open(path)`, or use the helpers in `internal/content/fsutil.go` (`openReadSeeker` for seekable/streamed access, `openReaderAt` for `zip`/`pdf`-style random access, `readAll` for whole-file slurps). This is what lets the same parser run against real files, archive entries, and in-memory test filesystems. Honour ctx: check `ctx.Err()` at entry, and inside any unbounded scan/decode loop. Return `ctx.Err()` on cancellation. Avoid expensive parses without bounds (use `bufio.Scanner` with a buffer cap, decode just enough of the file). Return `Attributes{}` (empty) if no type-specific data; never return `nil`.

## Image-family addition

When the new type is an image variant (e.g. `image/avif`):

- `Name()` MUST return `image/<subtype>`.
- `setTypeFlags` (`internal/celexpr/evaluator.go`) already has an `if strings.HasPrefix(name, "image/")` block — no edit needed there for new image variants; it sets `is_image = true` automatically. The type's `Attributes` should emit `img_width` / `img_height` directly (the final CEL names — there is no rename layer; see the `extend-cel-schema` foot-guns).
- If the new image type emits *additional* attributes beyond `img_width` / `img_height` (e.g. `color_space`, `bit_depth`), each one is a CEL-schema extension — use the `extend-cel-schema` skill.

For a non-image type with its own `is_*` flag (e.g. an `audio/*` family):

- Add an `IsAudio bool` field to `FileAttributes`.
- Add a `case "is_audio": return a.attrs.IsAudio, true` to `ResolveName` (`activation.go`) and declare the `is_audio` CEL variable (`extend-cel-schema`).
- Add an `if strings.HasPrefix(name, "audio/") { attrs.IsAudio = true }` block to `setTypeFlags`.

## Templates

- [templates/content_type.go.tmpl](templates/content_type.go.tmpl) — skeleton for a non-image, no-attribute content type. Copy into `internal/content/<name>.go` and fill in.

## References

- [references/detection.md](references/detection.md) — how `Registry.Detect` works (extension-then-magic), magic-byte gotchas, and what `Attributes(ctx, fsys, path)` should and should not do during a walk.

## Conventions

- File name in `internal/content/` is the type name lowercased: `csv.go`, `epub.go`, `image_avif.go` (use underscores for `image/...` subtypes).
- The struct type is unexported and has the form `<name>Type` (e.g. `csvType`). Match the existing files in `internal/content/`.
- Register from `init()`. Never expose a `New<Name>Type()` constructor — registration is a side-effect, not an API.
- `MagicBytes` returns `nil` for "no magic"; never an empty slice. The detector special-cases `nil` to skip the magic check.
- Tests for new types live in `internal/content/<name>_test.go` and follow the existing pattern (see `markdown_test.go` for a thorough example, `jsontype_test.go` for a minimal one).
- After adding the type, **run** `go run ./cmd/file-search-on --list` to confirm the type appears in the registered-types listing. The listing is generated from `content.DefaultRegistry().Types()`, so a missing type means `Register(...)` wasn't called.
