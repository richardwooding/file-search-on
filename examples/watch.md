# Watching for new / changed files

The `watch` subcommand (and the matching MCP `watch_search` tool) is the **inverse of `search`**: instead of walking a tree once and reporting what already matches, it watches directories continuously and emits each new or changed file *as it appears*, filtered by the same CEL expression vocabulary. "Tell me when X shows up" rather than "what X is here right now".

## When to reach for this vs `search`

| Question | Tool |
| --- | --- |
| "What files match X right now?" — one-shot walk | [`search`](../README.md) |
| "Tell me each time a file matching X appears" — continuous | **`watch`** |
| "Wait up to N seconds for the next matching file, then stop" — bounded | **`watch_search`** MCP tool |

Typical uses: a `~/Downloads` triage (`is_pdf && page_count > 50`), screenshot OCR as you save them (`is_image && body.contains("error")` with `--ocr`), build-artefact spotting (`is_binary`), or feeding a pipeline that reacts to new data drops.

## How it works

Built on [fsnotify](https://github.com/fsnotify/fsnotify). Every directory under each watch root is registered up front; directories created *during* the watch are registered when their `CREATE` event arrives, so the watch is effectively recursive. Each `CREATE` / `WRITE` event is debounced (300 ms) to coalesce the multi-write bursts editors and downloaders emit into a single evaluation, then the file is run through the **exact same** `BuildAttributes` + CEL `Evaluate` path as `search` — so OCR, hashes, perceptual hash, body access, xattrs, and index caching all compose identically.

Only create + write events are considered; deletes and renames are out of scope. Files created inside a brand-new directory in the narrow window before its watch is added can be missed — an inherent fsnotify race, acceptable for a "notice new matches" tool.

## CLI

Runs until Ctrl-C. Output is NDJSON by default (one match object per line), or `-o bare` for paths only, or a custom `--format` template.

```sh
# Every new / changed markdown file under the docs tree
file-search-on watch 'is_markdown' -d ./docs

# Watch Downloads for large PDFs, paths only
file-search-on watch 'is_pdf && page_count > 50' -d ~/Downloads -o bare

# OCR each screenshot as it lands on the Desktop; match ones mentioning "invoice"
file-search-on watch 'is_image && body.contains("invoice")' --ocr -d ~/Desktop

# Watch several roots at once (repeat -d), custom output line
file-search-on watch 'is_source && language == "go"' \
  -d ./cmd -d ./internal \
  --format '{{.Path}}	{{.ContentType}}'

# Persist parses/bodies across the watch (and across runs) with an index
file-search-on watch 'is_image' --ocr --index-path ~/.fso-index.db -d ~/Screenshots

# Empty expression matches every new / changed file
file-search-on watch -d ./incoming -o bare
```

Flags mirror `search` where they make sense: `--body` / `--body-max-bytes`, `--ocr` / `--ocr-timeout`, `--with-hashes`, `--with-phash`, `--with-xattrs`, `--exclude`, `--respect-gitignore`, `--index-path`. Ranking, semantic embedding, and project resolution are omitted — they're walk-collection concerns, not single-file-event concerns.

### Home-directory safety guard

`watch` refuses to start unless every `-d` directory is inside your home directory — a guard against accidentally aiming a long-running watcher at system paths or an entire volume. To watch elsewhere (another volume, `/opt`, `/srv`, or in a container where `HOME` isn't set), pass `--allow-outside-home`:

```sh
file-search-on watch 'is_video' -d /Volumes/Media --allow-outside-home
```

`$HOME` itself and anything under it pass. The guard is fail-closed: if `$HOME` can't be determined it refuses until you set `HOME` or opt out. The `mcp` server has the same guard (covering its cwd + `--warm-dir` / `--watch-index-dir` / `--sandbox-dir`).

NDJSON output is the same wire shape as `search -o json`, so the same `jq` recipes work:

```sh
file-search-on watch 'is_image' --with-phash -d ~/Screenshots | jq '{path, phash}'
```

## MCP

The MCP surface is a **bounded** `watch_search` tool rather than open-ended streaming: it blocks for a watch window, collects matching files, and returns them in one response. MCP is request/response, so an unbounded subscription would hang the call forever — use the CLI `watch` for that.

```json
{
  "name": "watch_search",
  "arguments": {
    "expr": "is_image && body.contains(\"error\")",
    "dir": "/Users/me/Desktop",
    "duration_seconds": 60,
    "max_events": 5,
    "ocr_images": true
  }
}
```

The call returns when **any** of these happens first: `duration_seconds` elapses (default 30s, hard-capped at 600s), `max_events` matches are collected, or the client cancels.

Response:

```json
{
  "matches": [
    {
      "path": "/Users/me/Desktop/Screenshot 2026-05-27 at 14.03.10.png",
      "content_type": "image/png",
      "size": 481222,
      "ocr_confidence": 0.94,
      "body": "… Build error: cannot find module …"
    }
  ],
  "count": 1,
  "watched_seconds": 12.4,
  "hit_max_events": true
}
```

`hit_max_events` is `true` when the watch returned early because `max_events` was reached; otherwise the window ran to `duration_seconds`. Inputs mirror the `search` tool: `ocr_images` / `ocr_timeout_ms`, `compute_hashes`, `with_phash`, `with_xattrs`, `include_body` / `body_max_bytes`, `excludes`, `respect_gitignore`, and `dir` / `dirs`.

## Pitfalls

- **Debounce window is 300 ms.** A file written and matched once won't re-emit for sub-300ms re-saves. Rapid append-loops (e.g. a growing log) emit at most one event per 300 ms quiet period, not per write.
- **No delete / rename events.** `watch` only reports files that exist and match at evaluation time. A file matched then deleted within the window still appears (it existed when the debounce timer fired).
- **New-subdirectory race.** A file created in the same instant a new subdirectory is created can be missed before the subdir's watch is armed. Re-running a `search` over the tree afterwards catches any such gaps.
- **`watch_search` is bounded.** It is not a persistent subscription — it returns after the window. For long-lived monitoring, run the CLI `watch` subcommand as a background process.
- **OCR / hashing cost applies per event.** With `--ocr` or `--with-hashes`, every matching event pays the extraction cost. Pair with `--index-path` so repeat hits on unchanged files are free.

## Related recipes

- [`ocr.md`](ocr.md) — OCR provider details; pairs with `watch --ocr` for live screenshot triage.
- [`body-search.md`](body-search.md) — the `body.contains` / `body.matches` filters that `watch --body` enables.
- [`indexing.md`](indexing.md) — `--index-path` caching, which makes repeat watch evaluations free.
