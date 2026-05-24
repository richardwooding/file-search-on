# Recipes — Browser bookmarks

Every Chromium-family browser (Chrome, Brave, Edge, Chromium, Opera, Vivaldi, Arc) writes its bookmarks to a single JSON file named `Bookmarks` (no extension) per profile under `~/Library/Application Support/<Vendor>/<Profile>/Bookmarks`. Safari uses `~/Library/Safari/Bookmarks.plist` (binary plist). Both detect as `browser/bookmarks-chromium` / `browser/bookmarks-safari` and fire `is_bookmark_file`.

The parser walks the recursive bookmark tree, collecting URL / title / folder names. With `--body`, the `body` CEL variable carries one `title<TAB>url` line per bookmark (plus folder names on their own lines) so `body.contains(...)` / `body.matches(...)` work the same way they do for markdown / email / SQLite FTS.

Out of scope for v1: Firefox `bookmarkbackups/*.jsonlz4` (LZ4-compressed; Firefox keeps the live store in `places.sqlite`, which already detects via the SQLite path and FTS body extraction).

## "Did I save anything about X?"

```sh
# Cross-browser body search — every saved page mentioning kubernetes
file-search-on 'is_bookmark_file && body.contains("kubernetes")' --body -d ~/Library

# Case-insensitive regex over titles AND URLs
file-search-on 'is_bookmark_file && body.matches("(?i)transformer")' --body -d ~/Library

# Github project URLs (regex over the URLs list)
file-search-on 'is_bookmark_file && bookmark_urls.exists(u, u.matches("github.com/[^/]+/[^/]+"))' -d ~/Library
```

## Inventory queries

```sh
# Per-browser bookmark count + path
file-search-on 'is_bookmark_file' -d ~/Library -o json | \
  jq -r '"\(.browser_vendor)\t\(.bookmark_count)\t\(.path)"'

# Most-bookmark-heavy Chromium profile
file-search-on 'is_chromium_bookmarks' -d ~/Library --sort bookmark_count --order desc --limit 5

# Vendor-specific filter
file-search-on 'is_bookmark_file && browser_vendor == "brave"' -d ~/Library

# Find profiles with a specific folder
file-search-on 'is_bookmark_file && "Reading List" in bookmark_folders' --body -d ~/Library
```

## Cross-family composition

`is_bookmark_file` composes with every other CEL filter — time bucketing, hashes, sort + top-K, body-search vocabulary.

```sh
# Recently-modified bookmark files (e.g. activity audit)
file-search-on 'is_bookmark_file && mod_time > timestamp("2026-04-01T00:00:00Z")' -d ~/Library

# Same domain across browsers
file-search-on 'is_bookmark_file && bookmark_urls.exists(u, u.startsWith("https://news.ycombinator.com"))' -d ~/Library

# Largest bookmark files on disk (sign of heavy users)
file-search-on 'is_bookmark_file' -d ~/Library --sort size --order desc --limit 10
```

## Known limitations

- **Firefox is out of scope** for v1. Firefox keeps the live bookmark store in `places.sqlite`, which already detects as `database/sqlite` with FTS body extraction; backups in `bookmarkbackups/*.jsonlz4` need LZ4 decompression that hasn't landed as a general capability yet.
- **Title / URL list caps** — at 1000 each. Power users with mega-tagged trees may see truncation; the `bookmark_count` and `bookmark_folder_count` are uncapped.
- **Folder names cap** — at 100 unique entries.
- **Depth cap** — 64 levels. Defends against adversarial nesting and won't truncate any real-world bookmark tree (10-15 levels is heavy).
- **Body lines** — pair `title\turl` by index; when titles and URLs aren't index-aligned (rare; the addURL collector keeps them roughly aligned per the walker), some lines may show one without the other.
- **`browser_vendor` is path-based**. A bookmark file copied out of its canonical browser directory won't get the vendor label even if its contents are unmistakably Chromium.
