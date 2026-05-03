# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

`file-search-on` is a Go CLI that recursively searches a directory and matches files against a [CEL](https://github.com/google/cel-spec) expression evaluated over file metadata and content-type-specific attributes (e.g. `is_pdf && page_count > 10`, `is_markdown && word_count > 500`).

**Markdown front-matter search is a primary feature**, not a side concern — see the dedicated section below before changing anything in `internal/content/markdown.go` or `internal/content/frontmatter.go`.

Module: `github.com/richardwooding/file-search-on`. Toolchain: Go 1.26.2.

## Commands

```sh
go build ./...                                  # build all packages
go build -o file-search-on ./cmd/file-search-on # build the CLI binary
go test ./...                                   # run all tests
go test -race -coverprofile=coverage.out ./...  # what CI runs
go test ./internal/celexpr -run TestEvaluator   # run a single test (regex match)
go vet ./...
go fix -diff ./...                              # preview Go 1.26 modernizers (see below)
go fix ./...                                    # apply them
golangci-lint run                               # CI uses `latest` version
```

### `go fix` (Go 1.26+ modernizers)

Go 1.26 reintroduced `go fix` as a code-modernization tool — it rewrites code to use newer language and stdlib features (e.g. `slices.Contains`, `any`, `min`/`max`, `range` over an integer, `sync.WaitGroup.Go`). See [the announcement](https://go.dev/blog/gofix) for the full set of fixers.

CI runs `go fix ./... && git diff --exit-code` after the build step, so any unapplied modernizer will fail the pipeline. Workflow:

1. Make sure the working tree is clean (`go fix` edits should be a separate commit from feature work).
2. Run `go fix -diff ./...` to preview, or `go fix ./...` to apply. Re-run until idempotent — applying one fix can unlock another.
3. `go test ./...` afterwards; semantic conflicts may need a manual touch-up.

Use `go tool fix help` to list fixers and `go tool fix help <name>` for details on a specific one.

Run the CLI:

```sh
./file-search-on 'is_markdown && word_count > 100' -d ./docs
./file-search-on --list   # list supported attributes & registered content types
```

If `Expr` is empty it defaults to `"true"` (matches all files). Worker count defaults to `runtime.NumCPU()` when `-w` is 0 or unset.

## Architecture

Three internal packages compose the pipeline. Read them together — they are tightly coupled by the `FileAttributes` shape:

- **`internal/content`** — pluggable content-type detection. Each type (markdown, json, xml, html, pdf, image variants) implements the `ContentType` interface (`Name`, `Extensions`, `MagicBytes`, `Attributes(path)`) and self-registers via `init()` calling `content.Register(...)` on a package-global `defaultRegistry`. `Registry.Detect(path)` tries extension match first, then falls back to magic-byte sniffing on the first 512 bytes. `Attributes(path)` is called *per matching file* during the walk and returns a `map[string]interface{}` of type-specific fields.
- **`internal/celexpr`** — wraps `cel-go`. `New(expr)` declares a fixed schema of CEL variables (the union of common file attributes + every type-specific attribute any content type might emit) and compiles the program once. `BuildAttributes(path, registry)` runs `os.Stat`, calls `registry.Detect`, then calls the matched type's `Attributes`, and packs everything into `FileAttributes`. `Evaluate` flattens that into a CEL activation, supplying zero values for type-specific vars when the matched type didn't produce them — this is required because cel-go errors on undeclared/unbound variables.
- **`internal/search`** — `Walk` is the orchestrator. It compiles the CEL expression once, then fans out: a `filepath.WalkDir` producer feeds paths into a buffered channel; N workers (`Workers`, default `NumCPU`) pull paths, call `BuildAttributes` + `Evaluate`, and append matches under a single `sync.Mutex`. Directory traversal errors are swallowed (returning `nil` from the WalkDir func). `ctx` cancellation is checked only at the producer.

The CLI (`cmd/file-search-on/main.go`) uses `kong` for argument parsing. The single `search` subcommand is the default. After `Walk` returns, results are sorted by path and printed as `<path>\t[<content-type>]\t<size> bytes`. The match count goes to stderr.

### Markdown front-matter (primary feature)

`internal/content/frontmatter.go` is the parser. `splitFrontmatter(data []byte) (*Frontmatter, []byte)` is the only entry point — it returns the parsed metadata (or `nil` if absent) and the body bytes that the rest of `markdown.go` should treat as the document.

Detection is purely by the leading bytes: `---\n` ⇒ YAML, `+++\n` ⇒ TOML, `{` at byte 0 ⇒ JSON. There is intentionally no magic-byte fallback — if the first bytes don't match, `frontmatter_format` is `""` and every promoted variable holds its zero value. Malformed input degrades silently (returns nil + the original bytes) so a broken front-matter block doesn't make a file vanish from results.

Six keys are promoted to first-class CEL variables in `markdown.go`'s `Attributes`: `title`, `author`, `tags`, `categories`, `draft`, `date`. Anything else is reachable through `frontmatter.<key>`. Title precedence is *front-matter > H1*. `tags` and `categories` accept either a list or a bare string (which is wrapped) — that coercion lives in `stringListValue`. `date` accepts native `time.Time` (TOML) or several string layouts (`timeValue`); add new layouts there, not at call sites.

Map values are normalised through `normalizeMap`/`normalizeValue` so cel-go sees `map[string]any` end-to-end. yaml.v3 already does this for the top-level map, but nested maps from generic `any` paths can sometimes be `map[any]any`; the normaliser covers that.

When you change the promoted-variable set:

1. Update `markdown.go` `Attributes` to populate the new key in the returned map (with a zero-value fallback in the same default block — never leave a key undeclared).
2. Add a matching `cel.Variable(...)` in `celexpr.New` **and** a default in the activation map and a case in the `attrs.Extra` switch in `celexpr.Evaluate`. All three must move together; cel-go errors on undeclared variables at runtime.
3. Update `cmd/file-search-on/main.go:printHelp` and the README front-matter table.
4. Add a test in `internal/content/frontmatter_test.go` that exercises the new promotion across at least one of the three formats.

### Adding a new content type

1. Create a new file in `internal/content/` implementing the `ContentType` interface and call `Register(&yourType{})` from `init()`.
2. If it introduces new attributes, declare matching `cel.Variable(...)` entries in `celexpr.New` **and** wire them in both the activation defaults map and the `attrs.Extra` switch in `Evaluate` (`internal/celexpr/evaluator.go`). Forgetting either side will produce CEL "no such attribute" errors at runtime.
3. If it's an image-family type, also extend the `strings.HasPrefix(contentTypeName, "image/")` branch logic in `BuildAttributes` and add it to the `--list` output in `cmd/file-search-on/main.go:printHelp`.