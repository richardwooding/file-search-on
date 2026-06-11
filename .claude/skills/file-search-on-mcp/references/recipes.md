# Recipes

Copy-pasteable scenarios. Each is a single MCP call (or a tiny sequence) for a realistic question. Paths use placeholders — substitute the agent's actual targets. All recipes are **read-only** unless noted.

## Contents

- Top-5 longest videos
- Photos near a GPS bounding box
- Find every TODO in Go source with context
- Duplicate photos by sha256
- Near-duplicate markdown by SimHash
- Files with embedded secrets
- What's in tree A but not in tree B
- Neglected markdown drafts (longest + oldest)
- Photos by camera, last 30 days (histogram)
- Find the Go module that owns a path
- Semantic search a docs tree
- Find the function that does X (function-level semantic search)
- AI-generated images via Content Credentials
- Wait for the next screenshot mentioning "invoice"

## Top-5 longest videos

```json
{
  "name": "search",
  "arguments": {
    "expr": "is_video",
    "dir": "~/Movies",
    "sort_by": "duration",
    "order": "desc",
    "limit": 5,
    "fields": ["duration", "video_codec", "video_height", "video_width"]
  }
}
```

`fields` keeps the response compact when only a few attrs are needed.

## Photos near a GPS bounding box

Bbox around central Cape Town (lat -33.96..-33.7, lon 18.3..18.7):

```json
{
  "name": "search",
  "arguments": {
    "expr": "is_image && gps_lat > -33.96 && gps_lat < -33.7 && gps_lon > 18.3 && gps_lon < 18.7",
    "dir": "~/Pictures",
    "sort_by": "taken_at",
    "order": "desc",
    "fields": ["taken_at", "camera_model", "gps_lat", "gps_lon"]
  }
}
```

For arbitrary polygons rather than bboxes, use `point_in_polygon(gps_lat, gps_lon, [lat0, lon0, lat1, lon1, ...])`.

## Find every TODO in Go source with context

```json
{
  "name": "find_matches",
  "arguments": {
    "pattern": "(?i)\\bTODO\\b",
    "expr": "is_source && language == \"go\"",
    "dir": "./internal",
    "context_before": 2,
    "context_after": 2,
    "prune_build_artefacts": true
  }
}
```

`prune_build_artefacts` skips `vendor`, `node_modules`, etc. without listing them in `excludes`. `find_matches` only sees text content types, so a bare `expr` of `is_source` is a tighter pass than no expr at all.

## Duplicate photos by sha256

```json
{
  "name": "find_duplicates",
  "arguments": {
    "dir": "~/Pictures",
    "expr": "is_image",
    "min_size": 65536
  }
}
```

Returns groups sorted by `wasted_bytes` desc — biggest reclamation candidates first. `min_size` skips trivial thumbnails. Hashes cache; the second run on an unchanged tree is essentially free.

## Near-duplicate markdown by SimHash

Catches typo-edits, regenerated headers, template copies — anything `find_duplicates` misses because the bytes aren't byte-identical.

```json
{
  "name": "find_near_duplicates",
  "arguments": {
    "dir": "~/Notes",
    "expr": "is_markdown",
    "threshold": 0.9,
    "min_size": 512
  }
}
```

Tighten `threshold` (`0.95` ≈ whitespace-only edits) to cut false positives, loosen (`0.75`) for structural overlap. The default 0.85 can over-cluster boilerplate-heavy corpora.

## Files with embedded secrets

`has_secrets` and `secret_kinds` are CEL functions that need the body:

```json
{
  "name": "search",
  "arguments": {
    "expr": "(is_source || is_text || is_yaml || is_toml || is_json) && has_secrets(body)",
    "dir": "./",
    "include_body": true,
    "prune_build_artefacts": true,
    "respect_gitignore": true,
    "fields": ["sha256"]
  }
}
```

Then narrow with `secret_kinds(body)` to inspect specific kinds:

```json
{
  "expr": "(is_source || is_text) && \"aws-access-key\" in secret_kinds(body)",
  "include_body": true
}
```

## What's in tree A but not in tree B

Pre-backup sanity check — files on the external drive not yet copied locally:

```json
{
  "name": "diff_trees",
  "arguments": {
    "tree_a": "/Volumes/Backup/Pictures",
    "tree_b": "~/Pictures",
    "op": "a-minus-b"
  }
}
```

Use `op: "mismatch"` to find same-named files whose content drifted (sync correctness).

## Neglected markdown drafts (longest + oldest)

```json
{
  "name": "search",
  "arguments": {
    "expr": "is_markdown && draft && mod_time < timestamp(\"2025-01-01T00:00:00Z\") && word_count > 200",
    "dir": "~/Notes",
    "sort_by": "mod_time",
    "order": "asc",
    "limit": 20,
    "fields": ["mod_time", "word_count", "tags"]
  }
}
```

Or call the baked preset:

```json
{ "name": "query_preset", "arguments": { "name": "old_drafts", "dir": "~/Notes" } }
```

## Photos by camera, last 30 days (histogram)

```json
{
  "name": "stats",
  "arguments": {
    "expr": "is_image && taken_at > timestamp(\"2025-09-01T00:00:00Z\")",
    "dir": "~/Pictures",
    "group_by": "camera_make"
  }
}
```

Time-bucket variants: `group_by: "taken_at_month"` / `taken_at_year` for activity-over-time, `mtime_year` for file-system-level recency.

## Find the Go module that owns a path

