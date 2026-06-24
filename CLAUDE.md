# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

`file-search-on` is a Go CLI that recursively searches a directory and matches files against a [CEL](https://github.com/google/cel-spec) expression evaluated over file metadata and content-type-specific attributes (e.g. `is_pdf && page_count > 10`, `is_markdown && word_count > 500`).

Module: `github.com/richardwooding/file-search-on`. Toolchain: Go 1.26.2.

## Commands

```sh
go build ./...                                  # build all packages
go build -o file-search-on ./cmd/file-search-on # build the CLI binary
go test ./...                                   # run all tests
go test -race -coverprofile=coverage.out ./...  # what CI runs
go test ./internal/celexpr -run TestEvaluator   # run a single test (regex match)
go vet ./...
go fix -diff ./...                              # preview Go 1.26 modernizers
go fix ./...                                    # apply them
golangci-lint run                               # CI uses `latest` version
```

**Tree-sitter grammars (binary size).** `internal/content/source_symbols_treesitter.go` extracts symbols for every source language except Go (which keeps the stdlib AST) — Rust / TypeScript / JavaScript / Ruby / Swift / Kotlin / C / C++ plus Python / Java / C# / PHP / Perl / R / MATLAB / Scala (migrated off regex in #365) — via the pure-Go `github.com/odvcencio/gotreesitter` runtime. A plain `go build` / `go test` embeds **all ~206 grammars** (~+22 MB) — fine for dev. Release builds (`.goreleaser.yaml`) pass `grammar_subset` + one `grammar_subset_<lang>` tag per supported language so only those embed (~+13 MB for the 16). To reproduce a release-equivalent local build:

```sh
go build -tags 'grammar_subset grammar_subset_rust grammar_subset_typescript grammar_subset_javascript grammar_subset_ruby grammar_subset_swift grammar_subset_kotlin grammar_subset_c grammar_subset_cpp grammar_subset_python grammar_subset_java grammar_subset_c_sharp grammar_subset_php grammar_subset_perl grammar_subset_r grammar_subset_matlab grammar_subset_scala' -o file-search-on ./cmd/file-search-on
```

When adding/removing a tree-sitter language, keep three places in sync: the `tsDetectFile` map in `source_symbols_treesitter.go`, `symbolExtractorWired` in `sourcetype.go`, and the `tags:` list in `.goreleaser.yaml`.

CI runs `go fix ./... && git diff --exit-code` after build, so any unapplied modernizer fails the pipeline. Re-run `go fix` until idempotent — applying one fix can unlock another. `go tool fix help` lists fixers.

Run the CLI:

```sh
./file-search-on 'is_markdown && word_count > 100' -d ./docs
./file-search-on --list   # list supported attributes & registered content types
./file-search-on mcp      # serve MCP over stdio
```

Empty `Expr` defaults to `"true"` (matches all files). Worker count defaults to `runtime.NumCPU()` when `-w` is 0 or unset.

### Fuzz testing

High-risk parsers (frontmatter, MP3 ID3v2 + Xing, MKV EBML, MP4 box walker, office/EPUB/email/PDF body extractors, CEL compile, index gob decoder, every hand-rolled binary header walker) have `FuzzXxx` targets in `*_fuzz_test.go`. (The C2PA JUMBF/COSE/RFC 3161 parser was extracted to `github.com/richardwooding/c2pa`, which carries its own fuzz targets.)

- **Regular CI** (`ci.yml`): `go test ./...` runs each target's seed corpus — a seed panic fails the build.
- **Scheduled fuzz** (`fuzz.yml`): 5 min/target nightly at 03:30 UTC. Crashes land in `testdata/fuzz/<FuzzName>/<hash>` — commit those to lock in regression coverage. Manual `workflow_dispatch` accepts a `fuzztime` input for bug-hunt sessions.

Run locally:

```sh
go test -run=FuzzSplitFrontmatter ./internal/content/                # seed corpus only
go test -fuzz=FuzzSplitFrontmatter -fuzztime=30s ./internal/content/ # mutate
```

When adding a new fuzz target: put `FuzzXxx(f *testing.F)` in an internal `*_fuzz_test.go` (`package <pkg>`, not `_test`), seed via `f.Add(...)` with valid + pathological inputs, the body must never panic, and add a matrix entry to `.github/workflows/fuzz.yml`.

## Architecture

Internal packages compose the pipeline. The first three are tightly coupled by the `FileAttributes` shape; `internal/index` is an optional cache plumbed through `BuildAttributesWith` and `search.Options.Index`. The directory-granularity project-type detection (the counterpart to `internal/content`) lives in the extracted module `github.com/richardwooding/projectdetect`.

**Robustness invariants (issue #337, pinned by tests — see `internal/search/doc.go`):** (1) *Graceful degradation* — a detected file whose `Attributes()` errors/panics still appears in `search 'true'` with its base attributes; never silently dropped. (2) *Cancellation* — every orchestrator and every hand-rolled parser loop checks `ctx.Err()` at entry and inside any unbounded loop, returning the ctx error (streaming) or `Cancelled=true` (buffered aggregators) promptly. `TestWalk_GracefulDegradation_EveryType` and `TestOrchestrators_PreCancelledCtxReturnPromptly` fail the suite if either regresses — uphold both when adding a content type or an orchestrator/loop.

- **`internal/content`** — pluggable content-type detection. Each type implements `ContentType` (`Name`, `Extensions`, `MagicBytes`, `Attributes(ctx, path)`) and self-registers in `init()` via `Register(...)`. `Registry.Detect(path)` tries exact-basename match (optional `FilenameMatcher` interface), then longest-suffix extension match, then magic-byte sniffing on the first 512 bytes. `Attributes` is called per matching file and returns `map[string]any`; implementations check `ctx.Err()` at entry and inside any unbounded loop. Covers: markdown, json/xml/html/yaml/toml, pdf, image variants (EXIF via `imagemeta`), text/csv, epub, office (DOCX/XLSX/PPTX/ODT), audio (tags via `dhowden/tag`, playback via hand-rolled per-format), video (MP4/MKV/AVI), archive (ZIP/TAR/TAR.GZ/GZIP), binary (ELF/Mach-O/PE via `debug/*`), email (RFC 5322 + mbox), source (19 languages), notebooks (Jupyter/Zeppelin), disk images (DMG/ISO/VHD/VHDX/VMDK/QCOW2/WIM), install packages (PKG/DEB/RPM/AppImage), VM bytecode (Java class/Python pyc/WASM), science data (FITS/VOTable/HDF5/PDS3+4/CDF), database (SQLite), font (SFNT family), and repo/build/system metadata files. Bodies are extracted on demand by `internal/content/body.go` (`ExtractBody`) — office walks ZIP+XML, email walks MIME, PDF uses `ledongthuc/pdf` wrapped in defer/recover. C2PA / Content Credentials (the `is_c2pa` / `c2pa_*` image attributes) are read by the extracted `github.com/richardwooding/c2pa` module (`c2pa.Read` in `imagetype.go`) — pure-Go, read-only, unverified. `setTypeFlags` in `internal/celexpr/typeflags.go` is the single source of truth for content-type-name → `is_X` predicate dispatch.

- **`internal/celexpr`** — wraps `cel-go`. `New(expr)` declares a fixed schema of CEL variables (union of common file attrs + every type-specific attribute) and compiles the program once. `BuildAttributesWith(ctx, fsys, fsPath, displayPath, registry, opts)` is the primary attribute-extraction entry point: runs `os.Stat`, optionally consults `opts.Index` (a `(size, mtime)`-validated cache), calls `registry.Detect` + `ContentType.Attributes` only on cache miss, and packs into `FileAttributes`. `Evaluate` flattens `FileAttributes` into a CEL activation, supplying zero values for type-specific vars (cel-go errors on undeclared/unbound). `Schema()` (in `schema.go`) returns the structured docs that drive both `--list` and the MCP `list_attributes` tool — the only place attribute docs are hard-coded. Symlink awareness lives in `probeSymlink` / `applySymlinkInfo`.

- **`internal/search`** — `WalkStream` is the orchestrator: compiles CEL once, fans out via `filepath.WalkDir` producer → buffered channel → N workers (`Workers`, default `NumCPU`). Cancellation honoured at producer-send, worker-receive, and inside per-file `Attributes` calls. Hand-rolled binary parsers thread `ctx` through inner loops AND shared `walkBoxes`/`descendBoxes`/`walkEBML` primitives. Producer honours `Options.Excludes` (basename globs) and `Options.RespectGitignore`. `Walk` is the buffered wrapper that applies `Sort`/`Order`/`Limit` post-collection via `SortAndLimit`. `Options.IncludeBody` (cap via `BodyMaxBytes`, default 1 MiB) reads/extracts the file body and surfaces it as the `body` CEL variable so `body.contains("X")` / `body.matches("...")` work. Sibling entry points: `ComputeStats` (histogram bucketed by `Options.GroupBy`, with detector-only fast path when CEL is trivial), `ReadLines`, `FindDuplicates` (sha256, size pre-buckets), `FindNearDuplicates` (SimHash via the external `github.com/richardwooding/fingerprint`), `WalkArchiveEntries` + `ReadFileInArchive` (search inside ZIP/TAR/TAR.GZ/GZIP via `IterateArchive` + `NewSingleFileFS`), `FindMatches` (line-level regex with before/after context). Presets live in `presets.go`; partial-result suggestions in `suggestions.go`.

- **`internal/mcpserver`** — thin adapter over the SDK. `New(version, idx, defaultTimeout)` registers tools: `search`, `read_attributes`, `read_lines`, `stats`, `find_duplicates`, `find_near_duplicates`, `list_archive_contents`, `read_file_in_archive`, `find_matches`, `list_attributes`, `index_stats`, `detect_project` / `find_projects` / `resolve_project_for_path`, `list_presets` / `query_preset`. Every output struct embeds `CommonOutput` (carries `server_version`). Every call wraps ctx with `defaultTimeout`; on timeout the search/stats tools return partial results with `cancelled=true` + `cancellation_reason`, NOT an error. Three transports: stdio (`Run`), Streamable HTTP (`RunHTTP`), deprecated SSE (`RunSSE`).

- **`github.com/richardwooding/projectdetect`** (extracted module, was `internal/projecttype`) — directory-granularity counterpart to `internal/content`. Each `ProjectType` has `Indicator`s (`HasFile` / `HasGlob` / `CELExpr` over `files` + `subdirs`). `projectdetect.Detect(fsys, dir)` returns every matching type; `Find(ctx, root, opts)` walks recursively. `ProjectResolver` answers "what project does this file belong to?" by walking up from the file's parent (cached per-dir). Surfaced as `project_types` / `project_type` CEL vars when `search.Options.ResolveProjects` is set. `CollectBuildExcludes` drives `--prune-build-artefacts`. 18 built-ins; custom types loadable via YAML. **CEL indicators are opt-in in the library** (the base package has no cel-go dependency) — `cmd/file-search-on/main.go` blank-imports `github.com/richardwooding/projectdetect/celindicators` to enable `cel:` indicators in custom YAML. To bump it: `go get github.com/richardwooding/projectdetect@latest`.

- **`internal/index`** — optional cache of `(content_type, attributes)` keyed by absolute path + validated by `(size, mtime)`. Two implementations: in-memory (`NewMemory`, MCP auto-on) and on-disk (`Open` / `OpenWith`, single-file bbolt with `attrs_v1` / `bodies_v1` / `body_access_v1` / `meta` buckets, used by CLI `--index-path`). Encoding is gob. Body cache has its own per-entry + total-size caps with FIFO eviction. Stats counters drive the CLI footer line and the MCP `index_stats` tool.

CLI (`cmd/file-search-on/main.go`) uses `kong`. `main()` builds a cancellable ctx via `signal.NotifyContext` so Ctrl-C / SIGTERM shut down cleanly. The single wire shape `search.Match` (in `internal/search/match.go`) is shared by CLI JSON output and the MCP `search` / `read_attributes` tools via `MatchFrom`. The CLI and MCP surfaces are at near-total parity: nearly every MCP tool has a CLI subcommand counterpart (and vice versa). The `validate` (wraps `celexpr.ValidateExpr`) and `index-stats` (wraps `index.Index.Stats()`) subcommands are the CLI counterparts of the MCP `validate_expr` / `index_stats` tools — keep both sides in sync when changing either. Direction that does NOT mirror: `organize` / `playground` / `config-paths` / `hash-set` / `embed warm` are CLI-only with no MCP tool.

### Releases

Releases are tag-driven. Pushing a `v*` tag triggers `.github/workflows/release.yml`, which runs **GoReleaser v2** (pinned `~> v2`) and produces three artifacts:

1. **GitHub Release** with archives for `linux`/`darwin`/`windows` × `amd64`/`arm64` + `checksums.txt`. Binaries stamped with `version`/`commit`/`date` via `-ldflags -X`.
2. **OCI image** at `ghcr.io/richardwooding/file-search-on:<version>` (and `:latest` for non-prerelease tags), built by `ko`. Linux only — amd64 + arm64 manifests. Base: `cgr.dev/chainguard/static`.
3. **Homebrew cask** committed to `richardwooding/homebrew-tap` at `Casks/file-search-on.rb`. Install: `brew install richardwooding/tap/file-search-on`.

Notes for future agents:

- Config uses `homebrew_casks`, not `brews` (deprecated in GoReleaser v2.10).
- ko publishes via `go-containerregistry`; no Docker daemon needed on the runner. `permissions: packages: write` is mandatory.
- The cask push to `richardwooding/homebrew-tap` needs a PAT (`HOMEBREW_TAP_GITHUB_TOKEN`); the default `GITHUB_TOKEN` is scoped to the source repo only.

Local dry-run: `goreleaser check` then `goreleaser release --snapshot --clean --skip=publish`. Snapshot will try to load OCI images into local Docker and fail without it — add `--skip=ko` to bypass.

Cutting a release: `git tag vX.Y.Z && git push origin vX.Y.Z`, then watch the Release workflow. The `cut-release` skill automates this end-to-end including verification. To roll back: delete the tag, delete the GitHub Release, untag ghcr.io, revert the homebrew-tap commit — usually cheaper to cut `vX.Y.Z+1`.

### Adding a new content type

Use the `add-content-type` skill — it knows the exact files to touch. Summary:

1. Create a file in `internal/content/` implementing `ContentType` and call `Register(&yourType{})` from `init()`. For exact-name dispatch (e.g. `package.json`, `Dockerfile`), also implement `Filenames() []string`.
2. If it introduces new attributes, use the `extend-cel-schema` skill — it covers the four call sites that must move together (`cel.Variable` declarations, activation defaults, `attrs.Extra` switch, `Schema()`). cel-go errors at runtime, not compile time, when these drift.
3. Add a `case` for the new content-type name in `setTypeFlags` (`internal/celexpr/typeflags.go`) — and, if it belongs to a family (`build/*`, `repo/*`, `manifest/*`, `system/*`, etc.), rely on the matching prefix-`if` block at the bottom.
4. For image-family types, extend the `strings.HasPrefix(contentTypeName, "image/")` branch in `BuildAttributes`. For office docs, register as `office/<format>` — the prefix branch sets `is_office` automatically and `dublincore.go`'s `readZipDublinCore` handles core props.
5. **Update `README.md`** — add the new attribute(s) to the matching family table under *Available attributes*.
6. **Update `examples/`** — add a recipe to the matching `examples/*.md` file. If the new feature closed a "Known limitations" / "tracked in #X" note, delete the stale note in the same PR.

README and `examples/` are NOT auto-generated — they drift if you skip them. v0.10.0 shipped 9 new attributes that all three programmatic surfaces (`--list`, `list_attributes`, `search.Match`) picked up correctly; every one was missing from the README + examples until follow-up sweeps (#61, #63).

### Adding a CEL function

CEL functions are wired in `internal/celexpr/functions.go`. Four call sites must move together: the algorithm impl, the `ref.Val` binding, the `cel.Function(...)` entry in `fuzzyFunctions()`, and the `FunctionDoc` in `celexpr.Schema()`. cel-go binds at runtime, so missing any of them surfaces as a CEL "undeclared reference" error at expression compile time, not at Go build time.

1. Implement the algorithm as an exported pure-Go function so it's unit-testable without going through CEL.
2. Write the binding that adapts `ref.Val` arguments via `.Value().(string)` / `.Value().(int64)` and wraps the result via `types.Int(...)` / `types.String(...)` / `types.Double(...)` / `types.DefaultTypeAdapter.NativeToValue(...)`.
3. Add the `cel.Function(name, cel.Overload(...))` entry to `fuzzyFunctions()`.
4. Add a `FunctionDoc` to `celexpr.Schema()`.
5. Optionally update the MCP `search` tool's `Description` if the new function is significant.
6. Add unit tests for the algorithm + a CEL-level integration test in `TestEvaluateFuzzyFunctions` (table-driven).
7. Update `README.md` (*Built-in functions* table) and `examples/` (at least one recipe under `examples/fuzzy-search.md` or thematic equivalent).

### MCP server

Use the `add-mcp-tool` skill when adding a tool. Summary: define request/response structs with `json` + `jsonschema` tags (schemas are auto-generated by the SDK — don't hand-write), write a handler with signature `func(ctx, *mcp.CallToolRequest, In) (*mcp.CallToolResult, Out, error)`, call `mcp.AddTool(s, &mcp.Tool{...}, handler)` inside `New(...)`, and add a test in `server_test.go` using `mcp.NewInMemoryTransports()`. When changing the search surface, prefer adding inputs to the existing `search` tool over forking a new one so MCP clients see one entry point that mirrors the CLI. Every output struct must embed `CommonOutput` so the response carries `server_version` — the regression test in `server_version_test.go` will fail CI if you forget.
