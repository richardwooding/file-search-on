# Tool reference

Every MCP tool the `file-search-on` server exposes. Each entry has a one-line purpose, the key inputs (omitting boilerplate like `timeout_seconds`), the output shape, gotchas worth knowing, and one example invocation. Grouped by the same six families as the SKILL.md table.

## Contents

- Search & inspect — `search`, `search_semantic`, `read_attributes`, `read_lines`
- Aggregate — `stats`
- Dedup & diff — `find_duplicates`, `find_near_duplicates`, `diff_trees`
- Archive — `list_archive_contents`, `read_file_in_archive`
- Pattern + watch — `find_matches`, `watch_search`
- Project + introspection + monitoring — `detect_project`, `find_projects`, `resolve_project_for_path`, `list_attributes`, `list_presets`, `query_preset`, `index_stats`, `monitor_info`

Common inputs shared by every walking tool: `dir` (default `.`, ignored when `dirs[]` is non-empty), `dirs[]`, `excludes[]` (basename globs), `respect_gitignore`, `follow_symlinks`, `workers` (default `runtime.NumCPU()`), `timeout_seconds` (override the server default; `0` disables; on expiry returns partial results with `cancelled=true`).

---

## Search & inspect

### `search`

Walk a directory tree and return files matching a CEL expression evaluated over file metadata + content-type attributes.

Key inputs:

- `expr` — CEL filter. Empty matches every file. See cel-vocabulary.md for predicates / attributes / functions.
- `sort_by` + `order` — buffered top-K. Recognised keys: `size`, `name`, `path`, `mod_time`, `word_count`, `line_count`, `page_count`, `duration`, `bitrate`, `sample_rate`, `video_height`, `video_width`, `frame_rate`, `iso`, `focal_length`, `taken_at`, `sent_at`, `year`, `entry_count`, `uncompressed_size`, `loc`, `attachment_count`, `email_count`.
- `limit` — cap. With `sort_by` it's top-N after sorting; without, it's first-N in walk order.
- `rank` — CEL expression returning double / int / bool, evaluated per file as a custom sort key. Higher ranks first. Overrides `sort_by` when set; composes with `similarity` for semantic re-rank.
- `include_snippet` + `snippet_lines` — populate `match.snippet` with first N body lines (text content types only).
- `include_body` + `body_max_bytes` — expose the file body as the `body` CEL variable so `body.contains` / `body.matches` fire. Expensive; pair with a tight `expr`.
- `compute_hashes` — populate `md5` / `sha1` / `sha256` as CEL variables (forensic).
- `check_disguised` — populate `magic_content_type` / `extension_content_type` / `is_disguised` (off by default).
- `with_xattrs` — populate the xattr family (`is_quarantined`, `quarantine_source_url`, `finder_tags`, …). Darwin only.
- `ocr_images` + `ocr_timeout_ms` — run OCR over `image/*` files (macOS Vision); populates `body` + `ocr_*`.
- `with_phash` — compute the perceptual hash; auto-enabled when `expr` references `image_similar_to`.
- `resolve_projects` — populate `project_types` / `project_type` per match.
- `prune_build_artefacts` — pre-walk to find project roots and prune `vendor`, `node_modules`, `target`, `__pycache__`, etc.
- `fields[]` — project each match to just these attributes. `path` / `content_type` / `size` always included.
- `hash_allowlist_path` / `hash_denylist_path` — NSRL / IOC interop; populate `is_known_good` / `is_known_bad`.

Output: `matches[]` of the `Match` shape (path + content_type + size + every populated attribute), `count`, `cancelled`, `cancellation_reason`, `elapsed_seconds`, `suggestions[]`. The `Match` schema is the canonical wire shape — every CEL attribute the matched content type emits is included unless `fields` filters.

Gotchas:

- The `Match` shape uses snake_case JSON keys matching CEL names (`taken_at`, `img_width`, `gps_lat`, …).
- Sorting buffers the full result set; streaming + sort are incompatible. Combine `expr` and `excludes` to keep the buffer small.
- `include_body` reads every candidate's body — pair with a tight type predicate.

Example:

