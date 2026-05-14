# Recipes — Cross-cutting cookbook

Patterns that span multiple content-type families, plus pipeline / output-format tricks.

## Cross-family queries

All media files (image, audio, video) over 100 MB — likely the largest space-eaters in a directory:

```sh
file-search-on '(is_image || is_audio || is_video) && size > 100000000' -d ~/
```

All "long" media — videos > 30 min OR audio > 10 min:

```sh
file-search-on '(is_video && duration > 1800) || (is_audio && duration > 600)'
```

All authored documents (PDF, EPUB, office, markdown) by a specific author:

```sh
file-search-on 'author == "Jane Doe" && (is_pdf || is_epub || is_office || is_markdown)'
```

Foreign-language anything — `language` is cross-cutting across markdown / HTML / PDF / EPUB / office:

```sh
file-search-on 'language == "fr"'
file-search-on 'language != "" && language != "en" && !language.startsWith("en")'
```

Anything titled — useful for inventories:

```sh
file-search-on 'title != ""' --format '{{.Path}}\t{{.ContentType}}\t{{.Title}}'
```

## Path-based filters

Inside or outside specific subtrees:

```sh
file-search-on 'is_pdf && dir.contains("/contracts/")'
file-search-on 'is_markdown && !dir.contains("/drafts/")'
file-search-on 'is_image && dir.startsWith("/Volumes/Photo Backup/")'
```

By filename pattern (CEL has `string.matches(regex)`):

```sh
file-search-on 'is_image && name.matches("IMG_[0-9]{4}.*")'        # iPhone-style filenames
file-search-on 'is_text && name.endsWith(".log.1")'                # rotated logs
```

## Output formats — pipeline patterns

### `bare` for `xargs`

```sh
# Backup all 4K videos to an external drive
file-search-on 'is_video && video_height >= 2160' -o bare -d ~/Videos | \
  xargs -I {} cp {} /Volumes/Backup/4K/

# Re-encode old H.264 to H.265 (ffmpeg required)
file-search-on 'is_video && video_codec == "h264" && video_height >= 1080' -o bare | \
  xargs -I {} ffmpeg -i {} -c:v libx265 {}.h265.mp4
```

### `json` for `jq` and downstream tooling

```sh
# Histogram of audio bitrates
file-search-on 'is_audio && bitrate > 0' -o json | jq -r '.bitrate' | sort -n | uniq -c

# Total disk used by 4K HEVC content
file-search-on 'is_video && video_height >= 2160 && video_codec == "h265"' -o json | \
  jq -s 'map(.size) | add | . / 1073741824 | "\(.)GB"'

# Everything by content type, frequency-sorted
file-search-on 'true' -o json | jq -r '.content_type' | sort | uniq -c | sort -rn
```

### `--format` for spreadsheet imports

```sh
# Photo metadata as TSV ready for a spreadsheet
file-search-on 'is_image && taken_at > timestamp("2024-01-01T00:00:00Z")' \
  --format '{{.Path}}\t{{.CameraModel}}\t{{.ISO}}\t{{printf "%.1f" .FocalLength}}\t{{.TakenAt}}' \
  > photos-2024.tsv
```

## Combining with other tools

### `find` for non-content filters

`file-search-on` doesn't know about file mtime / atime. Combine with `find` when you need them:

```sh
# Recently-modified Markdown files
find . -name '*.md' -mtime -7 -print0 | \
  xargs -0 -I {} file-search-on 'is_markdown && draft' -d $(dirname {})
```

### `ripgrep-all` for full-text inside metadata-matched files

```sh
# Search inside PDFs by Jane Doe for a specific phrase
file-search-on 'is_pdf && author == "Jane Doe"' -o bare | \
  xargs rga 'specific phrase'
```

### `du` summary by content family

```sh
# Disk used by audio / video / images, separately
for fam in is_audio is_video is_image is_office is_epub; do
  total=$(file-search-on "$fam" -o json -d ~/ 2>/dev/null | jq -s 'map(.size) | add')
  echo "$fam: $total bytes"
done
```

## Programmatic use via MCP

When run as an MCP server (`file-search-on mcp`), the same expressions work. An LLM agent can compose CEL based on the user's intent and call the `search` tool. Example client request:

```json
{
  "method": "tools/call",
  "params": {
    "name": "search",
    "arguments": {
      "expr": "is_video && video_height >= 2160 && video_codec == \"h265\"",
      "dir": "/Users/me/Videos"
    }
  }
}
```

The MCP `search` tool returns the same attribute set as `-o json` — see [`list_attributes`](https://github.com/richardwooding/file-search-on#mcp-server-mode) for the schema.

The `list_attributes` payload also includes the four built-in fuzzy functions (`levenshtein`, `soundex`, `ngrams`, `ngram_similarity`) with their signatures and examples — agents pick them up via the MCP handshake without prior knowledge.

### Saving response tokens with `fields`

Both the MCP `search` and `read_attributes` tools accept an optional `fields: []string` to project responses to only the listed attributes. `path`, `content_type`, and `size` stay on always; everything else is opt-in:

```json
{
  "name": "search",
  "arguments": {
    "expr": "is_image",
    "dir": "/Users/me/Pictures",
    "sort_by": "taken_at",
    "order": "desc",
    "limit": 50,
    "fields": ["taken_at", "camera_model"]
  }
}
```

Saves the ~12 EXIF fields per match that the caller didn't ask for. The sort key (`taken_at` here) is honoured even when it's in `fields` — sort happens before projection, so it works for keys NOT in `fields` too. Unknown field names error at request validation, before the walk runs — call `list_attributes` for the canonical set. Omit `fields` to keep the existing behaviour (every populated attribute).

The CLI equivalent is `--format` (Go `text/template`) — agents on the MCP side use `fields` instead.

## Fuzzy / phonetic matching across families

```sh
# Cross-family phonetic search — any author / artist / camera-make matching a phonetic target.
file-search-on '(is_markdown && soundex(author) == soundex("Schmidt")) ||
                (is_audio && soundex(artist) == soundex("Schmidt")) ||
                (is_image && soundex(camera_make) == soundex("Schmidt"))'

# Tolerate typos in cross-cutting filename matches.
file-search-on 'levenshtein(name, "kubernetes-deploy.yaml") <= 3'
```

See [`fuzzy-search.md`](./fuzzy-search.md) for the dedicated fuzzy / phonetic recipe collection.

## When file-search-on isn't the right tool

- **Content search inside files** — use `ripgrep` (text), `rga` (multi-format), or `xsv` (CSV). `file-search-on` does metadata filtering; pipe its output into a content tool.
- **Filesystem-attribute search** (mtime, atime, owner, mode) — use `find`. Compose with `file-search-on` via shell.
- **Real-time indexing** — this tool walks directly; for huge repeated searches over the same tree, build an index with another tool and query that.
