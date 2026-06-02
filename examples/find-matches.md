# Line-level regex matching

The `find-matches` subcommand (and matching MCP `find_matches` tool) scans text files for an RE2 regular expression and reports every hit at the **line level**, with optional before/after context windows. Combines a CEL pre-prune (same vocabulary as `search`) with a line-level regex scan — pick candidate files cheaply by type and attribute, then run the regex only on what's left.

## When to reach for this vs alternatives

| Question | Tool |
| --- | --- |
| "What files mention X?" — paths only | [`grep -l`](https://www.gnu.org/software/grep/) or `file-search-on search ... --body` |
| "Show me every line mentioning X with context" — file + line + surrounding | **`find-matches`** |
| "What's the file count by language?" — aggregate | `stats --group-by language` |
| "Find files containing X AND with a typed attribute" — same call | **`find-matches`** with `--expr` |

`find-matches`'s edge over plain `grep` is the CEL pre-prune: `is_source && language == "go" && loc > 200` filters by content type and typed attributes before the regex runs. Plain `grep` doesn't know what a "Go source file" is — it only knows about paths and bytes.

## How it works

Two-pass like `find_duplicates`:

1. **Walk + CEL filter.** Walks the tree, extracts attributes, applies `--expr` (default: every file). Candidates are filtered to text content types (`markdown` / `text` / `html` / `csv` / `json` / `xml` / `source/*`). Binary families (image / audio / video / archive / binary / office / epub / email) are silently dropped — line-scanning them would produce noise.
2. **Per-file line scan.** Each candidate is opened and read with a `bufio.Scanner` capped at 64 KiB per line (pathological long lines are truncated). For each line, the compiled RE2 regex runs; on hit the line is recorded with its 1-indexed line number, the configured number of trailing context lines lazy-filled from subsequent reads, and the configured number of leading lines pulled from a ring buffer.

## CLI

```sh
# All TODOs in source files, with 2 lines of context either side
file-search-on find-matches '(?i)\bTODO\b' -d ./src --expr 'is_source' -C 2

# Case-insensitive, word-boundary, only Go files
file-search-on find-matches '(?i)\bunsafe\b' --expr 'is_source && language == "go"'

# Drafts mentioning a topic, with 1 line before / 3 lines after
file-search-on find-matches '\btransformer\b' --expr 'is_markdown && draft' -B 1 -A 3

# JSON for piping into jq — same wire shape as the MCP tool
file-search-on find-matches '\bAPI\b' --expr 'is_markdown' -o json | jq '.matches[] | {path, line}'

# Cap matches per file (handy when one file has hundreds of hits)
file-search-on find-matches '\bTODO\b' --max-matches-per-file 3

# Prune common build-artefact dirs in one flag
file-search-on find-matches '\berr\b' --expr 'is_source' --prune-build-artefacts
```

Default output is grep-style (`path:line:text` for matches; `path-line-text` for context, with `--` between context blocks):

```
internal/search/findmatches.go
98-	if opts.Pattern == "" {
99-		return nil, ErrEmptyPattern
100:	}
101-	re, err := regexp.Compile(opts.Pattern)
102-	if err != nil {
--
internal/search/findmatches_test.go
33-	res, err := search.FindMatches(t.Context(), search.Options{
34-		Root:    dir,
35:		Expr:    "is_source",
36-		Pattern: "TODO",
37-	}, content.DefaultRegistry())

2 match(es) across 2 file(s) (47 file(s) scanned)
```

**Exit codes** follow grep convention: `0` when at least one match was found, `1` when none, `124` on `--timeout`, `130` on Ctrl-C. Partial results are printed before exiting on timeout/interrupt.

## MCP

```json
{
  "name": "find_matches",
  "arguments": {
    "pattern": "(?i)\\bTODO\\b",
    "dir": "/Users/me/proj",
    "expr": "is_source && language == \"go\"",
    "context_before": 2,
    "context_after": 2,
    "max_matches_per_file": 5
  }
}
```

Response:

```json
{
  "matches": [
    {
      "path": "/Users/me/proj/internal/auth/session.go",
      "content_type": "source/go",
      "line": 42,
      "text": "\t// TODO: rotate the signing key",
      "before": ["func newSession(uid int64) (*Session, error) {", "\tif uid == 0 {"],
      "after": ["\t\treturn nil, ErrInvalidUID", "\t}"]
    }
  ],
  "count": 1,
  "files_scanned": 47,
  "files_with_matches": 1,
  "elapsed_seconds": 0.018
}
```

## Filtering by line role (`--match-in`)

A `TODO|FIXME|XXX|HACK` sweep across a typical source tree returns mostly noise: test fixtures with `"TODO"` inside string literals, fuzz seeds with `XXXX`, identifiers like `FooTODOBar`, ID3 frame names like `TXXX`. To filter to *real* annotations:

```sh
# Comments only — drops every match that isn't on a comment line.
file-search-on find-matches 'TODO|FIXME|XXX|HACK' \
  -e 'is_source && language == "go" && !is_test_file' \
  --match-in comments

# Code only — useful for finding patterns hidden in string literals.
file-search-on find-matches 'http://[^\s"]+' \
  -e 'is_source' \
  --match-in code
```

How it works: for source files the scanner classifies each line under the language's comment syntax (Go `//` + `/* */`, Python `#`, C/C++/Java/Rust/JS/TS/Swift/Kotlin/Scala/Zig `//` + `/* */`, PHP `//` + `#` + `/* */`, SQL `--`, Lua `--` + `--[[ ]]`, Haskell `--` + `{- -}`, OCaml `(* *)`, Clojure `;`, VB `'`, MATLAB `%`, Fortran `!`, Pascal `//` + `(* *)`, assembly `;` / `#`). Block comments track state across lines.

The classifier is **line-granular**: a trailing-comment line like `x := 1 // TODO` classifies as code (matches the hand-rolled `^\s*//<pattern>` regex shape an agent would otherwise write). Pure comment lines and lines inside an open block comment are `comments`; everything else is `code`. Non-source files (markdown, JSON, plain text) have no syntax registered — `--match-in` is a no-op for them and every match passes.

`strings` mode (matching inside string literals — useful for hunting hardcoded URLs / credentials) is deferred to a follow-up issue; v1 ships `any` / `comments` / `code`.

## Patterns

The regex flavour is [RE2](https://github.com/google/re2/wiki/Syntax) — Google's regex syntax, same as Go's `regexp` package and CEL's `matches()`. Highlights:

- `(?i)` → case-insensitive
- `(?m)` → multi-line (`^` / `$` match per line; `find-matches` already scans line-by-line so this rarely matters)
- `\b` → word boundary
- `[[:alpha:]]` → POSIX character class
- **No lookbehind / backreferences** — RE2 trades these for linear-time guarantees.

Anchors interact with the line scanner: `^TODO` matches only when the line starts with TODO; `TODO$` matches only when the line ends in TODO. The scanner strips the trailing newline, so `$` matches the literal end-of-line.

## Pitfalls

- **No body access via `--expr`.** Use `body.contains("…")` / `body.matches("…")` on `search` when the regex IS the filter and you just want paths. `find-matches` is the line-level shape; the regex isn't passed through the CEL evaluator.
- **Context windows include all prior lines, not just context lines.** A match on line 4 with `--before 3` gets lines 1, 2, 3 — even if line 1 also matched. ripgrep behaves the same way.
- **Binary content types are skipped silently.** Use [`body-search.md`](body-search.md) recipes if you need patterns inside text-shaped bodies of typed content; for raw bytes inside a PDF/EPUB an external extractor is required.
- **Pathological long lines are truncated** at 64 KiB per line. Minified JSON / rolled-up logs that exceed the cap will still be scanned, but the offending line is reported as truncated content.

## Related recipes

- [`source-code.md`](source-code.md) — type predicates and source-specific attributes (`language`, `loc`, `comment_loc`).
- [`body-search.md`](body-search.md) — when you want paths only and the regex is the filter, not the result shape.
- [`read-lines.md`](read-lines.md) — fetch arbitrary ranges of a single file once you know which one to look at.