```json
{
  "name": "search",
  "arguments": {
    "expr": "is_image && iso > 1600 && gps_lat != 0.0",
    "dir": "~/Pictures",
    "sort_by": "taken_at",
    "order": "desc",
    "limit": 20,
    "fields": ["taken_at", "camera_model", "iso", "gps_lat", "gps_lon"]
  }
}
```

### `search_semantic`

Semantic similarity search via local Ollama embeddings. Returns files ranked by **conceptual** similarity to a natural-language query; paraphrase / synonym / topic-level matches surface even when the exact words don't appear in the body.

Key inputs:

- `query` (required) — natural-language search string.
- `threshold` — cosine similarity floor (0..1, default 0.5). 0.7+ = strict topical match; 0.4–0.5 = loose / related.
- `limit` — top-K cap (default 50).
- `expr` — CEL pre-filter (scope to `is_pdf || is_office` etc.).
- `model`, `embedding_server` — per-call overrides for the server-startup defaults.

Output: `matches[]` ranked by `similarity` desc, `count`, `cancelled`, `cancellation_reason`, `elapsed_seconds`.

Gotchas:

- Requires a running Ollama with at least one embedding model pulled (e.g. `ollama pull nomic-embed-text`). The server boots without Ollama; the first call fails clearly if Ollama is unreachable or the model isn't pulled.
- The per-file embedding caches alongside (size, mtime); repeat searches against an unchanged tree are I/O-cheap.
- `similarity` is exposed as a CEL variable on each match so it composes with `rank` (e.g. `"rank": "similarity * 0.7 + (mod_time > timestamp(\"2025-01-01T00:00:00Z\") ? 0.3 : 0.0)"`).

Example:

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

### `read_attributes`

Extract content-type attributes for **one** file path — same shape as a single `search` match but without walking a directory.

Key inputs:

- `path` (required) — absolute or `~/...` path.
- `fields[]` — same projection trick as `search`.
- `compute_hashes`, `check_disguised`, `with_xattrs`, `hash_allowlist_path`, `hash_denylist_path` — same as `search`.

Output: a single `Match` object.

Gotchas:

- No CEL filter (the agent already knows the path). Use when piping a single discovered path through to its attribute set.
- Returns an error (not a partial result) when the file is missing.

Example:

```json
{
  "name": "read_attributes",
  "arguments": {
    "path": "~/Pictures/IMG_4021.jpg",
    "fields": ["taken_at", "camera_make", "camera_model", "gps_lat", "gps_lon", "iso", "focal_length"]
  }
}
```

### `read_lines`

Return a contiguous line range of a single file (1-indexed, inclusive).

Key inputs:

- `path` (required).
- `start` (1-indexed).
- `end` (inclusive; omit / 0 means EOF).
- `max_line_bytes` — per-line scanner cap; pathological long lines are truncated.

Output: `lines[]`, `total_lines`, `truncated` (true when any line exceeded the cap), `content_type`.

Gotchas:

- Only text content types serve content (binary families return empty).
- Pair with `search` (to find files) and `find_matches` (to find lines) for the read-around-match flow.

Example:

```json
{ "name": "read_lines", "arguments": { "path": "./main.go", "start": 100, "end": 150 } }
```

---

## Aggregate

### `stats`

Histogram + totals for a directory tree, bucketed by an attribute.

Key inputs:

- `expr` — optional CEL pre-prune (e.g. `is_image` for photos-only).
- `group_by` — bucket key. Default `content_type`. Recognised: `content_type`, `ext`, `dir`, `language`, `camera_make`, `camera_model`, `lens`, `artist`, `album`, `genre`, `kernel`, `binary_format`, `binary_type`, `frontmatter_format`, plus time buckets `mtime_year` / `mtime_month` / `mtime_day` / `taken_at_year` / `taken_at_month` / `taken_at_day` / `sent_at_*` / `date_*`.

Output: `groups[]` (sorted by count desc) with `name`, `count`, `total_size`; `total_count`, `total_size`; legacy `content_types[]` (populated only for the default `group_by`); plus the usual partial-result fields.

Gotchas:

- Unknown `group_by` falls back to `content_type` silently.
- Stats has a detector-only fast path when `expr` is trivial — much faster than a full attribute parse.

