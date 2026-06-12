# Recipes — Source code

Source-code content types: `source/go`, `source/python`, `source/javascript`, `source/typescript`, `source/rust`, `source/c`, `source/cpp`, `source/java`, `source/ruby`, `source/swift`, `source/kotlin`, `source/scala`, `source/shell`, `source/lua`, `source/elixir`, `source/clojure`, `source/haskell`, `source/ocaml`, `source/zig`, `source/csharp`, `source/php`, `source/perl`, `source/r`, `source/ada`, `source/sql`, `source/vb`, `source/fortran`, `source/matlab`, `source/assembly`, `source/pascal`. Umbrella boolean `is_source`. Covers the Tiobe top 20 (May 2026) minus Scratch (a block-visual environment with binary files — not a text-source content type).

Hand-rolled. No third-party language-detection lib (no `go-enry`, no `linguist`). Detection is extension-based — see "Out of scope" below for the cases that surfaces. Line classification follows the cloc / tokei convention: blank lines are blank, lines starting with a comment marker are comment, everything else is code. Mixed lines (code with trailing comment) count as code.

## All-source triage

The umbrella query — every source file under a tree:

```sh
file-search-on 'is_source' -d ./internal
```

By language:

```sh
file-search-on 'is_source && language == "go"'      -d .
file-search-on 'is_source && language == "python"'  -d ~/Code
file-search-on 'is_source && language == "rust"'    -d ./crates
```

Multiple languages — CEL `in`:

```sh
file-search-on 'is_source && language in ["go", "rust", "zig"]' -d ~/Code
```

Tiobe top 20 (May 2026) — the new additions:

```sh
file-search-on 'is_source && language == "csharp" && loc > 200'   -d ./MySolution
file-search-on 'is_source && language == "php"'                    -d ./wordpress
file-search-on 'is_source && language == "r" && is_test_file'      -d ./analysis/tests
file-search-on 'is_source && language in ["sql", "matlab"]'        -d ./pipeline
```

## Filter by size (LOC, not bytes)

`loc` is non-blank, non-comment lines. Composes with the standard `size` for byte-level filtering:

```sh
# Files with > 500 lines of actual code.
file-search-on 'is_source && loc > 500' -d ./internal

# Tiny files — likely stubs or single-function modules.
file-search-on 'is_source && loc <= 10' -d ./internal

# Top-10 longest Go files in a project, sorted descending.
file-search-on 'is_source && language == "go"' -d . -o json |
  jq -s 'sort_by(-.loc) | .[0:10] | .[] | "\(.loc)\t\(.path)"'
```

## Comment density

`comment_loc` and `blank_loc` round out the line-counting trio. Composing them gives a comment-density signal:

```sh
# Heavily-commented Python (more comment lines than code).
file-search-on 'is_source && language == "python" && comment_loc > loc' -d ~/Code

# Files with NO comments — possibly machine-generated or rushed.
file-search-on 'is_source && loc > 100 && comment_loc == 0' -d ./src

# High blank-to-code ratio (sparse code, lots of vertical space).
file-search-on 'is_source && loc > 50 && blank_loc * 2 > loc' -d ./internal
```

Express comment density with arithmetic — CEL has full int / double support:

```sh
# > 25% comment density on long files (loc > 100).
file-search-on 'is_source && loc > 100 && comment_loc * 4 > loc' -d ./internal
```

## Generated-file filtering

`file-search-on` doesn't auto-detect generated files — language-detection libraries do this with heuristics that drift over time. Filter explicitly by name when needed:

```sh
# Hand-written Go, excluding protobuf-generated files.
file-search-on 'is_source && language == "go" && !name.endsWith(".pb.go")' -d .

# Skip mock files.
file-search-on 'is_source && language == "go" && !name.startsWith("mock_")' -d ./internal

# Skip vendored dependencies.
file-search-on 'is_source && !path.contains("/vendor/") && !path.contains("/node_modules/")' -d .
```

## Cross-language scans

Aggregate counts across the whole project — combine with `jq`:

