# Cross-tree diff — set operations across two trees

The `diff` subcommand (and the matching MCP `diff_trees` tool) compares **two file trees by content hash** and reports the requested set operation. Where [`duplicates`](duplicates.md) answers "what's duplicated *within* this tree?", `diff` answers the inverse: "what's in tree A that's *not* in tree B?", "what content do they share?", "which same-named files have drifted?".

Read-only — `diff` never moves, copies, or deletes anything. It's a discovery tool.

## When to reach for this

| Question | Op |
| --- | --- |
| "What's on my external drive that I don't have locally?" — pre-backup sanity | `a-minus-b` (A = drive, B = local) |
| "What content do these two snapshots share?" | `intersect` |
| "Every distinct file across both trees" | `union` |
| "Which files have the same name but different content?" — drift detection | `mismatch` |
| "What's in the new snapshot that's missing from the old?" — incremental migration | `b-minus-a` |

## How it works

Both trees are walked, every file is hashed (SHA-256), and the chosen set operation runs over the hashes:

- **`a-minus-b`** (default): files whose content (by hash) is in A but absent from B.
- **`b-minus-a`**: the inverse.
- **`intersect`**: content present (by hash) in both trees.
- **`union`**: every distinct content hash across both.
- **`mismatch`**: files that share a *relative path* between the trees but whose content differs — keyed by path, not hash.

Hashes are read from / written to the attribute index (`--index-path`) alongside `(size, mtime)`, so two warm trees diff in seconds — no bytes are re-read for unchanged files. The same hash cache is shared with `duplicates`, `--with-hashes`, and the hash-set predicates.

`--expr` scopes which files are considered in **both** trees before hashing, so you can restrict the diff to a subset:

```sh
file-search-on diff ~/A ~/B --op a-minus-b --expr 'size > 1000000'   # only large files
file-search-on diff ~/A ~/B --op mismatch --expr 'is_source'         # only source-code drift
```

## CLI

```sh
# Pre-backup: what's in ~/Pictures that the backup drive is missing?
file-search-on diff ~/Pictures /Volumes/Backup/Pictures --op a-minus-b

# Content shared between two snapshots
file-search-on diff ./snapshot-jan ./snapshot-feb --op intersect

# Same-named files whose content drifted between two checkouts
file-search-on diff ./checkout-a ./checkout-b --op mismatch

# Warm both trees into an index so the next diff is near-instant
file-search-on diff ~/A ~/B --op union --index-path ~/.fso-index.db

# Human-readable table instead of NDJSON
file-search-on diff ~/A ~/B --op a-minus-b -o table
```

Default output is **NDJSON** — one record per line, ready for `jq`:

```json
{"status":"only_in_a","path_a":"/Users/me/Pictures/IMG_4021.jpg","sha256":"a6f9…56d1","size":2304882}
{"status":"only_in_a","path_a":"/Users/me/Pictures/IMG_4022.jpg","sha256":"3df0…81e7","size":1981233}
```

```sh
# Total bytes present in A but not B
file-search-on diff ~/A ~/B --op a-minus-b | jq -s 'map(.size) | add'

# Just the paths missing from the backup
file-search-on diff ~/Pictures /Volumes/Backup --op a-minus-b | jq -r .path_a
```

Each record carries `status` (one of `only_in_a` / `only_in_b` / `both` / `name_match_content_differs`), `path_a` and/or `path_b`, `sha256`, and `size`. For `mismatch`, `sha256` is empty (the two sides differ by definition) and both paths are populated.

**Exit codes**: `0` on success, `124` on `--timeout`, `130` on Ctrl-C. Partial results are printed before exiting on timeout / interrupt.

## MCP

```json
{
  "name": "diff_trees",
  "arguments": {
    "tree_a": "/Users/me/Pictures",
    "tree_b": "/Volumes/Backup/Pictures",
    "op": "a-minus-b",
    "expr": "is_image"
  }
}
```

Response:

```json
{
  "op": "a-minus-b",
  "records": [
    {"status": "only_in_a", "path_a": "/Users/me/Pictures/IMG_4021.jpg", "sha256": "a6f9…56d1", "size": 2304882}
  ],
  "count": 1,
  "total_a": 812,
  "total_b": 811,
  "elapsed_seconds": 0.4
}
```

Inputs mirror the CLI: `op`, `expr`, `excludes`, `respect_gitignore`, `follow_symlinks`, `min_size`, `timeout_seconds`. On timeout the partial result is returned with `cancelled=true`, never an error.

## Pitfalls

- **`a-minus-b` is by content, not name.** A file renamed between the trees still counts as "in both" — its bytes are identical, only the path changed. Use `mismatch` when you care about same-path-different-content; use `a-minus-b` / `b-minus-a` when you care about content presence regardless of path.
- **`mismatch` only fires on shared relative paths.** A file present in A at `docs/x.md` and in B at `notes/x.md` is NOT a mismatch (different relative paths); it surfaces under the hash-based ops instead.
- **Cold diffs read every candidate file in full.** The first diff of two large trees hashes everything — pair with `--index-path` so the second run is free. Scope with `--expr` / `--min-size` / `--exclude` to cut the candidate set.
- **Zero-byte files are skipped.** Like `duplicates`, empty files aren't hashed (they'd all collide trivially).

## Related recipes

- [`duplicates.md`](duplicates.md) — the within-a-tree counterpart (find byte-identical copies in one tree).
- [`near-duplicates.md`](near-duplicates.md) — fuzzy similarity (SimHash) when you want "almost the same", not "byte-identical".
- [`indexing.md`](indexing.md) — `--index-path` hash caching that makes repeat diffs near-instant.
- [`forensics.md`](forensics.md) — `--with-hashes` and hash-set allow/deny lists built on the same hash plumbing.
