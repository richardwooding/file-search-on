# Body-content filters

Metadata is cheap; body content is expensive but powerful. With `--body` (CLI) or `include_body: true` (MCP) the file body becomes a CEL string variable that pairs naturally with CEL's built-in string methods.

## CEL string methods (no custom functions needed)

CEL already provides everything you need on strings:

| Method | Example | Notes |
| --- | --- | --- |
| `body.contains(s)` | `body.contains("transformer")` | Substring (case-sensitive). |
| `body.matches(re)` | `body.matches("(?i)\\bAPI\\b")` | RE2 regex (Google's syntax — same as Go's `regexp`). `(?i)` for case-insensitive. |
| `body.startsWith(s)` | `body.startsWith("---\n")` | Useful for frontmatter detection. |
| `body.endsWith(s)` | `body.endsWith(".\n")` | |
| `size(body)` | `size(body) > 5000` | Byte length. Distinct from `word_count` (which is whitespace-tokenised). |

No custom CEL function for regex — `matches` is part of the standard CEL string vocabulary.

## CLI

```sh
# Substring: find docs that mention a topic
file-search-on 'is_markdown && body.contains("transformer")' -d ~/notes --body

# Regex: find TODOs (case-insensitive, word-boundary)
file-search-on 'is_source && body.matches("(?i)\\bTODO\\b")' -d ./src --body

# Combine with sort + limit for top-K results
file-search-on 'is_source && body.contains("panic")' \
    -d ./src --body \
    --sort size --order desc --limit 5

# Multiple regex alternatives — find any of three terms
file-search-on 'is_markdown && body.matches("(?i)\\b(GPT|claude|gemini)\\b")' -d ~/notes --body

# Reject patterns with negation
file-search-on 'is_source && body.contains("import") && !body.contains("import \"crypto/md5\"")' -d ./src --body

# Find files whose body is just frontmatter (no content)
file-search-on 'is_markdown && body.startsWith("---") && size(body) < 500' -d ./posts --body
```

## MCP

```json
{
  "name": "search",
  "arguments": {
    "expr": "is_markdown && body.contains(\"transformer\")",
    "dir": "/Users/me/notes",
    "include_body": true
  }
}
```

For regex:

```json
{
  "name": "search",
  "arguments": {
    "expr": "is_source && body.matches(\"(?i)\\\\bAPI\\\\b\")",
    "dir": "./src",
    "include_body": true,
    "excludes": ["node_modules", ".git", "vendor"]
  }
}
```

(Note the double-escaped backslashes — JSON requires `\\` and the CEL regex requires `\b`, so the input string carries `\\\\b`.)

## Cost and tuning

`--body` reads every candidate file. Three knobs make this cheap:

1. **Tight type predicate**: write `is_markdown && body.contains(...)` so the metadata-only `is_markdown` check fires first and prunes most candidates before the expensive body read.
2. **Excludes + .gitignore**: combine with `--exclude` / `--respect-gitignore` so `node_modules`, `target`, etc. never get opened.
3. **Body cap**: `--body-max-bytes` defaults to 1 MiB. Drop it (e.g. `--body-max-bytes 4096`) for "find files whose **header** contains X" — much cheaper than reading whole files.

## Which content types populate

Only text-based families:

- `markdown`, `text`, `html`, `csv`, `json`, `xml`
- All 18 `source/*` languages (Go, Python, JS, TS, Rust, C, C++, Java, Ruby, Swift, Kotlin, Shell, Lua, Elixir, Clojure, Haskell, OCaml, Zig)

Binary families (PDF, image, audio, video, archive, binary, office, epub, email) leave `body` empty — `body.contains(...)` on them is always false. For PDF / office bodies, use the per-family attributes (`title`, `author`, `language`, `page_count`) which the parsers extract; full text extraction from those formats is out of scope.

## Combining with snippets

`--body` (for filtering) and `--snippet` (for displaying a preview) compose cleanly:

```sh
# Filter on body content, return a preview snippet for context
file-search-on 'is_markdown && body.contains("transformer")' \
    -d ~/notes --body \
    --snippet --snippet-lines 5 \
    -o verbose
```

The body is read once per candidate; the snippet is a separate (line-based) read. Both are cheap when the file is already in OS file cache from the body read.
