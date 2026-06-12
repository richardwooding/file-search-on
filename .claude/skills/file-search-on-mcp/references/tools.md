# Tool reference

Every MCP tool the `file-search-on` server exposes (33 tools). Each entry has a one-line purpose, the key inputs (omitting boilerplate like `timeout_seconds`), the output shape, gotchas worth knowing, and one example invocation. Grouped by the same families as the SKILL.md table.

## Contents

- Search & inspect — `search`, `search_semantic`, `list_embedding_models`, `pull_embedding_model`, `read_attributes`, `read_lines`
- Aggregate — `stats`
- Dedup & diff — `find_duplicates`, `find_near_duplicates`, `find_duplicate_functions`, `diff_trees`
- Archive — `list_archive_contents`, `read_file_in_archive`
- Pattern + watch — `find_matches`, `watch_search`
- Cross-file code graph — `imported_by`, `find_definition`, `code_graph`, `who_calls`, `calls`, `impact`, `dead_code`, `test_gaps`, `complexity`
- CEL utilities — `validate_expr`, `list_attributes`
- Project + presets + monitoring — `detect_project`, `find_projects`, `resolve_project_for_path`, `list_presets`, `query_preset`, `index_stats`, `monitor_info`

Common inputs shared by every walking tool: `dir` (default `.`, ignored when `dirs[]` is non-empty), `dirs[]`, `excludes[]` (basename globs), `respect_gitignore`, `follow_symlinks`, `workers` (default `runtime.NumCPU()`), `timeout_seconds` (override the server default; `0` disables; on expiry returns partial results with `cancelled=true`).

---

## Search & inspect

### `search`

Walk a directory tree and return files matching a CEL expression evaluated over file metadata + content-type attributes.

Key inputs:

