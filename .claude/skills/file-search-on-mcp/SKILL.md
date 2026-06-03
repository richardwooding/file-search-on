---
name: file-search-on-mcp
description: Uses the file-search-on MCP server (23 tools — search / search_semantic / validate_expr / stats / find_duplicates / find_near_duplicates / diff_trees / find_matches / list_archive_contents / read_file_in_archive / watch_search / detect_project / find_projects / resolve_project_for_path / read_attributes / read_lines / list_attributes / list_presets / query_preset / list_embedding_models / pull_embedding_model / index_stats / monitor_info) to query files by typed content-type attributes via CEL expressions — PDF page_count, image EXIF (camera/lens/GPS/taken_at), audio tags + bitrate, video codec/duration, archive entry_count, binary architecture, source LOC + functions + imports, notebook cells, markdown frontmatter, git-aware (last commit time / author / churn) and codegen-aware (is_generated_code) attributes, plus body.contains / body.matches, fuzzy / phonetic / geo / image-similarity / secret-scan helpers, sha256 dedup + SimHash near-dup, cross-tree diff, and project-type detection. Use when the question is about file *attributes* (not filenames as with Glob or plain text as with Grep), when answers need aggregation or dedup, or when an agent needs photo / source / archive / email / binary metadata.
---

# file-search-on MCP

`file-search-on` is a content-type-aware file search exposed as a Model Context Protocol server. Once registered with an MCP client, it gives an agent **typed access to file metadata** — not just paths and bytes but image EXIF, PDF page counts, audio bitrates, source-code LOC, archive entries, email headers, GPS coordinates, git-aware churn / authorship, and so on — through 23 tools driven by a CEL expression language.

This skill is the agent-facing usage guide. The codebase ships at `github.com/richardwooding/file-search-on`. The MCP launch shape is `file-search-on mcp` (stdio for desktop clients) or `file-search-on mcp --transport http --addr :8080` (Streamable HTTP). The skill assumes the server is already running and registered with the client.

## When to reach for this MCP

| Question shape | Use |
| --- | --- |
| "Find files named `X`" or "paths matching a glob" | Glob |
| "Find files containing literal text `X`" (single regex over plain files) | Grep |
| "Find files **by attribute** — biggest videos, photos near a GPS bbox, source with > 200 LOC, PDFs with > 10 pages, drafts older than 90 days" | this MCP — `search` |
| "What's in this folder?" — count by content type / language / camera / extension | this MCP — `stats` |
| "Find duplicates" (byte-identical) or **near**-duplicates (typo-edit / template) | this MCP — `find_duplicates` / `find_near_duplicates` |
| "Find regex hits with surrounding context" pruned by attribute | this MCP — `find_matches` |
| "What's in tree A but not in tree B?" | this MCP — `diff_trees` |
| "What project does this path belong to?" | this MCP — `detect_project` / `find_projects` / `resolve_project_for_path` |
| "Conceptual / paraphrase-tolerant search" via embeddings | this MCP — `search_semantic` |

If a question can be answered by listing paths or grepping bytes, the built-in tools are cheaper. Anything that needs *typed* understanding of the file — including aggregation, dedup, project context, fuzzy match, or geo filtering — belongs here.

## Quick start

Three canonical calls that demonstrate the patterns. Every walking tool accepts `dir` (default `.`), `expr` (CEL filter, empty = match all), `timeout_seconds` (override the server default), and `excludes` / `respect_gitignore` for walk pruning.

**1. CEL filter** — biggest markdown long-reads:

```json
{
  "name": "search",
  "arguments": {
    "expr": "is_markdown && word_count > 500",
    "dir": "~/Documents/notes"
  }
}
```

**2. Top-K sort + fields projection** — five longest videos, only the fields needed:

```json
{
  "name": "search",
  "arguments": {
    "expr": "is_video",
    "dir": "~/Movies",
    "sort_by": "duration",
    "order": "desc",
    "limit": 5,
    "fields": ["duration", "video_codec", "video_height"]
  }
}
```

**3. Body content filter** — Go source mentioning `panic` (only candidates that are text are body-read; `expr` keeps the body pass narrow):

```json
{
  "name": "search",
  "arguments": {
    "expr": "is_source && language == \"go\" && body.contains(\"panic\")",
    "dir": "./internal",
    "include_body": true
  }
}
```

## The 23 tools at a glance

Grouped into seven families. Full inputs / outputs / gotchas in [references/tools.md](references/tools.md).

