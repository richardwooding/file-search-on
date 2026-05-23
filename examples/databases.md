# Recipes — Databases

Content types: `database/sqlite` (the main DB), `database/sqlite-wal` (write-ahead log sidecar), `database/sqlite-shm` (shared-memory sidecar). The most-deployed database in the world: every iOS / Android app, every browser (Chrome `History`, Firefox `places.sqlite`), every CLI tool with a local store. Files named `.db`, `.sqlite`, `.sqlite3`, `.db3` are common under `~/Library`, `~/.config`, and app-data directories. WAL-mode databases (`sqlite_format_version == 2`) ship `.db-wal` + `.db-shm` companions next to the main file.

The parser reads the 100-byte SQLite header directly AND walks the `sqlite_master` b-tree page to surface schema metadata — pure stdlib, no third-party SQLite library. The 32-byte WAL header gives byte-order, page-size, and a best-effort frame count. Both header and schema attributes are populated automatically when an `is_sqlite` match fires.

Umbrella `is_database` extends to future DuckDB / PostgreSQL-dump / BoltDB additions. Sidecars (`is_sqlite_wal`, `is_sqlite_shm`) deliberately do NOT fire `is_database` / `is_sqlite` — they accompany a database, they aren't one. Compose with OR (`is_sqlite || is_sqlite_wal`) when a query wants the full trio.

## Find SQLite files

```sh
# All SQLite files under a directory
file-search-on 'is_sqlite' -d ~/Library

# Umbrella predicate (today same as is_sqlite; future-proof for new database/* types)
file-search-on 'is_database' -d ~

# Filter by application stamp — Firefox stamps places.sqlite with 0x0FACADE0
file-search-on 'is_sqlite && sqlite_application_id == 263849184' -d ~

# Find WAL-mode databases (sqlite_format_version == 2)
file-search-on 'is_sqlite && sqlite_format_version == 2' -d ~

# Large SQLite files by page count × page size
file-search-on 'is_sqlite && sqlite_page_count > 10000' -d ~ --sort sqlite_page_count --order desc

# UTF-16 encoded databases (uncommon but happens)
file-search-on 'is_sqlite && sqlite_text_encoding != "utf-8"' -d ~

# Files with non-zero user_version (app actively tracks schema version)
file-search-on 'is_sqlite && sqlite_user_version > 0' -d ~/.config
```

## Schema introspection

The b-tree walker reads `sqlite_master` and surfaces six per-DB attributes:

```sh
# Find DBs containing a 'history' table (Firefox places, Chrome History, etc.)
file-search-on 'is_sqlite && "history" in sqlite_table_names' -d ~/Library

# Find DBs with many tables — heavyweight schemas
file-search-on 'is_sqlite && sqlite_table_count > 20' -d ~/Library --sort sqlite_table_count --order desc

# Find DBs with triggers (often interesting for audit / change-tracking)
file-search-on 'is_sqlite && sqlite_trigger_count > 0' -d ~/Library -o verbose

# Find DBs with views (denormalised query layers)
file-search-on 'is_sqlite && sqlite_view_count > 0' -d ~/Library

# Match by known-good schema fingerprint (forensic / version detection)
file-search-on 'is_sqlite && sqlite_schema_fingerprint == "abc123..."' -d ~

# List the schema fingerprint of every Firefox places.sqlite under ~/Library
file-search-on 'is_sqlite && "moz_places" in sqlite_table_names' -d ~/Library -o json | \
  jq -r '"\(.path)\t\(.sqlite_schema_fingerprint)"'
```

The fingerprint is a SHA256 hex over sorted `(type, name, sql)` tuples from `sqlite_master` — stable across cosmetic CREATE-statement reorders, changes whenever any object's definition changes.

## WAL + SHM sidecars

A SQLite database in WAL mode pairs the main `.db` with `.db-wal` (write-ahead log) and `.db-shm` (shared memory) companion files. They detect as `database/sqlite-wal` and `database/sqlite-shm`. Use them to spot DBs with recent uncommitted activity, dangling sidecars after a crash, or — paired with `~/Library` walks — to see how many WAL-mode DBs are live on a system.

