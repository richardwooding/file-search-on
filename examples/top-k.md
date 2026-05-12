# Top-K queries: sort + limit

When you want "the biggest 5", "the most recent 10", or "the longest 3" of something, use `--sort` and `--limit` together instead of post-processing with `sort | head`.

## CLI

```sh
# 5 longest videos
file-search-on 'is_video' --sort duration --order desc --limit 5 -d ~/Movies

# 10 most recent photos
file-search-on 'is_image' --sort taken_at --order desc --limit 10 -d ~/Pictures

# 3 largest archives — useful for "what's eating my Downloads?"
file-search-on 'is_archive' --sort uncompressed_size --order desc --limit 3 -d ~/Downloads -o verbose

# Top 20 source files by lines of code (real lines, not blanks/comments)
file-search-on 'is_source && language == "go"' --sort loc --order desc --limit 20 -d ./src

# Oldest 5 markdown drafts — surface stale unfinished work
file-search-on 'is_markdown && draft' --sort mod_time --order asc --limit 5 -d ~/notes

# Highest-bitrate audio in the library — find the lossless masters
file-search-on 'is_audio' --sort bitrate --order desc --limit 10 -d ~/Music
```

## Recognised sort keys

| Key | Type | Source |
| --- | --- | --- |
| `size` | int | universal — `os.Stat` size |
| `name`, `path` | string | universal |
| `mod_time` | timestamp | universal — `os.Stat` mtime |
| `word_count`, `line_count`, `page_count` | int | documents / markup |
| `iso`, `focal_length`, `taken_at` | int / float / timestamp | image EXIF |
| `duration`, `bitrate`, `sample_rate` | float / int / int | audio + video |
| `video_width`, `video_height`, `frame_rate` | int / int / float | video |
| `year`, `track` | int | audio tags |
| `entry_count`, `uncompressed_size` | int | archives |
| `loc`, `attachment_count`, `email_count` | int | source / email |
| `sent_at` | timestamp | email |

Files missing the attribute (e.g. sorting `duration` on a markdown file) group at the end with stable walk-order ties.

## MCP

```json
{
  "name": "search",
  "arguments": {
    "expr": "is_video",
    "dir": "/Users/me/Movies",
    "sort_by": "duration",
    "order": "desc",
    "limit": 5
  }
}
```

The MCP response always includes `matches[]`, `count`, `cancelled`, `cancellation_reason`, and `elapsed_seconds`. With sort/limit, `count` reflects the truncated set.

## Streaming + sort/limit

`--sort` forces buffered mode regardless of `-o bare/json` or `--unsorted` — top-K is incoherent with streaming (you can't know the top-5 until you've seen every candidate). `--limit` alone (no sort) returns the first N in walk order; the CLI still buffers because the count footer is useful, but for very large result sets streaming-with-cap is a future enhancement.

## Combining with timeouts

`--sort` + `--timeout` pair cleanly: on timeout, whatever matches were collected so far are sorted + truncated to the limit and printed. The footer shows the partial count and the process exits 124.

```sh
# Top 10 biggest files in a huge tree; bail after 30s if it's taking too long
file-search-on 'true' --sort size --order desc --limit 10 --timeout 30s -d /var
```
