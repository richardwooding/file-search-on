# Recipes — Source code

Source-code content types: `source/go`, `source/python`, `source/javascript`, `source/typescript`, `source/rust`, `source/c`, `source/cpp`, `source/java`, `source/ruby`, `source/swift`, `source/kotlin`, `source/shell`, `source/lua`, `source/elixir`, `source/clojure`, `source/haskell`, `source/ocaml`, `source/zig`. Umbrella boolean `is_source`.

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

## Out of scope

- **Shebang detection** for extensionless scripts (`~/bin/foo` containing `#!/usr/bin/env python3`). Detection is extension-only; a follow-up could add shebang routing, but it requires changes to the detector contract.
- **Cyclomatic complexity, AST-derived attributes** (function count, class count, import lists). These need per-language parsers — out of scope for v1.
- **String-aware comment classification.** A line containing `s = "//"` is treated as code (correct, since `s = "//"` doesn't start with `//`). A line whose code happens to start with `//` inside a string... is rare enough to ignore.
- **Generated-file detection.** Use name-based filters explicitly (see above).
- **Vendored / `node_modules` skip lists.** Use path-based filters explicitly.
