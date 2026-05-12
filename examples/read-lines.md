# Read a range of lines

The `lines` subcommand and matching MCP `read_lines` tool return a specific line range from a single file. Designed as the second step after `search`: find files via search, then fetch context around each match without leaving the tool.

## CLI

```sh
# First 50 lines
file-search-on lines main.go --start 1 --end 50

# Lines 1000–1050 of a log file
file-search-on lines /var/log/app.log --start 1000 --end 1050

# First 20 lines, capped (in case the file is huge)
file-search-on lines big.csv --max-lines 20

# Machine-readable output for piping into jq
file-search-on lines main.go --start 1 --end 10 -o json
```

Output is the raw lines on stdout. With `-o json` you get a structured object:

```json
{
  "path": "/Users/me/proj/main.go",
  "start_line": 1,
  "end_line": 10,
  "total_lines": 234,
  "lines": [
    "package main",
    "",
    "import (",
    "\t\"context\"",
    "\t\"fmt\""
  ],
  "truncated": false
}
```

`truncated: true` when the requested range exceeded `--max-lines` (default 1000); only the first `max_lines` lines of the range are returned.

## MCP

```json
{
  "name": "read_lines",
  "arguments": {
    "path": "/Users/me/proj/main.go",
    "start_line": 50,
    "end_line": 100
  }
}
```

## Composing with search

The bread-and-butter pattern: find candidate files via `search`, then call `read_lines` for each:

```sh
# Find TODOs in source files, then read 5 lines of context around each
file-search-on 'is_source && body.matches("(?i)\\bTODO\\b")' --body -o bare -d ./src | while read f; do
    echo "=== $f ==="
    file-search-on lines "$f" --max-lines 20
done
```

In an MCP agent flow:

1. Call `search` with `expr` matching the content/file pattern of interest.
2. For each entry in `matches[]`, call `read_lines` with the match's `path` and a small range (e.g. start 1, end 30) to inspect the file's preamble — or use the snippet returned by `search` directly when only the header matters.

`read_lines` shares the server-default timeout with the other tools — pathological files (multi-gigabyte logs) can't wedge the server. Per-line cap is 64 KiB; lines longer than that are truncated to that length and the scan continues (the response remains structurally well-formed).

## Edge cases

- **`end_line` past EOF** → clamped to `total_lines`. No error.
- **`start_line` past EOF** → empty `lines[]`, `end_line == start_line - 1`. No error.
- **`start_line > end_line`** → returns an error (invalid range).
- **`max_lines = 0`** → uses the 1000-line default.
- **Empty file** → empty `lines[]`, `total_lines == 0`.
- **Path doesn't exist** → error.