| Family | Tools | Use for |
| --- | --- | --- |
| **Search & inspect** | `search`, `search_semantic`, `read_attributes`, `read_lines` | The everyday surface: walk a tree under a CEL filter, embed-rank by natural language, fetch attributes for one path, or pull a line range |
| **Aggregate** | `stats` | Histograms + totals bucketed by `group_by` (content_type, language, camera_make, ext, dir, …) |
| **Dedup & diff** | `find_duplicates`, `find_near_duplicates`, `diff_trees` | Byte-identical groups by sha256, fuzzy groups via SimHash on body (auto-bumps threshold for source-heavy trees + strips boilerplate), or cross-tree set ops (a-minus-b / intersect / mismatch) |
| **Archive** | `list_archive_contents`, `read_file_in_archive` | Per-entry CEL filter inside ZIP / TAR / TAR.GZ / GZIP without extracting, or pull one entry's bytes out |
| **Pattern + watch** | `find_matches`, `watch_search` | Line-level RE2 regex with context windows + `match_in: any\|comments\|code` per-language filter (CEL pre-prune for speed), or a bounded "tell me when X appears" subscription |
| **CEL utilities** | `validate_expr`, `list_attributes` | Compile-only CEL validator with "did you mean" Levenshtein suggestions; schema discovery with summary / section / names modes |
| **Project + presets + monitoring** | `detect_project`, `find_projects`, `resolve_project_for_path`, `list_presets`, `query_preset`, `list_embedding_models`, `pull_embedding_model`, `index_stats`, `monitor_info` | Project-root detection (18 built-in types), 14 named recipe presets, on-demand Ollama embedding model catalog + pull, cache stats, live dashboard URL + peer instances |

## CEL essentials