```sh
# Total LOC by language.
file-search-on 'is_source' -d . -o json |
  jq -s 'group_by(.language) | map({language: .[0].language, files: length, loc: (map(.loc) | add)}) | sort_by(-.loc)'

# Average comment density by language (high = well-documented codebase).
file-search-on 'is_source && loc > 0' -d . -o json |
  jq -s 'group_by(.language) | map({language: .[0].language, avg_density: ((map(.comment_loc) | add) / (map(.loc) | add))}) | sort_by(-.avg_density)'
```

## Test files vs implementation

Use name-matching to split tests from implementation:

```sh
# Test files only (Go convention).
file-search-on 'is_source && language == "go" && name.endsWith("_test.go")' -d .

# Non-test Go files with > 200 LOC (focus on big implementation files).
file-search-on 'is_source && language == "go" && !name.endsWith("_test.go") && loc > 200' -d .

# Python tests.
file-search-on 'is_source && language == "python" && (name.startsWith("test_") || name.endsWith("_test.py"))' -d .
```

## Useful output formats

```sh
# Path + language + loc + comment_loc, tab-separated.
file-search-on 'is_source' --format '{{.Path}}\t{{.Language}}\t{{.LOC}}\t{{.CommentLOC}}' -d ./internal

# Bare paths for xargs (e.g. run gofmt over every Go file > 200 LOC).
file-search-on 'is_source && language == "go" && loc > 200' -d . -o bare |
  xargs gofmt -l

# JSON for jq pipelines — biggest source files across the whole tree.
file-search-on 'is_source' -d . -o json |
  jq -s 'sort_by(-.loc) | .[0:20] | .[] | "\(.loc)\t\(.language)\t\(.path)"'
```

## Symbols + imports (17 languages)

Three list-valued attributes — `functions`, `type_names`, `imports` — give structured answers to the universal "where is X defined?" and "which files use Y?" questions. Go uses the stdlib AST; the other 15 languages — Python / Java / C# / PHP / Perl / R / MATLAB / Scala / Rust / TypeScript / JavaScript / Ruby / Swift / Kotlin / C / C++ — use an embedded pure-Go tree-sitter grammar (accurate parse, no regex since #365). Other languages leave these arrays empty.

