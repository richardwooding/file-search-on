# Duplicate file detection

The `duplicates` subcommand (and matching MCP `find_duplicates` tool) reports groups of **byte-identical files** keyed by sha256. The headline use case is "what's eating my disk?" — finding redundant copies that can be safely deleted.

## How it works

Two-pass for performance:

1. **Size bucketing.** Walk the tree; bucket files by their exact byte size. Files with a unique size cannot be duplicates of anything (size is invariant under content), so they're skipped from the expensive second pass.
2. **Hashing collision groups.** For each size bucket with ≥ 2 files, read each file in full and compute its sha256. Group by hash; emit groups with count > 1.

With `--index-path`, hashes are cached alongside the rest of the attribute entry (validated by `(size, mtime)`). **First runs on large trees can be slow** — every candidate file is read in full — but **subsequent runs on unchanged files are free**.

## CLI

```sh
# Whole tree
file-search-on duplicates -d ~/Downloads

# Photos only — scope to one content family
file-search-on duplicates 'is_image' -d ~/Pictures

# Skip tiny duplicates that aren't worth reclaiming
file-search-on duplicates -d . --min-size 4096

# Combine: large duplicate images
file-search-on duplicates 'is_image && size > 1000000' -d ~/Pictures

# With persistent cache so repeat runs are cheap
file-search-on duplicates -d ~/Music --index-path ~/.cache/fso/music.db

# JSON output for piping into jq
file-search-on duplicates -d ~/Music -o json | jq '.duplicates[] | select(.wasted_bytes > 10000000)'
```

Output (table mode, sorted by wasted bytes descending):

```
hash:  a3b2c1d4...
size:  2,048,000 bytes  (count=3, wasted=4,096,000 B)
  /Users/me/Downloads/copy1.zip
  /Users/me/Downloads/copy2.zip
  /Users/me/Archive/old/backup.zip

hash:  ef91...
size:  524,288 bytes  (count=2, wasted=524,288 B)
  /Users/me/Pictures/IMG_001.jpg
  /Users/me/Pictures/duplicates/IMG_001.jpg

2 duplicate group(s), 1,234 files considered, 4,620,288 B wasted
```

## MCP

```json
{
  "name": "find_duplicates",
  "arguments": {
    "expr": "is_image",
    "dir": "/Users/me/Pictures",
    "min_size": 100000
  }
}
```

Response shape:

```json
{
  "total_files": 1234,
  "duplicate_groups": 2,
  "wasted_bytes": 4620288,
  "duplicates": [
    {
      "hash": "a3b2c1d4...",
      "size": 2048000,
      "count": 3,
      "wasted_bytes": 4096000,
      "paths": ["/Users/me/Downloads/copy1.zip", ...]
    }
  ],
  "elapsed_seconds": 0.823
}
```

Inspect `cancelled` / `cancellation_reason` for partial results on timeout — same semantics as `search` and `stats`.

## Performance & caching

Cost depends on cache state and tree shape:

- **Cold cache, mostly-unique tree** (most files have unique sizes): cheap — only the size-collision groups get hashed.
- **Cold cache, lots of same-sized files** (e.g. millions of photos, similar sizes): expensive — every candidate hashed once.
- **Warm cache** (second run): free for unchanged files. The cache is validated by `(size, mtime)`, so an edited file naturally re-hashes.

Recommended for large trees:

1. First run: pair with `--timeout 5m` and `--index-path /var/cache/fso/...` so the cache survives. Accept a partial result if the timeout fires.
2. Subsequent runs: free, can be cron-scheduled (e.g. nightly disk-usage reports).

The `min_size` flag is the cheapest way to scope: 4 KiB threshold skips a lot of tiny config and dotfile duplicates that aren't worth reclaiming.

## Limitations

- **No content-aware matching.** "Two images that look the same but differ by 1 EXIF byte" are NOT detected as duplicates — sha256 is byte-exact. For perceptual image similarity you'd need a different tool (ImageMagick `compare`, perceptual-hash libs, etc.).
- **No partial-file dedup.** A 100 GB file and a slightly trimmed copy aren't grouped. Same reason — content addressing is whole-file.
- **No suggested action.** `find_duplicates` reports; it doesn't delete. Pair with the CLI output + `xargs rm` (carefully!) or use a dedicated cleanup tool.
