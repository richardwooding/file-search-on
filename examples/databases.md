# Recipes — Databases

Content type: `database/sqlite` — SQLite v3 database files. The most-deployed database in the world: every iOS / Android app, every browser (Chrome `History`, Firefox `places.sqlite`), every CLI tool with a local store. Files named `.db`, `.sqlite`, `.sqlite3`, `.db3` are common under `~/Library`, `~/.config`, and app-data directories.

The parser reads the 100-byte SQLite header directly AND walks the `sqlite_master` b-tree page to surface schema metadata — pure stdlib, no third-party SQLite library. Both header and schema attributes are populated automatically when an `is_sqlite` match fires.

Umbrella `is_database` extends to future DuckDB / PostgreSQL-dump / BoltDB additions.

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
- **WAL-aware reads.** The schema we see is from the canonical `.db` file; uncommitted writes in `-wal` aren't visible. For most use cases (find DBs containing X) this doesn't matter.
- **SQLCipher / encrypted databases.** SQLCipher encrypts everything including the SQLite magic bytes. Encrypted DBs detect as binary noise; we can't help without the key. v1 surfaces nothing.
- **WAL sidecar files.** A SQLite database in WAL mode (`sqlite_format_version == 2`) has a `-wal` companion file with recent uncommitted writes. The `-wal` file isn't currently registered as its own content type — it surfaces as `binary/elf` or unknown. Detection of `-wal` / `-shm` sidecars is a candidate for a follow-up.
- **`.db` overload.** Some non-SQLite formats use `.db` (Berkeley DB, etc.). The 16-byte SQLite magic prefix discriminates reliably — `is_sqlite` won't false-positive on Berkeley DB files.
