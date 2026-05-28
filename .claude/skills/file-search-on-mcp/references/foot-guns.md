# Foot-guns

The non-obvious pitfalls of the `file-search-on` MCP surface. Read before composing a non-trivial query against a large tree.

## Partial-result semantics

Every walking tool (`search`, `stats`, `find_duplicates`, `find_near_duplicates`, `find_matches`, `find_projects`, `diff_trees`) **does not error on timeout**. It returns the partial set with:

- `cancelled: true`
- `cancellation_reason: "timeout"` or `"client_cancel"`
- `elapsed_seconds: <float>`

The same `cancelled=true` fires whether the deadline expired OR the MCP client cancelled the request. **Distinguish via `cancellation_reason`** before deciding whether to retry with a higher `timeout_seconds` or report the user-cancel back. Treat the result set as a **subset**, never as exhaustive, when `cancelled` is true.

Some tools also return `suggestions[]` with agent-actionable hints (e.g. "narrow with `is_source`", "exclude `node_modules`", "pass `respect_gitignore: true`"). Surface those before retrying blindly.

## Paths are resolved in the server's working directory

In **stdio** mode the MCP server inherits the launching process's cwd. Relative paths (`./`, `src/`) resolve against the server's cwd, **not** the agent's. Two safe habits:

- Pass absolute paths.
- Use `~/...` — the server expands the leading `~` to the user's home dir.

`~` expansion happens in the server, not the agent — `"~/Pictures"` works even when the agent's shell isn't bash.

## `include_body` is the most expensive flag

When `include_body: true`, the server reads every candidate file's body (up to `body_max_bytes`, default 1 MiB) so the `body` CEL variable is populated. On a tree of thousands of files this is gigabytes of I/O.

Mitigations:

- Pair with a tight type predicate first: `is_source && language == "go" && body.contains("panic")` is dramatically cheaper than `body.contains("panic")` alone.
- For "find regex hits" without needing paths-only filtering, prefer `find_matches` — it streams body lines and only opens text content types.
- Set `body_max_bytes` lower (e.g. 256 KiB) for code searches where headers carry the signal.

## Archive entries have pseudo-paths

`list_archive_contents` returns entries with paths *inside* the archive (e.g. `src/main.go`). Those are not file system paths — you cannot pass them to `read_attributes` or `read_lines`. Use `read_file_in_archive` with the same `entry_path` string to pull the bytes out:

```json
{ "name": "read_file_in_archive", "arguments": { "path": "./source.tar.gz", "entry_path": "src/main.go" } }
```

Archive entry detection runs the magic-byte sniffer on the entry's bytes (first 512 against a synthetic in-memory FS), so a `.go` file inside a tarball detects as `source/go` — useful, but be aware the detection is independent of any outer-archive metadata.

## `find_matches` is text-only

`find_matches` filters candidates to text content types (`markdown`, `text`, `html`, `csv`, `json`, `xml`, `source/*`) — binary families (image, audio, video, archive, binary, office, epub, email) are silently dropped. If a search across, say, `.docx` body content matters, use `search` with `include_body: true` + `body.matches(...)`; the office body extractor walks the ZIP+XML and surfaces paragraph text as `body`.

The per-line scanner caps at 64 KiB per line — minified JSON / rolled-up logs that exceed the cap are truncated to that cap rather than skipped. The truncation is silent; assume "no match" on a giant line could mean "match exists past the cap".

## SimHash near-duplicates over-cluster boilerplate

`find_near_duplicates` default `threshold: 0.85` (≈ 9-bit Hamming distance on the 64-bit hash) is generous. On a corpus heavy in Go / template / license-header boilerplate, expect one giant false cluster of 100+ members. Tighten to `0.95` for typo-only / whitespace edits or `0.90` for "real" near-dups; loosen below 0.80 only when explicitly hunting structural overlap.

