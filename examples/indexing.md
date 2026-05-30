# Persistent attribute index

`file-search-on` parses every matching candidate file on every walk: PDF metadata, EXIF blocks, audio tags, markdown front-matter, ZIP headers, binary architectures. For trees that change rarely (a code repo, a Music library, an archive) this is wasted work — the *expression* changes between runs, but the underlying *attributes* don't.

Since v0.64.0 the on-disk index is **on by default**. Every subcommand that walks a tree (`search`, `stats`, `find_duplicates`, `find_near_duplicates`, `find_matches`, `diff`, `archive-contents`, `organize`, `near-duplicates`, `preset`, `attrs`, `watch`, `mcp`) auto-creates a per-cwd bbolt cache the first time it runs. Each entry is keyed by absolute path and validated against the file's `(size, mtime)` pair. Modify a file and only that entry is invalidated. Different CEL expressions reuse the same cache — the index stores attributes, not search results.

## Default path scheme

```
<UserCacheDir>/file-search-on/indexes/<basename(cwd)>-<sha1(abs(cwd))[:6]>.db
```

- **macOS**:   `~/Library/Caches/file-search-on/indexes/file-search-on-3a7b2c.db`
- **Linux**:   `$XDG_CACHE_HOME/file-search-on/indexes/...` (or `~/.cache/...`)
- **Windows**: `%LOCALAPPDATA%/file-search-on/indexes/...`

The `<basename>-<shorthash>` form is readable in `ls` and collision-free across projects that share a basename. Different cwds get different files; the same cwd is deterministic. Per-cwd keying means multiple concurrent stdio agents across different projects never fight for the same bbolt file.

## CLI: cold + warm

```sh
# Cold: parses every file, stores the attribute payload in the default
# per-cwd bbolt file. No flag needed.
file-search-on 'is_markdown && word_count > 500' -d ~/notes

# Warm: same dir, different CEL expression, same cache. PDF metadata,
# markdown frontmatter, image EXIF — all already cached. The walk still
# stats every file (mtime/size validation) but skips the per-file parse.
file-search-on 'is_pdf && page_count > 20' -d ~/notes
```

After each run, the CLI prints a stderr footer:

```
index: 1234 hits, 56 misses, 56 stored, 0 stale, 0 errors
```

- **hits** — file's `(size, mtime)` matched a cached entry; no parse.
- **misses** — no entry yet; the file was parsed and stored.
- **stored** — entries written this run (will equal `misses` on the cold path; should be `0` on a fully-warm run).
- **stale** — entry existed but `(size, mtime)` mismatched; entry was discarded and the file was re-parsed.
- **errors** — encoding failures, oversized payloads, write-channel back-pressure. Cache misses are always recoverable; errors are diagnostic.

## Override the default path

`--index-path <file.db>` overrides the per-cwd default. Useful when you want to share a single cache across multiple cwds, or pin the location for backup / shared-CI scenarios:

```sh
file-search-on 'is_image' -d ~/Pictures --index-path /Volumes/Backup/photos.db
```

## Opt out: `--no-index`

`--no-index` disables the on-disk index entirely for that run; the tool falls back to a process-lifetime in-memory cache. Useful for:

- **Hermetic CI** — tests / scripts that should leave no disk side effects.
- **One-shot runs** — a single grep-like invocation where the cache file is overhead.
- **Lock-contention bypass** — see below.

```sh
file-search-on 'is_source' -d ./project --no-index
```

## Multi-instance: lock contention falls back gracefully

bbolt is a single-writer database. When two stdio agents share the same cwd:

1. The first wins the file lock.
2. The second's `Open` blocks 5 s, then transparently falls back to in-memory caching for the rest of its session. A one-line stderr warning surfaces the fact:

```
file-search-on: index file <path>.db is held by another file-search-on instance;
this session will use in-memory cache (use --no-index to silence this warning)
```

Both agents keep working; only the cache layer differs for the loser. The first agent's writes accumulate normally; the second restarts cold next time. The monitor dashboard's Overview panel shows the backend state with a badge: 🔒 persistent / 🧠 in-memory + reason (`lock_contention` or `no_index_flag`).

Projects with **different cwds** → different bbolt files → zero contention. This is the common case (multiple Claude Code sessions across several repos at once).

## CLI: forcing a refresh

The cache is invalidated automatically when a file's mtime or size changes. To force a full refresh of everything (e.g. after upgrading the binary across an attribute schema bump), delete the file:

```sh
rm ~/Library/Caches/file-search-on/indexes/file-search-on-3a7b2c.db
file-search-on 'is_markdown' -d ~/notes   # rebuilds
```

The tool will refuse to open an index file from an incompatible schema version with a message like:

```
Error: index file at <path>.db has an incompatible schema; delete it or pass a new --index-path
```

We never auto-delete user files, even cache files — explicit `rm` is the recovery step.

## MCP server: same default-on behaviour

`file-search-on mcp` inherits the same per-cwd default + same `--index-path` / `--no-index` overrides. Agents that explore a tree iteratively get progressively faster `search` and `read_attributes` calls; the cache survives MCP server restarts because it's on disk by default:

```sh
file-search-on mcp                                    # default per-cwd index
file-search-on mcp --index-path /var/lib/fso.db       # explicit shared path
file-search-on mcp --no-monitor --no-index            # hermetic / no side effects
```

Hit/miss counters are exposed via the `index_stats` MCP tool (no input, returns `hits/misses/puts/stales/errors` for attributes + body + embedding caches) and live on the monitor dashboard's Cache panel.

```json
{ "name": "index_stats", "arguments": {} }
```

## When NOT to use it

- **One-shot scripts** in a hermetic CI where any disk side effect is unwanted — pass `--no-index`.
- **Trees that change every run**, like a build directory whose mtimes get bumped on every compile — every file is stale, every entry is invalidated, the cache is overhead. Either ignore that subtree via `--exclude` / `--prune-build-artefacts` or pass `--no-index`.
- **Hostile inputs**, e.g. arbitrary user uploads. The encoder has a 256 KiB per-entry soft cap to defend against pathological frontmatter, but you still don't want to point the cache at content you don't trust.

## CI / cron / large libraries

For trees that you scan periodically (e.g. weekly photo backup, monthly archive triage), the per-cwd default works out of the box — running `file-search-on` from the same cwd each time hits the same cache file:

```sh
# Photo library — full sweep monthly, then cheap delta queries during the month
cd ~/Pictures
file-search-on 'is_image && taken_at > timestamp("2024-01-01T00:00:00Z")'
```

Subsequent queries against `~/Pictures` for *any* attribute (camera_model, GPS bounding box, ISO range) hit the cache. Only files that were added, edited, or moved since the last walk re-parse.

Bbolt is a single file, so it backs up trivially (`cp ~/Library/Caches/file-search-on/indexes/<file>.db ~/backups/`). It's also safe to copy across machines as long as the absolute paths line up — the validator only checks `(size, mtime)`, not host or filesystem identity.

## Inspecting the cache

There is no built-in `index` subcommand for stats / vacuum / dump (yet). The MCP `index_stats` tool is the canonical observability surface; the CLI footer line is the equivalent for one-shot use. The monitor dashboard surfaces the same counters live with sparklines. If your index file grows unwieldy after months of file churn, the simplest reset is to delete it.
