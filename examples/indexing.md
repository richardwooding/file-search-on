# Persistent attribute index

`file-search-on` parses every matching candidate file on every walk: PDF metadata, EXIF blocks, audio tags, markdown front-matter, ZIP headers, binary architectures. For trees that change rarely (a code repo, a Music library, an archive) this is wasted work — the *expression* changes between runs, but the underlying *attributes* don't.

`--index-path` plugs in an on-disk cache (a single bbolt file). Each entry is keyed by absolute path and validated against the file's `(size, mtime)` pair. Modify a file and only that entry is invalidated. Different CEL expressions reuse the same cache — the index stores attributes, not search results.

## CLI: cold + warm

```sh
# Cold: parses every file, stores the attribute payload in the bbolt file.
file-search-on 'is_markdown && word_count > 500' \
    -d ~/notes \
    --index-path ~/.cache/fso/notes.db

# Warm: same dir, different CEL expression, same cache. The PDF metadata,
# markdown frontmatter, image EXIF — all already cached. The walk still
# stats every file (mtime/size validation) but skips the per-file parse.
file-search-on 'is_pdf && page_count > 20' \
    -d ~/notes \
    --index-path ~/.cache/fso/notes.db
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

## CLI: forcing a refresh

The cache is invalidated automatically when a file's mtime or size changes. To force a full refresh of everything (e.g. after upgrading the binary across an attribute schema bump), delete the file:

```sh
rm ~/.cache/fso/notes.db
file-search-on 'is_markdown' -d ~/notes --index-path ~/.cache/fso/notes.db   # rebuilds
```

The tool will refuse to open an index file from an incompatible schema version with a message like:

```
Error: index file at ~/.cache/fso/notes.db has an incompatible schema; delete it or pass a new --index-path
```

We never auto-delete user files, even cache files — explicit `rm` is the recovery step.

## MCP: cache lives for the server lifetime

In MCP server mode the cache is **on by default** with no flag. It uses an in-memory implementation that lives for the server process. Agents that explore a tree iteratively get progressively faster `search` and `read_attributes` calls:

```sh
file-search-on mcp                                         # in-memory cache, lost on restart
file-search-on mcp --index-path ~/.cache/fso/agent.db      # bbolt-backed, survives restart
```

Hit/miss counters are exposed via the new `index_stats` tool (no input, returns `hits/misses/puts/stales/errors`):

```json
{
  "name": "index_stats",
  "arguments": {}
}
```

## When NOT to use it

- **One-shot scripts**, e.g. a `find`-like invocation in a CI pipeline that walks a fresh checkout. The cache file would be cold every time and add disk I/O for no benefit.
- **Trees that change every run**, like a build directory whose mtimes get bumped on every compile — every file is stale, every entry is invalidated, the cache is overhead.
- **Hostile inputs**, e.g. arbitrary user uploads. The encoder has a 256 KiB per-entry soft cap to defend against pathological frontmatter, but you still don't want to point the cache at content you don't trust.

## CI / cron / large libraries

For larger trees that you scan periodically (e.g. weekly photo backup, monthly archive triage), put the index file under `~/.cache/`:

```sh
# Photo library — full sweep monthly, then cheap delta queries during the month
mkdir -p ~/.cache/fso
file-search-on 'is_image && taken_at > timestamp("2024-01-01T00:00:00Z")' \
    -d ~/Pictures \
    --index-path ~/.cache/fso/photos.db
```

Subsequent queries against `~/Pictures` for *any* attribute (camera_model, GPS bounding box, ISO range) hit the cache. Only files that were added, edited, or moved since the last walk re-parse.

Bbolt is a single file, so it backs up trivially (`cp ~/.cache/fso/photos.db ~/backups/`). It's also safe to copy across machines as long as the absolute paths line up — the validator only checks `(size, mtime)`, not host or filesystem identity.

## Inspecting the cache

There is no built-in `index` subcommand for stats / vacuum / dump (yet). The MCP `index_stats` tool is the canonical observability surface; the CLI footer line is the equivalent for one-shot use. If your index file grows unwieldy after months of file churn, the simplest reset is to delete it.
