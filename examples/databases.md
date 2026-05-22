# Recipes — Databases

Content type: `database/sqlite` — SQLite v3 database files. The most-deployed database in the world: every iOS / Android app, every browser (Chrome `History`, Firefox `places.sqlite`), every CLI tool with a local store. Files named `.db`, `.sqlite`, `.sqlite3`, `.db3` are common under `~/Library`, `~/.config`, and app-data directories.

The parser reads the 100-byte SQLite header directly — pure stdlib, no third-party SQLite library. v1 scope is header-only: detection plus version / size / encoding fields. Schema introspection (table names, counts, fingerprint) requires a b-tree walker and is a follow-up.

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

- **No schema introspection in v1.** Table / view / index / trigger names, row counts, and schema fingerprint require walking `sqlite_master` via a b-tree parser. Filed as a follow-up — the issue body is in #170. The header-only path covers ~80% of "find SQLite files matching X" use cases without the dep weight of `modernc.org/sqlite`.
- **SQLCipher / encrypted databases.** SQLCipher encrypts everything including the SQLite magic bytes. Encrypted DBs detect as binary noise; we can't help without the key. v1 surfaces nothing.
- **WAL sidecar files.** A SQLite database in WAL mode (`sqlite_format_version == 2`) has a `-wal` companion file with recent uncommitted writes. The `-wal` file isn't currently registered as its own content type — it surfaces as `binary/elf` or unknown. Detection of `-wal` / `-shm` sidecars is a candidate for a follow-up.
- **`.db` overload.** Some non-SQLite formats use `.db` (Berkeley DB, etc.). The 16-byte SQLite magic prefix discriminates reliably — `is_sqlite` won't false-positive on Berkeley DB files.