```sh
# Every SQLite database AND its sidecars — the full trio
file-search-on 'is_sqlite || is_sqlite_wal || is_sqlite_shm' -d ~/Library

# WAL files holding pending writes (large WALs = lots of uncheckpointed activity)
file-search-on 'is_sqlite_wal && size > 100000' -d ~/Library --sort size --order desc

# Forensic timeline: when was a DB last actively written? Sort WAL files by mod_time
file-search-on 'is_sqlite_wal' -d ~/Library --sort mod_time --order desc --limit 10

# WAL files with many frames (deep WAL = checkpoint hasn't run in a while)
file-search-on 'is_sqlite_wal && sqlite_wal_frame_count > 100' -d ~/Library -o verbose

# Discriminate byte-order variants (rare — most modern systems are little-endian)
file-search-on 'is_sqlite_wal && sqlite_wal_byte_order == "be"' -d ~

# Just the SHM presence indicator — a SHM file means at least one connection has touched the DB
file-search-on 'is_sqlite_shm' -d ~/Library
```

## Curated app-name registry

`sqlite_application_name` is a curated label populated when the file matches a known stamp / filename pattern — saves agents from looking up magic decimals. Today's registry covers the obvious browser and OS-bundled DBs: Firefox places, the Chromium-family History / Cookies stores (Chrome / Chromium / Edge / Brave), Apple Photos / iMessage / Keychain, macOS libcache, and Fossil SCM repositories. Empty when no entry matches — use the empty case to surface unknown DBs for triage.

```sh
# Find every Firefox places database — no need to remember 0x0FACADE0
file-search-on 'is_sqlite && sqlite_application_name == "firefox-places"' -d ~/Library

# All browser History databases regardless of vendor
file-search-on 'is_sqlite && sqlite_application_name in ["chrome-history", "chromium-history", "edge-history", "brave-history"]' -d ~

# macOS libcache files (per-app Cache.db with user_version == 203) — common cleanup target
file-search-on 'is_sqlite && sqlite_application_name == "macos-libcache"' -d ~/Library/Caches

# Surface SQLite DBs we don't recognise — manual triage candidates
file-search-on 'is_sqlite && sqlite_application_name == ""' -d ~/Library --limit 20

# iMessage chat history
file-search-on 'is_sqlite && sqlite_application_name == "apple-imessage"' -d ~/Library/Messages

# Any keychain on the system
file-search-on 'is_sqlite && sqlite_application_name == "apple-keychain"' -d ~/Library/Keychains
```

Adding a new entry is a one-line struct literal append in `internal/content/database_sqlite_registry.go`. Each entry combines optional `(ApplicationID, UserVersion, Filename, PathContains)` dimensions — every non-zero / non-empty dimension must match (AND, not OR).

## Triage workflow

```sh
# Find the SQLite files apps actually use (page_count > 1 = at least one page beyond the header)
file-search-on 'is_sqlite && sqlite_page_count > 1' -d ~/Library/Application\ Support -o verbose

# Empty / freshly-created SQLite files (might be stale lock files)
file-search-on 'is_sqlite && sqlite_page_count <= 1' -d ~ -o bare

# Cross-cutting: biggest disk-eating SQLite files
file-search-on 'is_sqlite' -d ~ --sort size --order desc --limit 10
```

## Known limitations

- **Schema introspection truncates overflow pages.** Very long `CREATE` statements that overflow the b-tree page into linked pages aren't followed — `sqlite_table_names` may miss tables whose CREATE statement spans pages (rare in practice), and `sqlite_schema_fingerprint` covers only the inline portion of overflowed records. Most schemas fit inline.
- **Index b-trees aren't walked.** `sqlite_master` itself is always a table b-tree, so this is fine for schema discovery. We don't read user-table contents (FTS5 indexed text is a separate follow-up).
- **WAL-aware reads.** The schema we see is from the canonical `.db` file; uncommitted writes in `-wal` aren't visible. For most use cases (find DBs containing X) this doesn't matter. The WAL file's 32-byte header is parsed for byte-order + page-size + frame-count metadata, but the WAL frame contents (the pending pages themselves) aren't read.
- **SQLCipher / encrypted databases.** SQLCipher encrypts everything including the SQLite magic bytes. Encrypted DBs detect as binary noise; we can't help without the key. v1 surfaces nothing.
- **SHM internal structure.** SHM file detection is extension-only — the on-disk layout is implementation-defined and not a stable contract. Mere presence indicates the parent DB is in WAL mode with active or recent connections; that's enough for triage.
- **`.db` overload.** Some non-SQLite formats use `.db` (Berkeley DB, etc.). The 16-byte SQLite magic prefix discriminates reliably — `is_sqlite` won't false-positive on Berkeley DB files.
