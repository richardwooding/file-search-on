# Snippets: see what's in a file alongside its metadata

Most workflows that involve "search for matching files, then read them to decide" can be collapsed into a single call by enabling snippets. Each match comes back with the first N lines of body text, so the agent (or the shell user) can triage without a follow-up read.

## CLI

```sh
# 5 most recent markdown drafts, with a 10-line preview of each
file-search-on 'is_markdown && draft' \
    --sort mod_time --order desc --limit 5 \
    --snippet --snippet-lines 10 \
    -o verbose -d ~/notes

# Find Go source files referencing TODO, return first 5 lines for context
file-search-on 'is_source && language == "go"' \
    --snippet --snippet-lines 5 \
    -o json -d ./src | jq 'select(.snippet | test("TODO"))'

# Triage CSVs by inspecting their header + first row in one go
file-search-on 'is_csv' --snippet --snippet-lines 2 -o verbose -d ./data
```

## Output formats

- **`-o verbose`** indents the snippet under the metadata for human reading:

    ```
    /path/to/post.md
      content_type   markdown
      size           1,234 bytes
      title          Hello World
      word_count     128
      snippet:
        # Hello World

        This is the first paragraph of the post. It's used
        as a teaser preview when surfacing search results.
    ```

- **`-o json`** sets `"snippet"` on each match record. Convenient for `jq` post-processing.
- **`--format`** sees `{{.Snippet}}` in the template context — useful for custom rendering.
- `-o default` and `-o bare` intentionally omit the snippet to stay compact / pipe-friendly.

## Which content types populate

Snippets only populate for text-based content types where reading the first N lines is meaningful:

- `markdown`, `text`, `html`, `csv`, `json`, `xml`
- All 18 `source/*` languages (Go, Python, JS, TS, Rust, C, C++, Java, Ruby, Swift, Kotlin, Shell, Lua, Elixir, Clojure, Haskell, OCaml, Zig)

Binary families (image, audio, video, archive, binary, office, epub, email, pdf) leave `snippet` empty — for those, the per-family attributes (artist, video codec, page count, etc.) already convey "what is this file".

## Buffer caps

Pathological inputs (minified JSON on one giant line, rolled-up logs) won't blow up: the scanner caps each line at 64 KiB, beyond which the line is truncated. The snippet still reflects whatever was read; broken or non-UTF-8 files just produce best-effort results.

## MCP

```json
{
  "name": "search",
  "arguments": {
    "expr": "is_markdown",
    "dir": "/Users/me/notes",
    "include_snippet": true,
    "snippet_lines": 8
  }
}
```

Each entry in the response `matches[]` carries a `snippet` field (omitted when empty per `,omitempty`). The agent can read the snippet and decide whether to call `read_attributes` for full metadata, or whether the snippet alone answers the question.