```json
{
  "name": "resolve_project_for_path",
  "arguments": { "path": "~/Code/somewhere/deep/internal/auth/session.go" }
}
```

Returns `project_root`, `project_types[]`, and the indicator filenames that matched (e.g. `go.mod`). A directory can match multiple types — a Go module that also ships `docker-compose.yml` hits both.

## Semantic search a docs tree

Conceptual match — paraphrase-tolerant, surfaces synonyms / topic-level hits even when the exact words don't appear:

```json
{
  "name": "search_semantic",
  "arguments": {
    "query": "post-mortem of a database outage",
    "dir": "~/Documents",
    "expr": "is_markdown || is_pdf",
    "threshold": 0.55,
    "limit": 20
  }
}
```

Requires Ollama running locally with an embedding model pulled. The first call fails clearly if Ollama is unreachable; embeddings cache per file (size, mtime).

## Find the function that does X (function-level semantic search)

Source files are embedded one chunk per function, so a hit pinpoints the matching function — not just the file. Ask `include_match_snippet` to inline its code:

```json
{
  "name": "search_semantic",
  "arguments": {
    "query": "retry an HTTP request with exponential backoff",
    "dir": "./internal",
    "expr": "is_source && language == \"go\"",
    "threshold": 0.5,
    "include_match_snippet": true,
    "limit": 10
  }
}
```

Each match carries `match_symbol` (the function/method), `match_start_line`/`match_end_line`, and `match_snippet` (its source, capped by `snippet_lines`, default 60). Drop `include_match_snippet` and `read_lines` the range yourself for the full body. `match_symbol` is empty when the best chunk is a file's leading package/imports header rather than a function.

## AI-generated images via Content Credentials

C2PA provenance is read (unverified, like EXIF) from JPEG/PNG:

```json
{
  "name": "search",
  "arguments": { "dir": "~/Downloads", "expr": "is_image && c2pa_ai_generated" }
}
```

Other C2PA filters: `is_c2pa` (has a manifest), `c2pa_claim_generator.contains(\"Firefly\")` (creating tool), `c2pa_signed_by.contains(\"Adobe\")` (claimed signer). Absence of `c2pa_ai_generated` does **not** mean "not AI" — most files carry no manifest.

## Wait for the next screenshot mentioning "invoice"

Bounded subscription — `watch_search` blocks up to `duration_seconds` (default 30, hard cap 600) and returns matches as they appear:

```json
{
  "name": "watch_search",
  "arguments": {
    "expr": "is_image && body.contains(\"invoice\")",
    "dir": "~/Desktop",
    "duration_seconds": 120,
    "max_events": 5,
    "ocr_images": true
  }
}
```

`ocr_images: true` runs the registered OCR provider (macOS Vision today) over each new image so `body.contains(...)` sees the recognised text. The first OCR per image is expensive (~200ms–2s); subsequent walks are free (body cache).

## High-churn files (refactor / review prioritisation)

`with_git: true` is auto-enabled by the `git_commit_count` reference in `expr` / `sort_by` — no need to pass it explicitly. First call after process start pays the ~500ms `git log` cost; subsequent calls are sub-10ms (HEAD-sha-validated `gitmeta.Pool`):

```json
{
  "name": "search",
  "arguments": {
    "expr": "is_git_tracked && git_commit_count > 0",
    "sort_by": "git_commit_count",
    "order": "desc",
    "limit": 20,
    "fields": ["git_commit_count", "git_last_commit_time", "git_last_commit_author", "loc"]
  }
}
```

This tracks **any** tracked file by churn — high-churn docs (markdown), config, and data files surface alongside source. Add `&& is_source` if you want to narrow to code only. Or just run the `hot_files` preset which bakes the same shape.

## Production code only (drop tests + codegen)

The "show me what humans wrote" filter — composites the `is_git_tracked` opt-in (#271) with `is_test_file` (per-language test convention) and `is_generated_code` (#276 codegen marker scan):

```json
{
  "name": "search",
  "arguments": {
    "expr": "is_source && is_git_tracked && !is_test_file && !is_generated_code",
    "sort_by": "loc",
    "order": "desc",
    "limit": 50,
    "profile": "code"
  }
}
```

`profile: "code"` skips non-source per-format parsing for the speedup. Or run the `prod_code` preset directly.

## "Did I forget to commit?"

Source files NOT in git AND not matched by `.gitignore` — catches new files an operator added but didn't `git add`:

```json
{
  "name": "search",
  "arguments": {
    "expr": "is_source && !is_git_tracked && !is_git_ignored",
    "sort_by": "size",
    "order": "desc",
    "limit": 50
  }
}
```

Or run the `untracked_code` preset.

## Comment-only TODO scan

`find_matches` with `match_in: "comments"` (#272) filters out TODO occurrences inside string literals, test fixtures, and identifiers — only fires on lines that begin with (or sit inside) a language-aware comment marker:

```json
{
  "name": "find_matches",
  "arguments": {
    "pattern": "(?i)\\bTODO\\b",
    "expr": "is_source",
    "match_in": "comments",
    "context_before": 1,
    "context_after": 1
  }
}
```

## Validate a CEL expression before running a walk

Cheap typo-check + "did you mean" suggestion before paying the walk setup cost. Particularly useful when the agent synthesised the expression from natural language:

```json
{ "name": "validate_expr", "arguments": { "expr": "is_markown && word_count > 500" } }
```

Returns `{"ok": false, "error": "undeclared reference to 'is_markown'", "suggestion": "is_markdown"}`.
