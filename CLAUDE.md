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
./file-search-on mcp      # serve MCP over stdio (see "MCP server" below)
```

If `Expr` is empty it defaults to `"true"` (matches all files). Worker count defaults to `runtime.NumCPU()` when `-w` is 0 or unset.

## Architecture

Four internal packages compose the pipeline. The first three are tightly coupled by the `FileAttributes` shape:

- **`internal/content`** — pluggable content-type detection. Each type (markdown, json, xml, html, pdf, image variants) implements the `ContentType` interface (`Name`, `Extensions`, `MagicBytes`, `Attributes(path)`) and self-registers via `init()` calling `content.Register(...)` on a package-global `defaultRegistry`. `Registry.Detect(path)` tries extension match first, then falls back to magic-byte sniffing on the first 512 bytes. `Attributes(path)` is called *per matching file* during the walk and returns a `map[string]interface{}` of type-specific fields.
- **`internal/celexpr`** — wraps `cel-go`. `New(expr)` declares a fixed schema of CEL variables (the union of common file attributes + every type-specific attribute any content type might emit) and compiles the program once. `BuildAttributes(path, registry)` runs `os.Stat`, calls `registry.Detect`, then calls the matched type's `Attributes`, and packs everything into `FileAttributes`. `Evaluate` flattens that into a CEL activation, supplying zero values for type-specific vars when the matched type didn't produce them — this is required because cel-go errors on undeclared/unbound variables. `Schema()` (in `schema.go`) returns the structured docs that drive both `--list` and the MCP `list_attributes` tool — the only place attribute documentation is hard-coded.
- **`internal/search`** — `Walk` is the orchestrator. It compiles the CEL expression once, then fans out: a `filepath.WalkDir` producer feeds paths into a buffered channel; N workers (`Workers`, default `NumCPU`) pull paths, call `BuildAttributes` + `Evaluate`, and append matches under a single `sync.Mutex`. Directory traversal errors are swallowed (returning `nil` from the WalkDir func). `ctx` cancellation is checked only at the producer.
- **`internal/mcpserver`** — exposes the same search via the [official MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk). `New(version)` builds an `*mcp.Server` with two tools (`search` thinly wraps `search.Walk`; `list_attributes` returns `celexpr.Schema()` plus the registered content types). `Run(ctx, version)` is the stdio entry point used by the `mcp` subcommand.

The CLI (`cmd/file-search-on/main.go`) uses `kong` for argument parsing. Two subcommands: `search` (default `withargs`) and `mcp`. After `Walk` returns, search results are sorted by path and printed as `<path>\t[<content-type>]\t<size> bytes`; the match count goes to stderr.

### Releases

Releases are tag-driven. Pushing a tag matching `v*` to `origin` triggers `.github/workflows/release.yml`, which runs **GoReleaser v2** (pinned `~> v2`, currently v2.15.4) and produces three artifacts in one shot:

1. **GitHub Release** with archives for `linux`/`darwin`/`windows` × `amd64`/`arm64` (six total — `.tar.gz` for unix, `.zip` for windows) plus `checksums.txt`. Binaries are stamped with `version`/`commit`/`date` via `-ldflags -X`; `./file-search-on --version` reads them back.
2. **OCI image** at `ghcr.io/richardwooding/file-search-on:<version>` (and `:latest` for non-prerelease tags), built by `ko` from the same `builds:` block. Linux only — `amd64` + `arm64` manifests. Base image is `cgr.dev/chainguard/static` (no shell). Image labels follow OCI annotation conventions.
3. **Homebrew cask** committed to [`richardwooding/homebrew-tap`](https://github.com/richardwooding/homebrew-tap) at `Casks/file-search-on.rb`. Install path is `brew install richardwooding/tap/file-search-on`.

Notes for future agents:

- The config uses **`homebrew_casks`**, not `brews`. `brews` was deprecated in GoReleaser v2.10; we deliberately picked the modern key. The cask form handles macOS-intel/macOS-arm/Linux-intel/Linux-arm splits via Homebrew's `on_macos`/`on_linux` DSL — works fine for CLI binaries even though casks were originally for GUI apps.
- ko publishes via [`go-containerregistry`](https://github.com/google/go-containerregistry); no Docker daemon is required on the runner. The workflow logs into `ghcr.io` with the workflow's auto-issued `GITHUB_TOKEN` (so `permissions: packages: write` is mandatory in the workflow).
- Pushing the cask to a *different* repo (`richardwooding/homebrew-tap`) requires a Personal Access Token with content write on that tap, exposed as the repo secret `HOMEBREW_TAP_GITHUB_TOKEN`. The default `GITHUB_TOKEN` is scoped to the source repo only.
- The first-ever release also needs the `ghcr.io/richardwooding/file-search-on` package's visibility flipped to public manually in GitHub package settings — otherwise `docker pull` requires auth.

#### Local dry-run

```sh
goreleaser check                                          # validate the YAML
goreleaser release --snapshot --clean --skip=publish      # builds dist/, no push
```

The snapshot run will *try* to load OCI images into the local Docker daemon and fail if there isn't one — that's expected on a dev machine and irrelevant for CI. Add `--skip=ko` to bypass it. `dist/` is gitignored.

#### Cutting a release

```sh
git tag v0.1.0
git push origin v0.1.0
```

Then watch the **Release** workflow in GitHub Actions. To roll back a botched release: delete the tag (`git push origin :refs/tags/vX.Y.Z`), delete the GitHub Release in the UI, untag the ghcr.io image, revert the homebrew-tap commit. Cheaper to cut `vX.Y.Z+1` if the change is non-destructive.

### Markdown front-matter (primary feature)

`internal/content/frontmatter.go` is the parser. `splitFrontmatter(data []byte) (*Frontmatter, []byte)` is the only entry point — it returns the parsed metadata (or `nil` if absent) and the body bytes that the rest of `markdown.go` should treat as the document.

Detection is purely by the leading bytes: `---\n` ⇒ YAML, `+++\n` ⇒ TOML, `{` at byte 0 ⇒ JSON. There is intentionally no magic-byte fallback — if the first bytes don't match, `frontmatter_format` is `""` and every promoted variable holds its zero value. Malformed input degrades silently (returns nil + the original bytes) so a broken front-matter block doesn't make a file vanish from results.

Six keys are promoted to first-class CEL variables in `markdown.go`'s `Attributes`: `title`, `author`, `tags`, `categories`, `draft`, `date`. Anything else is reachable through `frontmatter.<key>`. Title precedence is *front-matter > H1*. `tags` and `categories` accept either a list or a bare string (which is wrapped) — that coercion lives in `stringListValue`. `date` accepts native `time.Time` (TOML) or several string layouts (`timeValue`); add new layouts there, not at call sites.

Map values are normalised through `normalizeMap`/`normalizeValue` so cel-go sees `map[string]any` end-to-end. yaml.v3 already does this for the top-level map, but nested maps from generic `any` paths can sometimes be `map[any]any`; the normaliser covers that.

When you change the promoted-variable set:

1. Update `markdown.go` `Attributes` to populate the new key in the returned map (with a zero-value fallback in the same default block — never leave a key undeclared).
2. Add a matching `cel.Variable(...)` in `celexpr.New` **and** a default in the activation map and a case in the `attrs.Extra` switch in `celexpr.Evaluate`. All three must move together; cel-go errors on undeclared variables at runtime.
3. Add an entry to `celexpr.Schema()` in `internal/celexpr/schema.go` (this drives both `--list` output and the MCP `list_attributes` tool — `printHelp` is now data-driven and needs no edits) and update the README front-matter table.
4. Add a test in `internal/content/frontmatter_test.go` that exercises the new promotion across at least one of the three formats.

### Adding a new content type

1. Create a new file in `internal/content/` implementing the `ContentType` interface and call `Register(&yourType{})` from `init()`.
2. If it introduces new attributes, declare matching `cel.Variable(...)` entries in `celexpr.New` **and** wire them in both the activation defaults map and the `attrs.Extra` switch in `Evaluate` (`internal/celexpr/evaluator.go`). Forgetting either side will produce CEL "no such attribute" errors at runtime. Add an `AttributeDoc` to the right slice in `celexpr.Schema()` so the new attribute shows up in `--list` and the MCP `list_attributes` tool.
3. If it's an image-family type, also extend the `strings.HasPrefix(contentTypeName, "image/")` branch logic in `BuildAttributes`. The registered-types listing in `--list` is generated from `content.DefaultRegistry().Types()`, so the new type appears there automatically.

### MCP server

`internal/mcpserver` is a thin adapter over the existing `search.Walk` and `celexpr.Schema()`. The `mcp` subcommand starts a stdio JSON-RPC server using the official Go SDK; nothing else in the binary changes when MCP mode is active.

Tool input/output structs (e.g. `SearchInput`, `SearchOutput`) live next to the handlers in `server.go`. JSON schemas are generated automatically from `json` and `jsonschema` struct tags by the SDK — don't hand-write the schema.

To add a new tool:

1. Define request/response structs with `json` + `jsonschema` tags.
2. Write a handler with signature `func(ctx, *mcp.CallToolRequest, In) (*mcp.CallToolResult, Out, error)`.
3. Call `mcp.AddTool(s, &mcp.Tool{...}, handler)` inside `New(...)`.
4. Add a test in `server_test.go` using `mcp.NewInMemoryTransports()` — that's how the existing tests drive the server in-process without a subprocess.

When changing the search surface, prefer adding inputs to the existing `search` tool over forking a new tool, so MCP clients see one entry point that mirrors the CLI.