```sh
# Where is ProcessOrder defined?  (define-X)
file-search-on 'is_source && "ProcessOrder" in functions'

# Which files import net/http?  (use-X)
file-search-on 'is_source && "net/http" in imports'

# Files that declare a Handler type
file-search-on 'is_source && "Handler" in type_names'

# Python: where is the DataLoader class?
file-search-on 'is_source && language == "python" && "DataLoader" in type_names'

# Java: files importing Spring Web
file-search-on 'is_source && "org.springframework.web" in imports' -d ./service

# C#: files importing the EF Core DbContext namespace
file-search-on 'is_source && language == "csharp" && "Microsoft.EntityFrameworkCore" in imports'

# C#: every controller class
file-search-on 'is_source && language == "csharp" && type_names.exists(t, t.endsWith("Controller"))'

# PHP: every file that uses Symfony's HttpFoundation
file-search-on 'is_source && language == "php" && imports.exists(i, i.startsWith("Symfony\\Component\\HttpFoundation"))'

# PHP: classes implementing __construct (DI-shaped wiring)
file-search-on 'is_source && language == "php" && "__construct" in functions'

# Perl: every module using Moose
file-search-on 'is_source && language == "perl" && "Moose" in imports'

# Perl: find where a package is declared
file-search-on 'is_source && language == "perl" && "Acme::Widget" in type_names'

# R: every script that uses ggplot2
file-search-on 'is_source && language == "r" && "ggplot2" in imports'

# R: where is the Animal R6 class defined?
file-search-on 'is_source && language == "r" && "Animal" in type_names'

# MATLAB: every script defining a classdef
file-search-on 'is_source && language == "matlab" && type_names.size() > 0'

# MATLAB: find a function by name across return-value shapes
# (function out = name(...) / function [a,b] = name(...) all match)
file-search-on 'is_source && language == "matlab" && "computeFFT" in functions'

# Scala: every file importing cats-effect
file-search-on 'is_source && language == "scala" && "cats.effect.IO" in imports'

# Scala: where is the OrderService object / class defined?
file-search-on 'is_source && language == "scala" && "OrderService" in type_names'

# Scala: case classes + traits across a domain model
# (def operator methods like "+" are captured in functions too)
file-search-on 'is_source && language == "scala" && type_names.size() > 0' -d ./domain

# Who calls a function? (references = call sites; Go + tree-sitter languages)
file-search-on who-calls ProcessOrder -d .
# …or as a CEL filter via the `references` attribute:
file-search-on 'is_source && "ProcessOrder" in references'

# What does a function call? (forward direction; per-function attribution)
file-search-on calls ProcessOrder -d .

# Maintenance hotspots: files whose worst function is gnarly (file-level filter)
file-search-on 'is_source && language == "go" && max_complexity > 15'
# …drill into the worst individual functions (per-function ranking):
file-search-on complexity 'is_source && language == "go"' --top 20

# Candidate dead code — defined but never called. HEURISTIC: pair with
# !is_test_file to drop test-runner-invoked funcs; review, don't auto-delete.
file-search-on dead-code 'is_source && language == "go" && !is_test_file' -d .

# Rust: every file importing a crate (tree-sitter-extracted)
file-search-on 'is_source && language == "rust" && imports.exists(i, i.startsWith("serde"))'

# TypeScript: where is a React component / class defined?
file-search-on 'is_source && language == "typescript" && "UserService" in type_names'

# C / C++: files that include a given header
file-search-on 'is_source && language in ["c", "cpp"] && "vector" in imports'

# Hotspots: source files with many types AND many functions
file-search-on 'is_source && type_names.size() >= 3 && functions.size() >= 10'

# Refactor target: file with 1 function but many imports (import-heavy glue code)
file-search-on 'is_source && functions.size() == 1 && imports.size() > 10'
```

Repeat queries on unchanged trees are sub-second — symbols cache alongside the other attributes via the bbolt index, validated against `(size, mtime)`.

## Cross-file code graph (`imported_by` / `find_definition` / `code_graph`)

The `imports` / `functions` / `type_names` attributes above are *per-file*. Inverting them across a tree answers the *relationship* questions a single-file filter can't: **who imports X?**, **where is Y defined?**, **what does this codebase depend on most?** Three CLI subcommands (and the matching MCP tools) build that project-wide graph from the same walk + index:

```sh
# Reverse dependency — every file that imports a module ("who depends on X?").
file-search-on imported-by github.com/spf13/cobra -d .

# Prefix mode — everything under an internal package path.
file-search-on imported-by github.com/myorg/app/internal --mode prefix -d .

# Where is a function or type defined? (exact name; symbol-aware, not text grep)
file-search-on find-definition ServeHTTP --kind function -d .
file-search-on find-definition OrderService --kind type -d ./src

# Project-wide overview — import hubs, most-coupled files, duplicate definitions.
file-search-on code-graph 'is_source && language == "go"' --top 10 -d .

# JSON for tooling / dashboards.
file-search-on code-graph -o json -d . | jq '.import_hubs[:5]'
```