Example — photos by camera:

```json
{
  "name": "stats",
  "arguments": { "dir": "~/Pictures", "expr": "is_image", "group_by": "camera_make" }
}
```

---

## Dedup & diff

### `find_duplicates`

Find groups of **byte-identical** files keyed by sha256. Two-pass: unique-size pre-bucket, then hash only candidates in size collisions.

Key inputs:

- `expr` — optional CEL scope (`is_image` for photo dedup; `is_archive` for archive dedup).
- `min_size` — skip files smaller than this many bytes.

Output: `duplicates[]` sorted by `wasted_bytes` desc, each group `{hash, size, count, wasted_bytes, paths[]}`. Plus `total_files`, `duplicate_groups`, `wasted_bytes`, partial-result fields.

Gotchas:

- Hashes cache in the attribute index alongside `(size, mtime)` — repeat runs on unchanged files are free; first run on a large tree can be slow.
- Zero-byte files are dropped silently.

Example:

```json
{
  "name": "find_duplicates",
  "arguments": { "dir": "~/Pictures", "expr": "is_image", "min_size": 65536 }
}
```

### `find_near_duplicates`

Find groups of **similar** (not identical) files via 64-bit Charikar SimHash of their extracted body. Catches typo-edits, regenerated headers, template copies — what `find_duplicates` misses.

Key inputs:

- `expr` — pre-prune.
- `threshold` (0..1, default 0.85 ≈ 9-bit Hamming distance). 0.95 ≈ whitespace-only edits; 0.75 ≈ significant structural overlap.
- `min_size`, `body_max_bytes`.

Output: `groups[]` sorted by member count desc. Each member `{path, similarity, size}`. Plus `group_count`, `fingerprinted`, partial-result fields.

Gotchas:

- Only text-shaped and structured-document types fingerprint (markdown, text, html, csv, json, xml, source/*, pdf, office, epub, email). Binary families excluded.
- Fingerprints cache in the index; repeat runs skip body extraction AND SimHash compute.
- A 156-member cluster at default threshold is usually SimHash convergence on Go / template boilerplate; tighten to 0.95 for typo-only edits.

Example:

```json
{
  "name": "find_near_duplicates",
  "arguments": { "dir": "~/Notes", "expr": "is_markdown", "threshold": 0.9 }
}
```

### `diff_trees`

Cross-tree set operations by sha256. Read-only — never mutates either tree.

Key inputs:

- `tree_a`, `tree_b` (required).
- `op` — `a-minus-b` (default; content in A but not B), `b-minus-a`, `intersect` (in both), `union` (all distinct), `mismatch` (same relative path, different content — drift detection).
- `expr` — CEL pre-prune applied to both trees.
- `min_size`.

Output: `records[]` sorted by (path_a, path_b, sha256), each `{status, path_a, path_b, sha256, size}` where `status ∈ only_in_a | only_in_b | both | name_match_content_differs`. Plus `op`, `count`, `total_a`, `total_b`, partial-result fields.

Gotchas:

- Hash-based ops match on **content**, so a renamed file counts as "in both". Use `mismatch` when you specifically care about same-path-different-content.
- Hashes cache the same as `find_duplicates`; two warm trees diff in seconds.
- Zero-byte files are skipped.

Example — what's on the external drive that the local copy is missing:

```json
{
  "name": "diff_trees",
  "arguments": { "tree_a": "/Volumes/Backup/Pictures", "tree_b": "~/Pictures", "op": "a-minus-b" }
}
```

---

## Archive

### `list_archive_contents`

List or filter entries inside a ZIP / TAR / TAR.GZ / GZIP archive **without extracting**. Per-entry CEL evaluation against the same vocabulary the top-level search uses.

Key inputs:

- `path` (required) — the archive.
- `expr` — CEL filter applied per entry (`is_source && language == "go"`, `is_dockerfile`, …).
- `glob` — basename pattern applied BEFORE the CEL pass.
- `include_attributes` — off by default (terse name/size/content_type). On = full per-entry attributes.
- `include_body` — read entry bodies so `body.contains` fires; bypasses the entry-list cache.
- `max_entries` — cap.

Output: `entries[]` sorted by walk order, each with `name`, `size`, `content_type`, optional attributes. `cache_hit` flag.

Gotchas:

- Detection runs on each entry's bytes (first 512 sniffed against a synthetic in-memory FS), so `src/main.go` inside a tarball detects as `source/go`.
- Entry-list cache uses the attribute index; archives with > 10000 entries skip the cache.

Example — find every Go file inside a release tarball with > 200 LOC:

```json
{
  "name": "list_archive_contents",
  "arguments": {
    "path": "./release.tar.gz",
    "expr": "is_source && language == \"go\" && loc > 200",
    "include_attributes": true
  }
}
```

### `read_file_in_archive`

Read a single named file's bytes out of an archive without extracting.

Key inputs:

- `path` (required) — the archive.
- `entry_path` (required) — must match an entry exactly (no glob).
- `max_bytes` — cap (default 1 MiB).

Output: `content` (UTF-8 string when valid text) **or** `content_base64` (raw bytes), `content_type`, attributes, `truncated` flag.

Gotchas:

- Errors with entry-not-found when `entry_path` doesn't match.
- For text files, prefer `content`; binary entries surface as base64.

Example — pull pyproject.toml out of a source tarball:

```json
{
  "name": "read_file_in_archive",
  "arguments": { "path": "./source.tar.gz", "entry_path": "pyproject.toml" }
}
```

---

## Pattern + watch

### `find_matches`

Scan a directory tree for lines matching an RE2 regex with optional before/after context windows. Combines CEL pre-prune with line-level scan — pick candidate files cheaply by type, then run the regex only on what's left.

Key inputs:

- `pattern` (required) — RE2 regex.
- `expr` — CEL pre-filter (`is_source && language == "go"`).
- `context_before`, `context_after` — context window per hit.
- `max_matches_per_file` — cap per file (the scanner keeps reading past the cap until pending After windows are filled).
- `prune_build_artefacts` — pre-walk + prune `vendor` / `node_modules` / `target` / `__pycache__` / etc.

Output: `matches[]` sorted by (path, line), each `{path, content_type, line, text, before[], after[]}`. Plus `count`, `files_scanned`, `files_with_matches`, partial-result fields.

Gotchas:

- **Only text content types participate** — `markdown`, `text`, `html`, `csv`, `json`, `xml`, `source/*`. Binary families (image, audio, video, archive, binary, office, epub, email) are silently dropped.
- Pathological long lines truncated at 64 KiB per line.
- `expr` accepts the same predicates as `search`, but `pattern` is not passed through CEL — for "paths only" use `search` with `include_body` + `body.matches`.

Example:

```json
{
  "name": "find_matches",
  "arguments": {
    "pattern": "(?i)\\bTODO\\b",
    "expr": "is_source",
    "context_before": 2,
    "context_after": 2
  }
}
```

### `watch_search`

Watch a directory tree for a **bounded** window and return every new / changed file that matches a CEL expression. The inverse of `search` — "tell me when X appears" instead of "what X is here now".

Key inputs:

- `expr` — CEL filter (same vocabulary as `search`).
- `duration_seconds` — how long to watch. Default 30s, hard-capped at 600s.
- `max_events` — return early once this many matches collected.
- `include_body`, `body_max_bytes`, `ocr_images`, `compute_hashes`, `with_phash`, `with_xattrs` — same as `search`.

Output: `matches[]` (same shape as `search`), `watched_seconds`, `hit_max_events`.

Gotchas:

- This is a **bounded** subscription, not an open-ended stream — MCP is request/response. For unbounded streaming use the CLI `watch` subcommand.
- Watch is recursive (subdirectories created during the window are picked up automatically).
- Only CREATE + WRITE events are considered; deletes / renames are out of scope.
- 300 ms debounce coalesces editor multi-write bursts.

Example — wait for a screenshot mentioning "error":

```json
{
  "name": "watch_search",
  "arguments": {
    "expr": "is_image && body.contains(\"error\")",
    "dir": "~/Desktop",
    "duration_seconds": 60,
    "max_events": 5,
    "ocr_images": true
  }
}
```

---

## Project + introspection + monitoring

### `detect_project`

Inspect a single directory and report which project type(s) match based on canonical indicator files. Non-recursive.

Key inputs: `dir` (defaults to `.`).

Output: `matches[]` with `name` (project type), `description`, `indicator` (the file that matched), `path`.

Built-in project types: `go` (go.mod), `node` (package.json), `rust` (Cargo.toml), `python` (pyproject.toml / requirements.txt / Pipfile), `ruby` (Gemfile), `java-maven` (pom.xml), `java-gradle` (build.gradle), `dotnet` (*.csproj), `terraform` (*.tf), `docker-compose` (docker-compose.yml); plus static-site generators `hugo` / `jekyll` / `eleventy` / `astro` / `gatsby` / `mkdocs` / `docusaurus` / `pelican`. A directory can match multiple types simultaneously.

Example:

```json
{ "name": "detect_project", "arguments": { "dir": "~/Code/my-monorepo" } }
```

### `find_projects`

Walk a root directory and return every project root found.

Key inputs:

- `dir` / `dirs[]`.
- `nested` — when `true`, surfaces sub-projects inside matched roots (monorepo workspaces, vendored deps). Default `false` stops at first match per branch.
- `types[]` — filter to specific project types (e.g. `["go", "rust"]`).
- `excludes[]`, `respect_gitignore`.

Output: `projects[]` (path + matched types + indicators), partial-result fields.

Example — every Go module under `~/Code`:

```json
{ "name": "find_projects", "arguments": { "dir": "~/Code", "types": ["go"] } }
```

### `resolve_project_for_path`

Given an arbitrary file or directory path, walk UP the directory chain (unbounded) and return the nearest ancestor matching a registered project type.

Key inputs: `path` (required).

Output: `project_root` (matched directory; empty when no ancestor matches), `project_types[]` (all matching types — a Go module that also ships `docker-compose.yml` hits both), `indicators[]`.

Gotcha: walks up to filesystem root before giving up; safe but rarely needed for paths inside `/tmp` / `~/Downloads`.

Example:

```json
{
  "name": "resolve_project_for_path",
  "arguments": { "path": "~/Code/some-repo/internal/auth/session.go" }
}
```

### `list_attributes`

List every CEL attribute available to `search`, the built-in functions with their signatures, and every registered content type. Use to discover what's filterable / sortable / projectable at runtime; the canonical source of attribute names.

Output: `attributes` (grouped by `common` / `type_specific` / `frontmatter`), `functions[]` (name, signature, description), `content_types[]`.

No inputs. Cheap — read it first when an agent isn't sure which attribute to filter on.

### `list_presets`

List every named search recipe ('preset'). Pass the name to `query_preset` to run. See SKILL.md's preset table for the eight built-ins.

Output: `presets[]` with `name`, `description`.

### `query_preset`

Run a named preset.

Key inputs:

- `name` (required) — one of the eight preset names.
- `dir` / `dirs[]`, `limit`, `excludes`, `respect_gitignore`, `follow_symlinks` — per-call overrides.

Output: same `Match` shape as `search`.

Example:

```json
{ "name": "query_preset", "arguments": { "name": "large_files", "dir": "~/" } }
```

### `index_stats`

Cumulative attribute-cache counters for the running MCP server. Counters reset on restart.

Output: `hits`, `misses`, `puts`, `stales`, `errors`, plus `body_*` and `embed_*` analogues (body cache, embedding cache).

Useful for diagnosing cache effectiveness when a search feels slower than expected.

### `monitor_info`

Report this server's monitoring-dashboard URL + the registry of sibling instances (other concurrently-running `file-search-on` processes that have a dashboard). Pass `enable=true` to start this server's dashboard on a dynamic localhost port if it wasn't launched with `--monitor` / `--monitor-addr`.

Key inputs:

- `enable` — when true, start the dashboard on demand (idempotent — same URL on repeat calls).

Output: `enabled` (bool), `url` (this server's dashboard URL), `peers[]` (each `{pid, url, mode, working_dir, version, started_at, is_self}`), `note` (human hint).

Use the URL in a browser to see live cache stats, tool-call activity, capabilities, and a peer switcher.