- `expr` — CEL filter. Empty matches every file. See cel-vocabulary.md for predicates / attributes / functions.
- `sort_by` + `order` — buffered top-K. Recognised keys: `size`, `name`, `path`, `mod_time`, `word_count`, `line_count`, `page_count`, `duration`, `bitrate`, `sample_rate`, `video_height`, `video_width`, `frame_rate`, `iso`, `focal_length`, `taken_at`, `sent_at`, `year`, `entry_count`, `uncompressed_size`, `loc`, `attachment_count`, `email_count`, `git_last_commit_time`, `git_first_seen`, `git_commit_count`, `similarity`, `rank`. Per-family scalar attributes (e.g. `function_count`, `bitrate`) also work via the `Extra` fallback.
- `profile` — `"default"` (every content type's per-format parse runs) or `"code"` (skip non-source per-format parsing — image EXIF / audio tags / video tracks / archive entry walks etc. — for a ~5–10× speedup on source-heavy trees). Source files still parse fully; everything else falls back to common attrs only.
- `with_git` — populate the git-aware attributes (`is_git_tracked`, `is_git_ignored`, `git_last_commit_time`, `git_first_seen`, `git_commit_count`, `git_last_commit_author`, `git_last_commit_subject`). Auto-enabled when `expr`, `sort_by`, or `rank` references any `git_*` attribute (`celexpr.NeedsGit`). The server holds a HEAD-sha-validated `gitmeta.Pool` cache shared across calls — first call after process start or `git commit` pays the ~500ms `git log` cost; subsequent calls are sub-10ms.
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
- `hybrid` — fuse BM25 keyword relevance with embedding similarity via reciprocal-rank fusion; `keyword_query` overrides the BM25 query (defaults to `query`).
- `include_match_snippet` — inline the matched region's source as `match_snippet` on each hit (opt-in; text/source files only). `snippet_lines` caps it (default 60).

Output: `matches[]` ranked by `similarity` desc, `count`, `cancelled`, `cancellation_reason`, `elapsed_seconds`, `ann_used`. Each match locates **where** it matched (issue #366): `match_start_line` / `match_end_line` (1-based inclusive line range of the best-matching embedding chunk) and, for **source files** — which are embedded **one chunk per function** — `match_symbol` (the matching function / method name). With `include_match_snippet`, `match_snippet` carries that region's source text.

Gotchas:

- Requires a running Ollama with at least one embedding model pulled (e.g. `ollama pull nomic-embed-text`). The server boots without Ollama; the first call fails clearly if Ollama is unreachable or the model isn't pulled.
- The per-file embedding caches alongside (size, mtime); repeat searches against an unchanged tree are I/O-cheap. Source files are chunked by function span — the **first** search after upgrading to v0.91.0 re-embeds cached source files once (byte windows → function chunks); non-source stays a cache hit.
- `match_symbol` is empty when the winning chunk isn't a function (e.g. a file's leading package/imports/doc header) or for non-source files — `match_start_line`/`match_end_line` still pinpoint the region. `match_snippet` is only populated for line-addressable text/source types (structured bodies like PDF/office extract text whose lines don't map to disk).
- `similarity` is exposed as a CEL variable on each match so it composes with `rank` (e.g. `"rank": "similarity * 0.7 + (mod_time > timestamp(\"2025-01-01T00:00:00Z\") ? 0.3 : 0.0)"`).
- To fetch a matched function's full code, call `read_lines` on `[match_start_line, match_end_line]` (or set `include_match_snippet` to get it inline).

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

### `list_embedding_models`

List the embedding models the local Ollama server has pulled. Lets an agent enumerate options before calling `search_semantic` (which needs at least one model present).

Key inputs: `embedding_server` (override the server-startup default — usually `http://localhost:11434`).

Output: `server` (the Ollama URL queried), `local[]` — models actually pulled, each `{name, size_bytes, modified_at, digest, catalogued}` (`catalogued` = also in the recommended catalogue) — and `catalog[]` — recommended embedding models, each `{name, description, size, dimensions, pulled}` (`pulled` = already installed locally).

Gotchas:

- Returns an empty list (no error) when Ollama is reachable but no embedding models are pulled. Pair with `pull_embedding_model` to bootstrap.
- Errors clearly when Ollama is unreachable; the server boots without it.

### `pull_embedding_model`

Pull an embedding model into the local Ollama server. Long-running — streams progress lines back; the server reports the final total on completion.

Key inputs:

- `name` (required) — Ollama model identifier, e.g. `nomic-embed-text`, `mxbai-embed-large`, `all-minilm`.
- `embedding_server` — override the default Ollama URL.

Output: `name` (echoed model), `server` (Ollama URL), `already_pulled` (true when it was present before the call — then no download happened), `total_bytes` (downloaded), `duration_seconds`.

Gotchas:

- A typical embedding model is 100–700 MB. Run once per host; subsequent `search_semantic` calls reuse the pulled model.
- The MCP call holds open for the duration of the pull — use the per-call `timeout_seconds` generously (300–600s) for first-time pulls.

Example:

```json
{ "name": "pull_embedding_model", "arguments": { "name": "nomic-embed-text" } }
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
- `start_line` (1-indexed, inclusive; defaults to 1).
- `end_line` (1-indexed, inclusive; omit / 0 means EOF).
- `max_lines` — cap on lines returned (default 1000). When the requested range exceeds it, `truncated=true` and only the first `max_lines` of the range come back.

Output: `path`, `start_line`, `end_line`, `total_lines`, `lines[]`, `truncated`.

Gotchas:

- Reads any file's lines directly — no content-type gate (binary files yield raw byte-split lines). A per-line 64 KiB cap truncates pathologically long lines.
- Pair with `search` (to find files) and `find_matches` (to find lines) for the read-around-match flow; on `search_semantic` hits, read `[match_start_line, match_end_line]` to fetch the matched function.

Example:

```json
{ "name": "read_lines", "arguments": { "path": "./main.go", "start_line": 100, "end_line": 150 } }
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
- `threshold` (0..1, default 0.85 ≈ 9-bit Hamming distance). 0.95 ≈ whitespace-only edits; 0.75 ≈ significant structural overlap. **Auto-bump (#274)**: when the candidate set is source-heavy and the caller didn't pass an explicit threshold, the server bumps the floor to 0.92 to suppress the cross-language SimHash-convergence noise that swamped early dogfooding. Pass `threshold` explicitly to opt out.
- `min_size`, `body_max_bytes`.
- `members_limit_per_group`, `group_limit` (#279) — clamp result size. `members_limit_per_group` truncates each group's `members[]` list (top-similarity preserved); `group_limit` caps the number of groups returned. Useful when a dogfood run produces 50-member clusters that dominate token budget.

Output: `groups[]` sorted by member count desc. Each member `{path, similarity, size}`. Plus `group_count`, `fingerprinted`, partial-result fields.

Gotchas:

- Only text-shaped and structured-document types fingerprint (markdown, text, html, csv, json, xml, source/*, pdf, office, epub, email). Binary families excluded. Boilerplate strippers (license headers, codegen banners) run before SimHash so a tree of `// Code generated by protoc-gen-go.` files doesn't collapse into one giant cluster.
- Fingerprints cache in the index; repeat runs skip body extraction AND SimHash compute.
- A 156-member cluster at default threshold is usually SimHash convergence on Go / template boilerplate; the auto-bump should suppress this, but pass `threshold: 0.95` for typo-only edits.

Example:

```json
{
  "name": "find_near_duplicates",
  "arguments": { "dir": "~/Notes", "expr": "is_markdown", "threshold": 0.9 }
}
```

### `find_duplicate_functions`

Clusters of near-identical **functions** across the tree — copy-pasted logic the file-level `find_near_duplicates` misses (a duplicated helper inside two otherwise-distinct files never trips a whole-file fingerprint). Splits each source file into its functions (the per-function spans `complexity` / function-level semantic search use), SimHashes each body, and union-find groups within the threshold.

Key inputs:

- `expr` — CEL pre-filter; defaults to `is_source`. Non-source files have no function spans.
- `threshold` — SimHash similarity floor (0..1). Omit / 0 uses **0.92** (code SimHash sits high even for unrelated functions, so this is tighter than the prose default).
- `min_lines` — skip functions shorter than this; default 5 (trivial getters/wrappers fingerprint alike and would bury real duplication).
- `dir` / `dirs` / `excludes` / `respect_gitignore`.

Output: `groups[]` (member count desc, then total duplicated lines), each member `{path, symbol, start_line, end_line, lines, similarity}`; `functions_scanned`, `group_count`, `threshold`, `min_lines`. `read_lines` the `[start_line, end_line]` span to see the code.

Gotchas:

- Source languages only (Go + the tree-sitter set).
- Heuristic — SimHash matches token/structure shape, so functions sharing a skeleton but differing in intent can cluster. Review before extracting.
- Grouping is O(N²) over scanned functions; the `min_lines` filter keeps it tractable.

Example:

```json
{ "name": "find_duplicate_functions", "arguments": { "dir": ".", "expr": "language == \"go\"", "min_lines": 8 } }
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
- `match_in` (#272) — `"any"` (default), `"comments"`, or `"code"`. Per-language filter applied AFTER the regex matches the line: `comments` keeps only lines that are inside (or start with) a comment marker for the file's language; `code` is the inverse. Use to grep `TODO` annotations without firing on test fixture strings or doc references that happen to contain the token. Recognises C-family `//` + `/* */`, hash-family `#`, Lua/SQL `--`, Clojure/asm `;`, OCaml/Haskell `{- -}`. Files with unknown languages fall back to `any` regardless of the request.

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

## Cross-file code graph

Built by inverting the per-file `imports` / `functions` / `type_names` lists (the same data `search` surfaces) into a project-wide graph. One walk, then in-memory lookups. No extra dependencies. Answers the relationship questions per-file `search` can't. All three honour the partial-result contract (`cancelled=true` on timeout) and accept the shared walking inputs (`dir` / `dirs[]` / `excludes[]` / `respect_gitignore` / `follow_symlinks` / `workers` / `timeout_seconds`), plus `expr` (defaults to `is_source`).

### `imported_by`

Reverse-dependency lookup: every file that imports a given module.

Key inputs:

- `module` — the import string exactly as written in source (e.g. `github.com/spf13/cobra`, `numpy`, `react`). Required.
- `mode` — `exact` (default), `prefix` (module is a leading substring), or `regex` (RE2 against each import string).

Output: `importers[]` (`{path, language}`, sorted by path), `count`, `total_files`.

Gotcha: accurate for every language whose imports are extracted (Go via AST; Python / Java / C# / PHP / Perl / R / MATLAB / Scala). Other languages won't contribute edges.

Example — who depends on the internal `content` package:

```json
{
  "name": "imported_by",
  "arguments": {
    "module": "github.com/richardwooding/file-search-on/internal/content",
    "dir": "."
  }
}
```

### `find_definition`

Where a function or type is defined across the tree — symbol-aware, the complement to `find_matches` (which is text regex).

Key inputs:

- `symbol` — exact function or type name (not a substring). Required.
- `kind` — `function` / `type` / empty for both.

Output: `definitions[]` (`{path, language, kind}`, deduped per file per kind), `count`, `total_files`.

Gotcha: limited to the languages with symbol extraction (Go + Python / Java / C# / PHP / Perl / R / MATLAB / Scala). For others, fall back to `find_matches`.

Example:

```json
{
  "name": "find_definition",
  "arguments": { "symbol": "ServeHTTP", "kind": "function", "dir": "." }
}
```

### `code_graph`

Project-wide structure overview.

Key inputs:

- `top` — cap each ranked list (default 20).

Output: `overview` with `import_hubs` (modules by fan-in), `high_fan_out` (files by import count), `duplicate_definitions` (names defined in >1 file), `languages` (file counts), and `total_files` / `distinct_modules` / `distinct_symbols`.

Example — Go-only overview, top 10:

```json
{
  "name": "code_graph",
  "arguments": { "expr": "is_source && language == \"go\"", "top": 10, "dir": "." }
}
```

### `who_calls`

Reverse call lookup — every file that calls/references a symbol name.

Key inputs:

- `symbol` — exact function/method name (required). Name-based: `pkg.Foo()` / `x.Method()` key by `Foo` / `Method`.

Output: `callers[]` (`{path, language}`, sorted by path), `count`, `total_files`.

Gotcha: references are extracted for Go + the tree-sitter languages (Rust / TypeScript / JavaScript / Ruby / Swift / Kotlin / C / C++) only; callers in other languages won't appear.

```json
{ "name": "who_calls", "arguments": { "symbol": "ServeHTTP", "dir": "." } }
```

### `calls`

Forward call lookup — the distinct functions a given function calls ("what does Y call?").

Key inputs:

- `symbol` — exact function/method name (required).

Output: `callees[]` (sorted distinct names), `count`, `total_files`.

Gotcha: per-function attribution via span-containment (tree-sitter) / `go/ast` (Go); same language coverage and name-based caveats as `who_calls`. Callees include builtins/conversions where they appear by name (e.g. Go `len`, `append`); calls in nested closures attribute to the enclosing named function.

```json
{ "name": "calls", "arguments": { "symbol": "BuildCodeGraph", "dir": "." } }
```

### `impact`

Transitive reverse-dependency closure — every function that (in)directly calls `symbol`, the **blast radius** of changing it. `who_calls` is one hop; `impact` is the full closure with depth.

Key inputs:

- `symbol` — exact function/method name (required).
- `max_depth` — cap call hops; 0 (default) unbounded, 1 = direct callers only.

Output: `dependents[]` (`{symbol, depth, paths[]}`, depth asc then name; depth 1 = direct caller), `count`, `max_depth_reached`, `total_files`.

Gotcha: name-based BFS over the per-function call graph — same caveats as `who_calls` / `calls` (same-name collisions, interface / reflection dispatch). Cycles terminate via a visited set. The import-level equivalent ("what transitively imports this *file*") isn't available — it needs package resolution the graph doesn't carry.

```json
{ "name": "impact", "arguments": { "symbol": "BuildCodeGraph", "dir": "./internal", "max_depth": 3 } }
```

### `dead_code`

Candidate definitions (functions/types) whose name is never referenced anywhere in the walked set.

Output: `candidates[]` (`{path, language, kind, symbol}`, sorted by path), `count`, `total_files`.

**Gotcha — these are CANDIDATES, not authoritative.** Name-based heuristic; exported/public API used only externally, entry points (`main`), dynamic dispatch, reflection, and same-name collisions all produce false positives. Restricted to definitions in languages with reference extraction (Go + tree-sitter). Pair with `expr: "is_source && !is_test_file"` to drop test-runner-invoked functions. Use as a review starting point, never a delete list.

```json
{ "name": "dead_code", "arguments": { "expr": "is_source && language == \"go\" && !is_test_file", "dir": "." } }
```

### `test_gaps`

Production source files whose functions are never referenced from a test — candidate untested code, no coverage instrumentation required. Same machinery as `dead_code`, filtered to "not referenced from a `*_test` file": a function is *tested* when its name appears as a reference in any file flagged `is_test_file`.

Key inputs: `expr` (defaults to `is_source`), `dir` / `dirs` / `excludes` / `respect_gitignore`.

Output: `gaps[]` (`{path, language, function_count, untested_count, untested_functions[], fully_untested}`, sorted fully-untested-first then by `untested_count` desc), `count`, `total_files`. `fully_untested=true` means not one function in the file is referenced from a test (the clearest gaps).

**Gotcha — heuristic, direct-reference only.** Code exercised only transitively (a test calls `A`, which calls `B`, but no test names `B`) reads as untested; same-name collisions / reflection can mislead. Review candidates, not a coverage report — for precise line/branch coverage use a coverage profile. Restricted to reference-extraction languages (Go + tree-sitter); handles Rust inline `#[cfg(test)]` tests that a filename-sibling check would miss.

```json
{ "name": "test_gaps", "arguments": { "expr": "is_source && language == \"go\"", "dir": "./internal" } }
```

### `complexity`

Functions ranked by cyclomatic complexity, worst-first — maintenance hotspots.

Key inputs:

- `top` — cap on functions returned (default 50).

Output: `functions[]` (`{path, function, complexity, start_line, end_line, lines}`, sorted by complexity desc), `total_functions`.

Gotcha: gocyclo-style (1 + branch points). Coverage = Go + the tree-sitter languages. Directional for *ranking* hotspots — the exact number depends on per-grammar node coverage, not a certified metric. For a file-level filter use the search tool's `max_complexity` attribute; this is the per-function drill-down.

```json
{ "name": "complexity", "arguments": { "expr": "is_source && language == \"go\"", "top": 20, "dir": "." } }
```

---

## CEL utilities

### `validate_expr`

Compile-check a CEL expression without running a walk. Returns whether it parses and type-checks against the live schema, plus "did you mean…" suggestions when an attribute name is misspelled.

Key inputs:

- `expr` (required) — the CEL expression to validate.

Output: `ok` (bool), `error` (compile error message when invalid), `suggestion` (a single Levenshtein-nearest attribute name when the error names an undeclared reference — e.g. typo'd `is_markown` returns `is_markdown`), plus `referenced_variables` / `referenced_functions` (the names the expression touched, on success).

Gotchas:

- Use BEFORE running a long walk to catch typos cheaply (a `search` call with a typo'd `expr` errors at parse time, but you've already paid setup cost). Particularly useful when an agent is synthesising expressions from user input.
- The same schema as `search` — `validate_expr` and `search` agree on every attribute / function reference.

Example:

```json
{ "name": "validate_expr", "arguments": { "expr": "is_markown && word_count > 500" } }
```

Returns `{"ok": false, "error": "undeclared reference to 'is_markown'", "suggestion": "is_markdown"}`.

### `list_attributes`

List every CEL attribute available to `search`, the built-in functions with their signatures, and every registered content type. Use to discover what's filterable / sortable / projectable at runtime; the canonical source of attribute names.

Key inputs (#273):

- `mode` — `"full"` (default), `"summary"`, `"section"`, or `"names"`. Controls payload size:
  - `full` — every attribute with its CEL type and description, every function with signature + description, every content type with name + extensions. Largest output (~30 KB).
  - `summary` — counts only (attribute groups, function count, content-type count). Cheap.
  - `section` — pair with `section` to fetch exactly one slice (`common` / `type_specific` / `frontmatter` / `functions` / `content_types`).
  - `names` — pair with `section`; returns just the bare names, no types or descriptions. Cheapest non-empty payload — use when an agent only needs to enumerate identifiers.
- `section` — required when `mode` is `section` or `names`. One of `common`, `type_specific`, `frontmatter`, `functions`, `content_types`.

Output: `attributes` (grouped by `common` / `type_specific` / `frontmatter`), `functions[]` (name, signature, description), `content_types[]`. With `mode: "summary"` only the counts; with `mode: "section"` or `"names"` only the named slice is populated.

Read this first when an agent isn't sure which attribute to filter on; pair with `validate_expr` to confirm the chosen attribute name compiles.

---

## Project + presets + monitoring

### `detect_project`

Inspect a single directory and report which project type(s) match based on canonical indicator files. Non-recursive.

Key inputs: `dir` (defaults to `.`).

Output: `path` (the directory inspected), `project_types[]` (the matching type names, e.g. `go-module`, `node`), and `indicators[]` — each `{type, indicator}` pairing a matched type with the file/glob that triggered it. Empty `project_types` means no known type matched.

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

### `list_presets`

List every named search recipe ('preset'). Pass the name to `query_preset` to run. See SKILL.md's preset table for the fourteen built-ins (eight filesystem + six repo-aware).

Output: `presets[]` with `name`, `description`.

### `query_preset`

Run a named preset. Repo-aware presets (`recent_commits`, `hot_files`, `hotspots`, `prod_code`, `untracked_code`) auto-enable `with_git` via `celexpr.NeedsGit` — no separate opt-in required. `hotspots` ranks source files by complexity × churn (`max_complexity` × `git_commit_count`) — the 'what to refactor first' list.

Key inputs:

- `name` (required) — one of the fourteen preset names. Call `list_presets` for the live catalog.
- `dir` / `dirs[]`, `limit`, `excludes`, `respect_gitignore`, `follow_symlinks` — per-call overrides.

Output: same `Match` shape as `search`.

Example:

```json
{ "name": "query_preset", "arguments": { "name": "hot_files", "dir": "." } }
```

### `index_stats`

Cumulative attribute-cache counters for the running MCP server. Counters reset on restart.

Output: `hits`, `misses`, `puts`, `stales`, `errors`, plus `body_*` and `embed_*` analogues (body cache, embedding cache). When the server was launched with `--watch-index` (the background cache maintainer; auto-on under `--warm`), also reports `watch_refreshed` (cached files re-parsed after an edit), `watch_evicted` (entries dropped for deleted files), and `watch_errors`. Zero on a server without the watcher.

Useful for diagnosing cache effectiveness when a search feels slower than expected.

### `monitor_info`

Report this server's monitoring-dashboard URL + the registry of sibling instances (other concurrently-running `file-search-on` processes that have a dashboard). Pass `enable=true` to start this server's dashboard on a dynamic localhost port if it wasn't launched with `--monitor` / `--monitor-addr`.

Key inputs:

- `enable` — when true, start the dashboard on demand (idempotent — same URL on repeat calls).

Output: `enabled` (bool), `url` (this server's dashboard URL), `peers[]` (each `{pid, url, mode, working_dir, version, started_at, is_self}`), `note` (human hint).

Use the URL in a browser to see live cache stats, tool-call activity, capabilities, and a peer switcher.