Only text-shaped + structured-document types fingerprint (markdown / text / html / csv / json / xml / source/* / pdf / office / epub / email). Binary families return zero fingerprints and are silently excluded.

## `diff_trees` is content-based by default

`a-minus-b`, `b-minus-a`, `intersect`, `union` all key on **sha256 content hash**, not relative path. A file *renamed* between A and B (same bytes, different path) counts as "in both" — it appears in `intersect` and `union`, never in `a-minus-b`. Use `op: "mismatch"` to compare by relative path (and report when same-named files have different content). Zero-byte files are skipped.

## Compute opt-ins are opt-in for a reason

- `compute_hashes: true` reads every candidate file in full (MD5 + SHA1 + SHA256 in one `io.MultiWriter` pass). On large trees this is multi-GB. Cached `(size, mtime)`-validated alongside attributes — second runs are free.
- `with_phash: true` decodes every image. ~1 ms per image; not free on a tree of 50 000 photos. Auto-enabled when `expr` references `image_similar_to`.
- `ocr_images: true` invokes the registered OCR provider (macOS Vision today) per image: 200 ms – 2 s per image on the first walk, free on subsequent walks (body cache). On non-Darwin platforms it's a no-op.
- `with_xattrs: true` is Darwin-only; non-Darwin walks leave the xattr family empty. Two syscalls per match.
- `check_disguised: true` adds one extra 512-byte read per match (cached).

If a query doesn't need the field, leave the flag off — defaults are picked for cheap-and-correct.

## `sort_by` requires buffering the full result set

Top-K queries (`sort_by` + `limit`) collect every match first, then sort, then truncate. On a 10 M-file tree with a permissive `expr`, that's a heap of matches in memory before the cut. Combine `sort_by` with a tight `expr` and meaningful `excludes` to keep the buffer small. The streaming `WalkStream` path (no `sort_by`) returns matches in walk order without buffering.

## `field` projection happens AFTER sorting

`sort_by` still works on attributes not in the `fields[]` list — sort runs before projection. So `sort_by: "taken_at", fields: ["path"]` is valid and sorts photos by EXIF time even though `taken_at` isn't in the response. Useful for token savings without losing sort semantics.

## `excludes` is basename-only

`excludes` matches the **basename** of each file/directory against the listed globs (`node_modules`, `.git`, `target`, `*.bak`). It does not match against the full path — `excludes: ["src/old"]` does NOT match `~/proj/src/old`. For path-aware patterns, use `respect_gitignore: true` and let the `.gitignore` semantics handle it; for one-off path prunes, list the deepest unique basename.

`prune_build_artefacts: true` is the convenience shortcut: pre-walks each root to detect project types and prunes the canonical build-artefact basenames per type (`vendor`, `node_modules`, `target`, `__pycache__`, `.terraform`, `bin`, `obj`, …). Worth the pre-walk on monorepos.

## Walk and watch are independent surfaces

`watch_search` blocks up to `duration_seconds` (default 30, hard-capped at 600) and returns the matches seen during that window — it doesn't see files that already exist when the call starts. To combine "current matches + future matches", make a `search` call first and a `watch_search` call after.

`watch_search` only sees CREATE + WRITE events. Deletes and renames don't surface. New subdirectories created during the watch are picked up automatically (recursive registration on CREATE), but files written into a subdirectory in the narrow window before its watch is armed can be missed.

## The cache is per-process, not per-tool-call

`index_stats` reports the running server's cumulative cache counters. Hits stay hot for the server's lifetime; a server restart resets everything. If running in stdio mode the server is spawned per-MCP-session — restarts on every fresh client connection. For persistent caching across runs, launch with `--index-path <file.db>` so the bbolt-backed cache survives restarts; otherwise the in-memory cache is fine but rebuilds on each spawn.

The body cache has its own cap (`--body-cache-max-bytes`, default 256 MiB) and FIFO eviction — `BodyEvictions` and `BodyOversize` counters in `index_stats` flag pressure.