`imported_by` is accurate for every language whose imports are extracted (Go via AST; Python / Java / C# / PHP / Perl / R / MATLAB / Scala via the import-shape extractors). `find_definition` is limited to the languages with symbol extraction — for the rest, fall back to `find-matches`. Genuine call-graph ("who *calls* Y?") is still out of scope (see below) — it needs call-site extraction.

## Find copy-pasted functions (`duplicate-functions`)

`near-duplicates` compares whole *files*; `duplicate-functions` compares individual **functions**, so it catches a 20-line helper that's been copy-pasted into otherwise-distinct files. It splits each source file into its functions (the same per-function spans `complexity` uses), SimHashes each body, and clusters the near-identical ones — your extract-this-helper worklist.

```sh
# Copy-pasted Go functions across the repo (default threshold 0.92, min 5 lines).
file-search-on duplicate-functions 'is_source && language == "go"' -d .

# Looser match (catch structurally-similar, not just near-identical); ignore short funcs.
file-search-on duplicate-functions -d ./src --threshold 0.85 --min-lines 8

# JSON for tooling — each member carries {path, symbol, start_line, end_line, lines}.
file-search-on duplicate-functions -o json -d . | jq '.groups[0].members'
```

Each group's members list a `path` + `symbol` + line range — feed `[start_line, end_line]` to `lines` (or the `read_lines` MCP tool) to see the code. It's a heuristic (SimHash matches token/structure shape), so review a cluster before extracting — functions that share a skeleton but differ in intent can land together.

## Find untested code (`test-gaps`)

`test-gaps` reports production files whose functions are never referenced from a test — a coverage-hole finder that needs no `go test -cover` run or coverage profile. It reuses the cross-file reference graph: a function counts as *tested* when its name appears as a reference inside any `is_test_file`.

```sh
# Untested Go functions across the repo (fully-untested files ranked first).
file-search-on test-gaps 'is_source && language == "go"' -d .

# JSON — each gap carries {path, function_count, untested_count, untested_functions, fully_untested}.
file-search-on test-gaps -o json -d ./internal | jq '.gaps[] | select(.fully_untested)'
```

It's a **direct-reference heuristic**, not a coverage report: a function exercised only transitively (a test calls `A`, which calls `B`, but no test names `B`) shows as untested, and same-name collisions can mislead — treat the output as review candidates. For precise line/branch coverage, drive a real coverage profile. Works across the reference-extraction languages (Go + Rust / TS / JS / Ruby / Swift / Kotlin / C / C++), including Rust's inline `#[cfg(test)]` tests, which a filename-sibling approach would miss.

## Blast radius before a refactor (`impact`)

`who-calls` answers one hop ("who calls `Foo` directly?"); `impact` returns the **transitive closure** — every function that reaches `Foo` through the call graph, with the depth each was found at. Run it before changing a signature or behaviour to see what you might break.

```sh
# Everything that (in)directly calls BuildCodeGraph, with depth.
file-search-on impact BuildCodeGraph -d ./internal

# Just the direct callers (same set as who-calls, but symbol-level).
file-search-on impact ServeHTTP --max-depth 1 -d .
```

Output is a list of `{symbol, depth, defined-in}` ordered shallowest-first. Same name-based caveats as `who-calls` / `calls` — interface / reflection dispatch and same-name collisions can over- or under-count. The import-level equivalent ("what transitively imports this *file*") isn't available yet — it needs package resolution the graph doesn't carry.

## Filtering out generated code

```sh
# Production code only — exclude tests AND machine-generated files.
file-search-on 'is_source && !is_test_file && !is_generated_code'

# Just the generated files (so you can audit what they contain or
# adjust your tooling).
file-search-on 'is_source && is_generated_code'

# Per-language generated detection — protoc-gen-go outputs, easyjson,
# mockery, etc. all carry the canonical `// Code generated ... DO NOT
# EDIT.` marker:
file-search-on 'is_source && language == "go" && is_generated_code'
```

Detection scans the first ~20 lines of each source file for known generator markers:

| Language | Marker (substring) |
|---|---|
| Go | `// Code generated ` (the official `cmd/go` convention; the trailing `DO NOT EDIT.` is informational) |
| Python | `# Generated by`, `# autogenerated`, `# @generated`, `# DO NOT EDIT` |
| Java / Kotlin / Scala | `// Generated by`, `@generated`, `@javax.annotation.Generated`, `// DO NOT EDIT` |
| C# | `// <auto-generated>` (Microsoft's official convention) + the cross-language fallbacks |
| JS / TS / Rust / PHP | `// @generated` (Facebook's cross-language convention) + `// Generated by` / `// DO NOT EDIT` |
| Ruby / Shell | `# Generated by`, `# @generated`, `# DO NOT EDIT` |

Detection is **line-granular** and **substring-based** (no regex), so the per-file overhead is one cheap pass over the file's first 20 lines. The first marker hit short-circuits the rest. Caveat: a hand-written file with a test fixture string literal containing `// Code generated …` will also fire — compose with `!is_test_file` for the cleaner production-code filter. Issue #276.

## Git-aware queries

Filesystem `mod_time` is meaningless on a freshly cloned repo (every file's mtime is checkout time). Pass `--with-git` (CLI) or `with_git: true` (MCP) and use the `git_*` CEL attributes for repo-aware filtering.

```sh
# Files I (or any author) most recently edited — independent of checkout time.
file-search-on 'is_source && git_last_commit_time > timestamp("2026-05-01T00:00:00Z")' \
  --with-git -d . --sort git_last_commit_time --order desc --limit 20

# Hot files — high churn over the repo's history.
file-search-on 'is_source && git_commit_count > 50' \
  --with-git -d . --sort git_commit_count --order desc --limit 10

# Production code only — skip untracked scratch, generated artefacts, tests.
file-search-on 'is_source && is_git_tracked && !is_test_file' \
  --with-git -d .

# What did a specific author last touch?
file-search-on 'is_source && git_last_commit_author == "Alice"' \
  --with-git -d . --sort git_last_commit_time --order desc

# Recently-added files (first appeared in HEAD's history after May 2026).
file-search-on 'is_source && git_first_seen > timestamp("2026-05-01T00:00:00Z")' \
  --with-git -d .

# Find files matched by .gitignore (build artefacts an agent should skip).
file-search-on 'is_git_ignored' --with-git -d .
```

One `git log` pass per walk root up front; per-file lookups are free. Silent no-op when the root isn't inside a git working tree or when `git` isn't on PATH — the rest of the query still runs, the `git_*` fields just stay zero. Issue #271.

When the MCP server is started with `--warm`, the git cache is primed at startup alongside the attribute cache — so the first `with_git=true` search call is free. Subsequent calls reuse the same cache across the server's lifetime, paying only a `git rev-parse HEAD` (~3-5ms) to confirm the HEAD hasn't moved. After a `git commit` / `git checkout`, the cache rebuilds automatically on the next call. CLI one-shots (`file-search-on '…' --with-git`) don't benefit from the pool — each invocation builds and tears down its own cache.

## Out of scope

- **Shebang detection** for extensionless scripts (`~/bin/foo` containing `#!/usr/bin/env python3`). Detection is extension-only; a follow-up could add shebang routing, but it requires changes to the detector contract.
- **Symbol extraction for languages beyond the 17 supported** (Go / Python / Java / C# / PHP / Perl / R / MATLAB / Scala + tree-sitter's Rust / TypeScript / JavaScript / Ruby / Swift / Kotlin / C / C++). Lua / Haskell / Elixir / Zig / etc. leave `functions` / `type_names` / `imports` empty today — adding one is a small tree-sitter query addition per language. (Kotlin `interface` declarations parse imperfectly in the bundled grammar — classes/objects/functions are captured.)
- **Call graph** ("who *calls* Y?") and symbol-level dead code — needs call-site extraction; now feasible on the tree-sitter foundation.
- **Receiver-qualified Go methods** (e.g. `Handler.ServeHTTP` vs bare `ServeHTTP`). Bare names are what agents look up; matching `"ServeHTTP" in functions` works. A future `methods []string` could surface receiver pairs.
- **Call graph** ("who *calls* function Y?") and symbol-level dead-code. Call sites aren't extracted today, so neither is derivable — they want the tree-sitter upgrade path above. (The *cross-file import + definition graph* — "who imports X?", "where is Y defined?" — is now built; see [Cross-file code graph](#cross-file-code-graph-imported_by--find_definition--code_graph) below.)
- **Documentation extraction** (docstrings, godoc lines per function). Worth a follow-up.
- **String-aware comment classification.** A line containing `s = "//"` is treated as code (correct, since `s = "//"` doesn't start with `//`). A line whose code happens to start with `//` inside a string... is rare enough to ignore.
- **Generated-file detection.** Use name-based filters explicitly (see above).
- **Vendored / `node_modules` skip lists.** Use path-based filters explicitly.