The `expr` input is a [CEL](https://github.com/google/cel-spec) expression evaluated per file. Two layers:

**Family predicates** — boolean `is_X` types you can use directly without calling `list_attributes` first:

- File-family: `is_markdown`, `is_pdf`, `is_html`, `is_xml`, `is_json`, `is_yaml`, `is_toml`, `is_csv`, `is_text`, `is_image`, `is_audio`, `is_video`, `is_office`, `is_epub`, `is_archive`, `is_binary`, `is_email`, `is_source`, `is_notebook`
- Specialised umbrellas: `is_disk_image`, `is_install_package`, `is_bytecode`, `is_science_data`, `is_database`, `is_bookmark_file`, `is_chat_export`, `is_font`, `is_raw_photo`, `is_3d_model`, `is_system_metadata`
- Exact-name (filename-matched): `is_dockerfile`, `is_makefile`, `is_gomod`, `is_node_manifest`, `is_cargo_manifest`, `is_license`, `is_changelog`, `is_gitignore`, `is_codeowners`, `is_procfile`, `is_vagrantfile`, and family umbrellas `is_build` / `is_manifest` / `is_repo_meta` / `is_ignore` / `is_platform`
- Source code: `is_test_file` (per-language test convention), `is_generated_code` (codegen marker in first ~20 lines — Go `// Code generated ... DO NOT EDIT.`, Python `# Generated by`, C# `// <auto-generated>`, cross-language `@generated`)
- Git-aware (repo trees only; auto-warms via expression detection): `is_git_tracked`, `is_git_ignored`, plus the scalar attrs `git_last_commit_time`, `git_last_commit_author`, `git_last_commit_subject`, `git_first_seen`, `git_commit_count`
- State / forensic: `is_symlink`, `is_broken_symlink`, `is_btime_anomaly`, `is_disguised`, `is_known_good`, `is_known_bad`, `is_quarantined`, `is_codesigned`, `is_live_photo`

The full predicate catalogue + per-family attributes + functions live in [references/cel-vocabulary.md](references/cel-vocabulary.md). Call `list_attributes` to enumerate the live schema at runtime.

**Common scalar attributes** every file gets: `name`, `path`, `dir`, `ext`, `size` (bytes), `content_type`, `mod_time` / `created_at` / `metadata_changed_at` (timestamps), `title`, `author`, `language`.

**Body filters** — pass `include_body: true` (capped at `body_max_bytes`, default 1 MiB) to expose the file body as `body` so CEL string methods fire:

- `body.contains("substring")`
- `body.matches("(?i)\\bregex\\b")` — RE2
- `body.startsWith(...)`, `body.endsWith(...)`, `size(body)`

**Built-in functions** (no setup):

- `levenshtein(a, b)`, `soundex(s)`, `ngram_similarity(a, b, n)` — fuzzy / phonetic
- `point_in_polygon(lat, lon, poly)` — GPS bbox
- `image_similar_to(phash, ref_path, threshold)` — perceptual-hash similarity
- `has_secrets(body)`, `secret_kinds(body)` — credential pattern scan

## The partial-result contract

Every walking tool (`search`, `stats`, `find_duplicates`, `find_near_duplicates`, `find_matches`, `find_projects`, `diff_trees`) **does not error on timeout**. It returns the partial set with:

- `cancelled: true`
- `cancellation_reason: "timeout"` or `"client_cancel"`
- `elapsed_seconds: <float>`

**Always check `cancelled` before treating results as exhaustive.** Many tools also return `suggestions[]` with agent-actionable hints (e.g. "narrow with `is_source`", "exclude `node_modules`"). The per-call `timeout_seconds` input overrides the server default; `timeout_seconds: 0` disables the deadline for that call.

## Token-saving `fields` projection

`search` and `read_attributes` accept `fields: ["attr1", "attr2"]` to project each match to just the listed attributes. `path`, `content_type`, and `size` are always included. Use whenever the agent only needs a few attributes per match — for top-K queries over 50–100 matches the savings compound. Unknown names error at request validation; call `list_attributes` for the canonical list.

## Presets

Fourteen baked recipes ship with the server. Discover via `list_presets`, run via `query_preset`. Each preset bakes a vetted CEL filter + sensible sort / limit defaults. The repo-aware ones auto-enable `with_git`.

**Filesystem presets** (work on any tree):

| Preset | What it returns |
| --- | --- |
| `recent_changes` | Files modified in the last 7 days, newest first (uses `mod_time` — degrades on fresh clones; prefer `recent_commits` for repos) |
| `recent_photos` | Images taken in the last 30 days, newest first |
| `old_drafts` | Markdown drafts not modified in the last 90 days |
| `large_files` | Files larger than 100 MB across all formats |
| `large_binaries` | Compiled binaries larger than 100 MB |
| `suspicious_files` | Disguised files (magic ≠ extension) or btime anomalies |
| `failed_tests` | Test files with `FAIL` / `FIXME` / `XXX` / `TODO` in COMMENTS (line-anchored, not raw substring) |
| `system_metadata` | OS leftovers — `.DS_Store`, `Thumbs.db`, `Desktop.ini`, `.directory` |

**Repo-aware presets** (auto-enable `with_git` via expression / sort references):

| Preset | What it returns |
| --- | --- |
| `recent_commits` | Files most recently committed in the last 7 days, sorted by commit time (the git-aware sibling of `recent_changes`) |
| `hot_files` | Top-20 highest-churn tracked files (any type — source, docs, config) by `git_commit_count` desc — refactor / review prioritisation |
| `prod_code` | `is_source && is_git_tracked && !is_test_file && !is_generated_code`, top 100 by LOC — human-written production code |
| `untracked_code` | Source files not in git and not gitignored — the "did I forget to commit?" check |
| `generated_code` | Source files matching codegen markers (`is_generated_code`) |
| `test_files` | Source files matching the per-language test convention, top 50 by LOC |

Per-call overrides on `query_preset`: `dir` / `dirs`, `limit`, `excludes`, `respect_gitignore`, `follow_symlinks`.

## References

- [references/tools.md](references/tools.md) — every tool's inputs / output / gotchas / example invocation.
- [references/cel-vocabulary.md](references/cel-vocabulary.md) — full predicate catalogue, attributes by family, function signatures.
- [references/recipes.md](references/recipes.md) — copy-pasteable scenarios (top-K videos, GPS bbox, TODO grep, dedup photos, near-dup markdown, secret-scan, cross-tree diff, neglected drafts).
- [references/foot-guns.md](references/foot-guns.md) — partial-result semantics, path resolution, body cost, archive caveats, text-only families.

## Conventions

- `dir` defaults to `.` (the server's working directory). Pass an absolute path or `~/...` for clarity in stdio mode where the agent's cwd is the server's cwd.
- Empty `expr` matches every file. Tight predicates are dramatically cheaper than tight regexes — prefer `expr: "is_source && language == \"go\""` over `pattern` matching every file.
- Pair `include_body: true` with a tight `expr` — reading every candidate's body is the most expensive thing the server does.
- Time fields are timestamps; compare with `timestamp("2025-01-01T00:00:00Z")`.
- The CLI subcommands (`file-search-on monitors`, `file-search-on diff`, etc.) are siblings, not part of this MCP surface; see the project README for those.
