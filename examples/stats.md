# Stats — directory reconnaissance

The `stats` subcommand and matching MCP tool aggregate a content-type histogram plus totals for a directory tree — quick "what's in here?" reconnaissance without retrieving every path. Pair with the same CEL expression you'd use for `search` to scope the histogram.

## CLI

```sh
# Whole tree
file-search-on stats -d ~/Downloads

# Markdown-only — scoped by CEL filter
file-search-on stats 'is_markdown' -d ~/notes

# Filter + threshold — "how many big markdown posts do I have?"
file-search-on stats 'is_markdown && word_count > 500' -d ~/notes

# Skip the usual noise + honour .gitignore
file-search-on stats -d . --exclude node_modules --exclude .git --respect-gitignore

# JSON output for piping into jq
file-search-on stats -d ~/Music -o json | jq '.content_types[] | select(.name == "audio/flac")'
```

## Output

**Table (default):**

```
content_type                   count      total_size
markdown                          42        1,234,567 B
image/jpeg                       100      45,000,000 B
audio/flac                       250     2,500,000,000 B
unknown                            5           20,480 B
---                              ---             ---
TOTAL                            397     2,546,255,047 B
```

Sorted by count descending, tie-broken by name ascending — stable across runs so the output diffs cleanly when you take periodic snapshots.

**JSON (`-o json`):**

```json
{
  "total_count": 397,
  "total_size": 2546255047,
  "content_types": [
    {"name": "audio/flac", "count": 250, "total_size": 2500000000},
    {"name": "image/jpeg", "count": 100, "total_size": 45000000},
    {"name": "markdown",   "count": 42,  "total_size": 1234567},
    {"name": "unknown",    "count": 5,   "total_size": 20480}
  ]
}
```

## MCP

```json
{
  "name": "stats",
  "arguments": {
    "dir": "/Users/me/Downloads"
  }
}
```

With a CEL filter:

```json
{
  "name": "stats",
  "arguments": {
    "dir": "/Users/me/Pictures",
    "expr": "is_image && iso > 1600"
  }
}
```

Response shape mirrors the CLI JSON output plus the standard partial-result fields:

```json
{
  "total_count": 47,
  "total_size": 312500000,
  "content_types": [
    {"name": "image/jpeg", "count": 47, "total_size": 312500000}
  ],
  "elapsed_seconds": 0.823,
  "cancelled": false
}
```

Inspect `cancelled` and `cancellation_reason` on every response — on timeout the partial histogram is returned with `cancelled: true` rather than an error.

## Recipes

```sh
# Find the heaviest content type in a Downloads folder — the
# "what is eating my disk?" question
file-search-on stats -d ~/Downloads -o json | jq 'reduce .content_types[] as $b (null; if . == null or $b.total_size > .total_size then $b else . end)'

# Compare two trees side-by-side
diff <(file-search-on stats -d ./old -o json | jq -S .) <(file-search-on stats -d ./new -o json | jq -S .)

# Periodic snapshot — write daily stats to a log for trend analysis
file-search-on stats -d ~/Code -o json > ~/.cache/fso/stats-$(date +%F).json
```

## Performance

`stats` walks every file in the tree, same as `search`. It pays the per-file content-type detection cost (Stat + magic-byte sniff + extension match) on every file. With a CEL filter like `is_markdown && word_count > 500`, the per-file attribute parse also runs.

For repeat reconnaissance on the same tree, pair with `--index-path` (CLI) or the auto-on MCP cache: unchanged files hit the cache and skip the per-file parse, making subsequent `stats` runs much faster.

For *very* large trees, set `--timeout` (CLI) or `timeout_seconds` (MCP) — partial histograms are returned with `cancelled: true` rather than failing the call.
