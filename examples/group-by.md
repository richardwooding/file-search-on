# Stats — grouping by any attribute

The `stats` tool buckets by content type by default, but `--group-by` (CLI) / `group_by` (MCP) lets you aggregate by any attribute the parsers populate. Useful for "how many photos per camera?", "lines of code per language?", and "Downloads broken down by extension?" without retrieving every path.

## Recognised group_by values

| Key | What it buckets by |
| --- | --- |
| `content_type` (default) | The registered content-type name (e.g. `markdown`, `image/jpeg`, `notebook/jupyter`). |
| `ext` | File extension, lowercase, with leading `.` (e.g. `.md`, `.png`). |
| `dir` | Directory of each file (the OS-native parent path). |
| `language` | Source-code or document language (`go`, `python`, `en`, `fr`). |
| `camera_make`, `camera_model`, `lens` | Image EXIF. |
| `artist`, `album`, `genre` | Audio tags. |
| `kernel` | Jupyter/Zeppelin notebook kernel. |
| `binary_format`, `binary_type` | ELF/Mach-O/PE classification. |
| `frontmatter_format` | `yaml`, `toml`, or `json` for markdown front-matter. |

Unknown values fall back to `content_type` (rather than erroring) — the contract is "always return a histogram".

## CLI

```sh
# How many files per extension in this Downloads folder?
file-search-on stats -d ~/Downloads --group-by ext

# Lines of code per language in this repo
file-search-on stats 'is_source' --group-by language -d ./src

# Which cameras took my photos last year?
file-search-on stats 'is_image && taken_at > timestamp("2025-01-01T00:00:00Z")' \
    --group-by camera_make -d ~/Pictures

# Top artists by track count
file-search-on stats 'is_audio' --group-by artist -d ~/Music -o json | \
    jq '.groups | sort_by(-.count) | .[0:10]'

# Which subdirectory has the most files?
file-search-on stats --group-by dir -d ~/Code -o json | \
    jq '.groups | sort_by(-.count) | .[0:5]'

# Notebook kernels in use
file-search-on stats 'is_notebook' --group-by kernel -d ~/notebooks
```

## MCP

```json
{
  "name": "stats",
  "arguments": {
    "expr": "is_image",
    "dir": "/Users/me/Pictures",
    "group_by": "camera_make"
  }
}
```

Response shape (group_by `language` over a source tree):

```json
{
  "total_count": 412,
  "total_size": 1283456,
  "group_by": "language",
  "groups": [
    {"name": "go",         "count": 250, "total_size": 850000},
    {"name": "javascript", "count": 100, "total_size": 350000},
    {"name": "shell",      "count": 62,  "total_size": 83456}
  ],
  "elapsed_seconds": 0.31
}
```

When `group_by` is omitted or set to `content_type`, the response also includes a back-compat `content_types[]` field with the same data — older agent integrations that hard-coded `content_types[]` continue to work without changes.

## Caveats

- **Numeric- and time-typed attributes aren't supported.** Bucketing `taken_at` by exact timestamp isn't useful; range bucketing (per day / month) is out of scope for v1. Use `taken_at > timestamp(...)` in the `expr` to scope, then `group_by camera_make` (or similar string attribute) to aggregate.
- **List-typed attributes aren't supported.** `tags` / `architectures` / `email_to` etc. would need per-element bucketing (file appears in N buckets), which isn't currently implemented. Filter via `expr` for now.
- **Empty / missing values** bucket as `"unknown"` (or `"(no ext)"` for missing extensions). Every walked file lands in some bucket.
