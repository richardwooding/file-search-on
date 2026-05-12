# file-search-on

**Content-type aware file search with CEL-powered attribute filtering.**

`file-search-on` walks a directory tree and returns files matching a [CEL](https://github.com/google/cel-spec) expression evaluated over each file's metadata and content-type-specific attributes. Instead of grepping by name, ask things like:

```sh
file-search-on 'is_pdf && page_count > 10 && author == "Jane Doe"'
file-search-on 'is_image && gps_lat > 51.4 && gps_lat < 51.6'        # photos near home
file-search-on 'is_audio && artist == "Radiohead" && year < 2000'
file-search-on 'is_video && video_height >= 2160 && video_codec == "h265"'
file-search-on 'is_office && language == "fr"'
file-search-on 'is_markdown && "longread" in tags && word_count > 1000'

# Or match fuzzily — typos in the data are no longer fatal:
file-search-on 'is_audio && levenshtein(artist, "Radiohead") <= 2'                # catches "Radiohad", "Radiohea"
file-search-on 'is_image && soundex(camera_make) == soundex("Nikon")'             # phonetic match across capitalisation / spelling
file-search-on 'is_markdown && ngram_similarity(title, "kubernetes", 2) > 0.6'    # substring-tolerant title match
```

Across **73 file formats** organised into thirteen content-type families (documents, data, images, audio, video, office, ebooks, plain text, archives, compiled binaries, email, source code, notebooks), with format-specific metadata extraction.

## Features

- **Pluggable content-type detection** — extension-first with magic-byte fallback. New formats are a single registration call.
- **Twelve content-type families**, each with its own metadata extractors:

  | Family | Formats | Bundle of attributes |
  | --- | --- | --- |
  | **Documents** | PDF, EPUB | title, author, language, page_count |
  | **Markup** | Markdown, HTML, XML | title, word_count, frontmatter, language, root_element |
  | **Data** | JSON, YAML, CSV, TSV | json_kind, yaml_kind, yaml_document_count, column_count, csv_columns |
  | **Plain text** | TXT, log, … | line_count, word_count |
  | **Images** | JPEG, PNG, GIF, WebP, TIFF, BMP, SVG, HEIC | dimensions + EXIF: camera, lens, GPS, ISO, focal_length, taken_at |
  | **Audio** | MP3, M4A, FLAC, OGG | tags (artist, album, genre, year, …) + duration, bitrate / nominal_bitrate, sample_rate, channels, bit_depth, ReplayGain |
  | **Video** | MP4, MOV, MKV, WebM, AVI | duration, bitrate / nominal_bitrate, video_codec, audio_codec, video_width/height, frame_rate, rotation, HDR / colour-space, subtitles |
  | **Office** | DOCX, XLSX, PPTX, ODT | title, author, language (Dublin Core) |
  | **Archives** | ZIP (incl. JAR / WAR / EAR), TAR, TAR.GZ, GZIP | entry_count, uncompressed_size, top_level_entries, has_root_dir |
  | **Binaries** | ELF (Linux/BSD), Mach-O (macOS, incl. universal), PE (Windows) | architectures, bitness, binary_format, binary_type, is_dynamically_linked, is_stripped, entry_point |
  | **Email** | RFC 5322 (`.eml`), Unix mbox (`.mbox`) | title (subject), author (from), email_to, email_cc, sent_at, attachment_count, email_count |
  | **Source code** | Go, Python, JS/TS, Rust, C/C++, Java, Ruby, Swift, Kotlin, Scala, Shell, Lua, Elixir, Clojure, Haskell, OCaml, Zig | language, line_count, loc, comment_loc, blank_loc |
  | **Notebooks** | Jupyter `.ipynb`, Apache Zeppelin `.zpln` | cell_count, code_cell_count, markdown_cell_count, kernel, language, title |

  Type predicates (`is_pdf`, `is_image`, `is_audio`, `is_video`, `is_office`, `is_epub`, …) light up automatically from the registered content type. See [examples/](./examples/) for recipes by family.

- **First-class Markdown front-matter** — YAML (`---`), TOML (`+++`), and JSON (`{ ... }`) are recognised by leading bytes. Common keys (`title`, `author`, `language`, `tags`, `categories`, `draft`, `date`) become top-level CEL variables; everything else lives in a generic `frontmatter` map. See [examples/markdown.md](./examples/markdown.md).
- **CEL expressions** — the full Common Expression Language: comparisons, `&&`/`||`, string functions, list membership, timestamp arithmetic. Composes naturally with structural attributes.
- **Fuzzy and phonetic matching out of the box** — built-in `levenshtein` (edit distance), `soundex` (NARA-standard phonetic codes), `ngrams` and `ngram_similarity` (Jaccard over character n-grams) let you write typo-tolerant and "sounds-like" queries against any string attribute. EXIF camera make in `Nikkon` instead of `Nikon`? Artist tag mistyped as `Radiohad`? Markdown front-matter author spelled `Smyth` versus `Smith`? Same query catches all of them. See the [`fuzzy-search.md`](./examples/fuzzy-search.md) recipe page for the full set.
- **Multiple output formats** — `bare` (paths only), `default`, `verbose` (multi-line), `json` (NDJSON), or a Go `text/template` via `--format`.
- **MCP server mode** — same binary doubles as a [Model Context Protocol](https://modelcontextprotocol.io) server (stdio, HTTP, or SSE). LLM agents can invoke `search` and `list_attributes` tools directly.
- **Pure Go, no CGO** — cross-compiles cleanly to all six release targets. No image/audio/video decoder dependencies.
- **Parallel walking** — files are evaluated across a worker pool (defaults to `NumCPU`).

## Install

### Homebrew (macOS / Linux)

```sh
brew install richardwooding/tap/file-search-on
```

The cask is published from this repo on every tagged release to [`richardwooding/homebrew-tap`](https://github.com/richardwooding/homebrew-tap).

> **macOS Gatekeeper:** the binary isn't yet signed with an Apple Developer ID, so macOS may block it on first run. The cask's post-install hook strips the quarantine xattr automatically. If macOS still blocks it:
>
> ```sh
> sudo xattr -dr com.apple.quarantine $(brew --prefix)/bin/file-search-on
> ```

### Container (Docker / Podman)

OCI images are published to GitHub Container Registry on every tag, with `linux/amd64` and `linux/arm64` manifests:

```sh
docker run --rm -v "$PWD:/work" ghcr.io/richardwooding/file-search-on:latest \
  'is_markdown && draft' -d /work
```

Pin to a specific version with `:vX.Y.Z`. The base image is [`cgr.dev/chainguard/static`](https://images.chainguard.dev/directory/image/static), so the container has the binary and nothing else (no shell).

### Pre-built binaries

Pre-built archives for Linux, macOS, and Windows on `amd64` and `arm64` are attached to every [GitHub Release](https://github.com/richardwooding/file-search-on/releases), along with a `checksums.txt` you should verify.

### From source

Requires Go 1.26.2 or newer.

```sh
go install github.com/richardwooding/file-search-on/cmd/file-search-on@latest
```

Or build from a clone:

```sh
git clone https://github.com/richardwooding/file-search-on.git
cd file-search-on
go build -o file-search-on ./cmd/file-search-on
```

## Usage

```sh
file-search-on [EXPR] [-d DIR] [-w WORKERS]
file-search-on --list
```

| Flag | Description | Default |
| --- | --- | --- |
| `EXPR` | CEL expression to match files against. | `true` (matches everything) |
| `-d`, `--dir` | Directory to search. Repeatable — pass `-d ./docs -d ./posts` to walk multiple roots in one call. | `.` |
| `-w`, `--workers` | Number of parallel workers. | number of CPU cores |
| `-l`, `--list` | List supported attributes and registered content types. | |
| `-L`, `--max-line-bytes` | Per-line scanner cap for text/CSV/HTML in bytes. Raise for very long log lines. | 1 MiB |
| `-o`, `--output` | Output preset: `bare` (paths only), `default`, `verbose` (multi-line records), `json` (NDJSON). | `default` |
| `--format` | Custom Go `text/template` per match (e.g. `'{{.Path}}\t{{.Title}}'`). Takes precedence over `-o`. | |
| `--index-path` | Persistent attribute index file (bbolt). When set, unchanged files (matched by absolute path + size + mtime) skip the per-file content-type parse, making repeat searches dramatically faster. | unset (no caching) |
| `--timeout` | Maximum walk duration (Go duration string: `30s`, `2m`, `500ms`). On expiry, results collected so far are still printed and the process exits 124. Ctrl-C exits 130 with whatever was collected. | unset (no timeout) |
| `--sort` | Sort matches by attribute (e.g. `size`, `taken_at`, `duration`, `word_count`, …). Files missing the attribute group at the end. Forces buffered mode. | unset (path-sorted default for buffered modes; walk order for streaming) |
| `--order` | Sort direction: `asc` or `desc`. Ignored without `--sort`. | `asc` |
| `--limit` | Cap the result set at N matches. With `--sort`, returns the top-N. Without `--sort`, returns the first N in walk order. | `0` (unlimited) |
| `--snippet` | Include a snippet of each match's body in verbose/json/template output (first N lines, see `--snippet-lines`). Only text-based content types populate. | off |
| `--snippet-lines` | Snippet length when `--snippet` is set. | `10` |
| `--exclude` | Glob pattern matched against the basename of each file/directory; matches are skipped (dirs are pruned). Repeatable. | unset |
| `--respect-gitignore` | Parse a `.gitignore` at the walk root and skip matching paths. Honours standard gitignore semantics (including `**` and negation). Nested `.gitignore` files in subdirectories are NOT honoured in this version. | off |
| `--body` | Read each candidate file's full body (text content types only) and expose it to the CEL expression as the `body` string variable. Pair with CEL's built-in `contains` / `matches` / `startsWith` methods for content filtering. Expensive — reads every candidate, not just headers. | off |
| `--body-max-bytes` | Cap on the body string in bytes. Files larger than the cap are silently truncated; the prefix still participates in the filter. | `0` (1 MiB) |

Each matching file is printed as `<path>\t[<content-type>]\t<size> bytes`. The match count is written to stderr so it doesn't pollute pipelines.

### Output presets

```sh
file-search-on 'is_markdown' -d ./docs -o bare       # one path per line; pipe-friendly
file-search-on 'is_markdown' -d ./docs               # default (back-compat)
file-search-on 'is_markdown' -d ./docs -o verbose    # multi-line, all attributes
file-search-on 'is_markdown' -d ./docs -o json       # NDJSON, one object per line
file-search-on 'is_markdown' -d ./docs --format '{{.Path}}\t{{.Title}}\t{{.WordCount}}'
```

`-o bare`, `-o json`, and `--format` suppress the `<N> file(s) found` summary on stderr (the count is implicit in the line count). `--format` uses Go [`text/template`](https://pkg.go.dev/text/template); the data context is a flat record — `{{.Path}}`, `{{.Title}}`, `{{.WordCount}}`, `{{.Frontmatter}}`, all the `Is*` booleans, etc. Backslash escapes (`\t`, `\n`) are expanded before parsing.

### Persistent attribute index

The first time `file-search-on` walks a directory, it parses every file (PDF metadata, EXIF, audio tags, markdown front-matter, …). For repeated searches against an unchanged tree, that work is wasted: the CEL expression changes, but the underlying attributes do not. Pass `--index-path` to cache the parse result in a single bbolt file:

```sh
file-search-on 'is_markdown && word_count > 500' -d ./docs --index-path ~/.cache/fso/docs.db   # cold: parses + stores
file-search-on 'is_pdf && page_count > 10'        -d ./docs --index-path ~/.cache/fso/docs.db   # warm: hits the cache, dramatically faster
```

How it works:

- **Validation.** Each entry is keyed by absolute path and validated against the file's `(size, mtime)` pair. Modify a file (`touch`, edit, replace) and that entry is invalidated; the rest stay warm.
- **Expression-independent.** The cache stores the per-file attributes, not the search results — so any CEL expression you run benefits, including `--list` lookups via `read_attributes`.
- **Footer line.** When `--index-path` is set, the CLI prints a stderr line like `index: 1234 hits, 56 misses, 56 stored, 0 stale, 0 errors` after the search.
- **Schema versioning.** A new binary refuses to open an index file from an incompatible schema version and tells you to delete it. We never auto-delete user data; cache files are recoverable but not disposable from the tool's perspective.
- **GC is lazy.** Entries for deleted/moved files are simply never read; they cost a small amount of disk space until the file is recreated. There is no built-in vacuum step.

In MCP server mode the cache is **on by default and lives for the process lifetime** (no flag needed). Pass `--index-path` to make the cache persist across restarts. See [MCP server mode](#mcp-server-mode).

### Top-K queries, snippets, and excludes

Three controls let agents (and shells) shape the result set without post-processing:

**Sort + limit** for top-K queries — "the 5 longest videos", "10 most recent photos", "biggest archives":

```sh
file-search-on 'is_video' --sort duration --order desc --limit 5 -d ~/Movies
file-search-on 'is_image' --sort taken_at --order desc --limit 10 -d ~/Pictures
file-search-on 'is_archive' --sort uncompressed_size --order desc --limit 3 -d ~/Downloads
```

Recognised `--sort` keys: `size`, `name`, `path`, `mod_time`, `word_count`, `line_count`, `page_count`, `duration`, `bitrate`, `sample_rate`, `video_height`, `video_width`, `frame_rate`, `iso`, `focal_length`, `taken_at`, `sent_at`, `year`, `entry_count`, `uncompressed_size`, `loc`, `attachment_count`, `email_count`. Files missing the attribute (e.g. sorting by `duration` on a markdown file) group at the end. `--sort` forces buffered mode — top-K is incoherent with streaming. `--limit` without `--sort` returns the first N in walk order.

**Snippets** for "what is this file about?" — include the first N lines of the body alongside the metadata:

```sh
file-search-on 'is_markdown && word_count > 500' --snippet --snippet-lines 5 -o verbose -d ~/notes
file-search-on 'is_source && language == "go"' --snippet -o json -d . | jq -r '.path + "\n" + .snippet'
```

Snippets populate for text-based content types (markdown / text / html / csv / json / xml / source code in 18 languages) and stay empty for binary families.

**Excludes** prune the walk before it visits a tree (much faster than filtering after the fact):

```sh
file-search-on 'is_source' -d . --exclude node_modules --exclude .git --exclude target
file-search-on 'true' -d . --respect-gitignore
```

`--exclude` matches against the basename of each file/dir (`filepath.Match` semantics) — `--exclude node_modules` prunes any directory named `node_modules` anywhere in the tree, and `--exclude '*.bak'` skips backup files. `--respect-gitignore` parses a `.gitignore` at the walk root and honours its patterns (including `**` and negation); only the root file is consulted (no nested `.gitignore` traversal in this version).

### Body-content filters

Most filters are metadata-only (size, content type, frontmatter, EXIF, …). For "find files mentioning X" queries, pass `--body` so the CEL expression sees the file contents as a `body` string variable, and combine with CEL's built-in string methods:

```sh
# Substring search (cheap)
file-search-on 'is_markdown && body.contains("transformer")' -d ~/notes --body

# Regex search via CEL's matches operator (RE2 syntax — same as Go's regexp/re2)
file-search-on 'is_source && body.matches("(?i)\\bTODO\\b")' -d ./src --body

# Combine with sort/limit for "5 biggest source files containing 'panic'"
file-search-on 'is_source && body.contains("panic")' -d ./src --body --sort size --order desc --limit 5
```

CEL provides `contains`, `matches`, `startsWith`, `endsWith`, and `size()` as built-in string methods — no custom CEL functions needed. `matches` uses Google's [RE2](https://github.com/google/re2/wiki/Syntax), the same regex engine Go's standard `regexp` package uses.

Caveats:
- Only text-based content types populate `body` (markdown / text / html / csv / json / xml / source code). Binary families (PDF / image / audio / video / archive / binary / office / epub / email) get empty bodies — `body.contains(...)` on them is always false.
- `--body` reads every candidate file, not just headers. Pair with a tight type predicate (`is_markdown && body.contains(...)`) so the cheap predicate prunes most files before the expensive read.
- Bodies are capped at `--body-max-bytes` (default 1 MiB). Files larger than the cap have the prefix participating in the filter; the rest is invisible.

See [examples/body-search.md](./examples/body-search.md) for more recipes.

### Stats — histogram by any attribute

The `stats` subcommand walks a tree and aggregates a histogram with totals — quick reconnaissance without retrieving every path. The default bucket is content type; pass `--group-by` to bucket by any other recognised attribute.

```sh
# What's in this Downloads folder? (default: by content_type)
file-search-on stats -d ~/Downloads

# Markdown-only stats with a CEL filter
file-search-on stats 'is_markdown && word_count > 500' -d ~/notes

# Bucket photos by camera_make
file-search-on stats 'is_image' --group-by camera_make -d ~/Pictures

# Count source files per language
file-search-on stats 'is_source' --group-by language -d ./src

# Aggregate across two roots
file-search-on stats -d ~/Documents -d ~/Downloads
```

Recognised `--group-by` keys:

- **String attributes:** `content_type` (default), `ext`, `dir`, `language`, `camera_make`, `camera_model`, `lens`, `artist`, `album`, `genre`, `kernel`, `binary_format`, `binary_type`, `frontmatter_format`.
- **Time bucketing:** `mtime_year` / `mtime_month` / `mtime_day` (file mtime); `taken_at_year` / `_month` / `_day` (image EXIF); `sent_at_year` / `_month` / `_day` (email); `date_year` / `_month` / `_day` (markdown front-matter). Files with zero timestamps bucket as `"(no date)"`.

Unknown values fall back to `content_type`.

```sh
# "How many photos per year did I take?"
file-search-on stats 'is_image' --group-by taken_at_year -d ~/Pictures

# "What did I edit last month?" (mtime in YYYY-MM)
file-search-on stats --group-by mtime_month -d ~/Documents
```

Output (table mode, default):

```
camera_make                    count      total_size
SONY                              42      245,000,000 B
Apple                             89      178,000,000 B
unknown                           17        4,000,000 B
---                              ---             ---
TOTAL                            148      427,000,000 B
```

`-o json` writes the same data as a structured object: `{total_count, total_size, group_by, groups[], content_types[], …}`. See [examples/stats.md](./examples/stats.md) for recipes; the MCP `stats` tool exposes the same shape.

### Find duplicate files

The `duplicates` subcommand (and matching MCP `find_duplicates` tool) reports groups of byte-identical files keyed by sha256. Useful for "what's eating my disk?" reconnaissance.

```sh
# Whole tree
file-search-on duplicates -d ~/Downloads

# Photos only — pair with a CEL filter to scope the candidates
file-search-on duplicates 'is_image' -d ~/Pictures

# Skip tiny duplicates that aren't worth reclaiming
file-search-on duplicates -d . --min-size 4096

# JSON output for piping into jq
file-search-on duplicates -d ~/Music -o json | jq '.duplicates[0]'
```

Two-pass for performance: files with unique sizes are skipped (they can't be duplicates). Only files in size-collision groups get hashed. With `--index-path`, hashes are cached alongside the rest of the attribute entry — first runs on large trees can be slow, but **subsequent calls on unchanged files are free**.

Output (table mode, sorted by wasted bytes descending):

```
hash:  a3b2c1...
size:  2,048 bytes  (count=3, wasted=4,096 B)
  /Users/me/dl/copy1.pdf
  /Users/me/dl/copy2.pdf
  /Users/me/Pictures/random-name.pdf

2 duplicate group(s), 1,234 files considered, 4,096 B wasted
```

See [examples/duplicates.md](./examples/duplicates.md) for recipes.

### Read a range of lines

The `lines` subcommand prints a specific line range from a single file — useful as a follow-up to `search`:

```sh
file-search-on lines main.go --start 1 --end 50          # first 50 lines
file-search-on lines log.txt --start 1000 --end 1050     # an arbitrary window
file-search-on lines big.csv --start 1 --max-lines 20    # first 20, capped
file-search-on lines main.go --start 1 -o json           # machine-readable
```

The matching MCP `read_lines` tool returns `{path, start_line, end_line, total_lines, lines[], truncated}` — pair with `search` to fetch context around each match without leaving the MCP server.

### Timeouts and partial results

Walks over large trees can take a while. Both the CLI and the MCP server expose timeouts so callers don't wait forever, and both surface **partial results** when a deadline fires — whatever was collected before cancellation is still returned, just with a clear "this was incomplete" signal.

**CLI** — `--timeout` accepts any Go duration:

```sh
file-search-on 'is_pdf' -d ~/Documents --timeout 30s
file-search-on 'is_video && duration > 1800' -d ~/Movies --timeout 2m -o json
```

When the deadline fires:

- Whatever results were already collected are printed to stdout (sorted, in the requested format).
- The footer `<N> file(s) found` reflects the partial count.
- A warning is written to stderr: `search timed out after 30s; results above may be incomplete`.
- The process exits with code **124** (matches GNU `timeout(1)` convention). Ctrl-C exits **130** (`128 + SIGINT`). Successful completion exits 0; hard errors exit 1.

Default is **no timeout** — back-compatible with how the CLI behaved before this flag existed.

**MCP** — every tool call is bounded by a server-default timeout (typically **60 seconds**, configurable at startup via `--timeout`). The `search` tool also accepts `timeout_seconds` on input to override per-call:

```json
{ "expr": "is_image && iso > 1600", "dir": "~/Pictures", "timeout_seconds": 10 }
```

`timeout_seconds: 0` disables the timeout for that specific call. On expiry, the search tool **does not return an error** — it returns the partial match set with three new fields:

| Field | Type | Meaning |
| --- | --- | --- |
| `cancelled` | bool | True if the walk did not complete. |
| `cancellation_reason` | string | `"timeout"` (our deadline fired) or `"client_cancel"` (transport closed, parent context cancelled, etc.). |
| `elapsed_seconds` | float | Wall-clock time spent in the search handler. |

Always inspect `cancelled` before treating the result as exhaustive. `read_attributes` is bounded by the same server default but returns an error on cancellation (single-file extraction has no partial-result semantics).

The reasoning: an MCP client (Claude Desktop, Claude Code, etc.) has its own read deadline; if the server walks for minutes the client gives up and the agent loses both the data and the chance to retry. Returning a partial set with a clear `cancelled` flag lets the agent see what was found, decide whether it's enough, and refine if not.

### Single-file inspection

When you already have a path and just want every attribute the parser produces, use the `attrs` subcommand. It skips the walker and the CEL filter — straight to `BuildAttributes` on one file:

```sh
file-search-on attrs ~/Pictures/photo.jpg
file-search-on attrs ~/Music/track.mp3 -o json | jq '.bitrate'
file-search-on attrs ~/Documents/report.pdf --format '{{.Title}} ({{.PageCount}} pages)'
```

`-o verbose` is the default for `attrs` — it dumps every populated attribute (camera / EXIF for photos, ID3v2 / playback for audio, codec / dimensions / framerate for video, Dublin Core for office docs, frontmatter for markdown). `-o json` and `--format` use the same record schema as `search`. The MCP equivalent is the `read_attributes` tool — same shape, same coverage.

## Recipes

Focused recipe collections live under [`examples/`](./examples/):

| Recipe file | What's in it |
| --- | --- |
| [`examples/markdown.md`](./examples/markdown.md) | Front-matter (YAML / TOML / JSON), draft flags, tag membership, custom keys |
| [`examples/images.md`](./examples/images.md) | EXIF camera/lens, GPS bounding boxes, ISO / aperture / focal length, taken-at ranges |
| [`examples/audio.md`](./examples/audio.md) | Artist / album / genre / year, bitrate, sample rate, hi-res filtering |
| [`examples/video.md`](./examples/video.md) | Codec, resolution, frame rate, duration, MKV vs MP4 |
| [`examples/office.md`](./examples/office.md) | DOCX / XLSX / PPTX / ODT — title, author, language |
| [`examples/epub.md`](./examples/epub.md) | EPUB books — title, author, language; XMP fallback |
| [`examples/data.md`](./examples/data.md) | JSON arrays vs objects, CSV column membership, XML root elements |
| [`examples/text.md`](./examples/text.md) | Plain text / log files — line count, word count, big-line caps |
| [`examples/notebooks.md`](./examples/notebooks.md) | Jupyter (`.ipynb`) and Apache Zeppelin (`.zpln`) — `cell_count`, `code_cell_count`, `kernel`, `language` |
| [`examples/cookbook.md`](./examples/cookbook.md) | Cross-cutting recipes — dedupe, mixed media filters, pipeline integration |
| [`examples/fuzzy-search.md`](./examples/fuzzy-search.md) | Fuzzy / phonetic / n-gram similarity matching — `levenshtein`, `soundex`, `ngrams`, `ngram_similarity` |
| [`examples/indexing.md`](./examples/indexing.md) | Persistent attribute index (`--index-path`) — cold/warm CLI runs, MCP auto-on cache, refresh + inspection |
| [`examples/timeouts.md`](./examples/timeouts.md) | Timeouts and partial results — CLI `--timeout`, MCP `timeout_seconds`, exit codes, cancellation semantics |
| [`examples/top-k.md`](./examples/top-k.md) | Top-K queries — `--sort` + `--limit` for "biggest 5 videos", "10 most recent photos", etc. |
| [`examples/snippets.md`](./examples/snippets.md) | Body previews — `--snippet` returns the first N lines of text files alongside metadata |
| [`examples/exclude.md`](./examples/exclude.md) | Pruning the walk — `--exclude` basename globs and `--respect-gitignore` |
| [`examples/body-search.md`](./examples/body-search.md) | Content filters — `--body` exposes file body to CEL; pair with `contains` / `matches` (RE2) / `startsWith` |
| [`examples/stats.md`](./examples/stats.md) | Directory reconnaissance — `file-search-on stats` aggregates a content-type histogram with totals |
| [`examples/group-by.md`](./examples/group-by.md) | Stats bucketed by any attribute — `--group-by camera_make`, `--group-by language`, `--group-by taken_at_year`, etc. |
| [`examples/read-lines.md`](./examples/read-lines.md) | Print a specific line range from a file — pairs with `search` to fetch match context |
| [`examples/duplicates.md`](./examples/duplicates.md) | Find byte-identical files by sha256 — `file-search-on duplicates [--min-size N]` |

A handful of representative one-liners:

```sh
# All Markdown files larger than 500 words
file-search-on 'is_markdown && word_count > 500' -d ./docs

# 4K HEVC videos longer than 30 minutes
file-search-on 'is_video && video_height >= 2160 && video_codec == "h265" && duration > 1800' -d ~/Videos

# Photos taken in 2024 with a Sony camera at high ISO
file-search-on 'is_image && camera_make == "SONY" && iso > 1600 && taken_at > timestamp("2024-01-01T00:00:00Z")' -d ~/Pictures

# CSVs with a "revenue" column
file-search-on 'is_csv && csv_columns.exists(c, c == "revenue")' -d ./reports

# French-language office documents
file-search-on 'is_office && language == "fr"' -d ~/Documents

# Audio tracks ≥ 96 kHz (hi-res)
file-search-on 'is_audio && sample_rate >= 96000' -d ~/Music

# Fuzzy: artist tag within 2 edits of "Radiohead" (catches typos)
file-search-on 'is_audio && levenshtein(artist, "Radiohead") <= 2' -d ~/Music

# Phonetic: any author whose name sounds like "Smith"
file-search-on 'is_markdown && soundex(author) == soundex("Smith")' -d ./posts
```

Combine paths and types — find HTML files inside a `build/` directory:

```sh
file-search-on 'is_html && dir.contains("build")'
```

## Available attributes

Run `file-search-on --list` for the canonical, up-to-date listing. The summary tables below are organised by attribute family.

### Common (always present)

| Attribute | Type | Description |
| --- | --- | --- |
| `name`, `path`, `dir` | string | Path components |
| `size` | int | File size in bytes |
| `ext` | string | File extension (e.g. `.md`) |
| `content_type` | string | Detected content type |
| `is_markdown`, `is_json`, `is_yaml`, `is_xml`, `is_html`, `is_pdf`, `is_image`, `is_text`, `is_csv`, `is_epub`, `is_office`, `is_audio`, `is_video`, `is_archive`, `is_binary`, `is_email`, `is_source`, `is_notebook` | bool | Type predicates |
| `is_dockerfile`, `is_makefile`, `is_justfile`, `is_rakefile`, `is_license`, `is_changelog`, `is_contributing`, `is_codeowners`, `is_gitignore`, `is_dockerignore`, `is_gomod`, `is_node_manifest`, `is_cargo_manifest`, `is_pipfile`, `is_python_reqs`, `is_gemfile`, `is_procfile`, `is_vagrantfile` | bool | Per-type predicates for exact-name files (Dockerfile, Makefile, LICENSE, .gitignore, go.mod, package.json, etc.). Family predicates `is_build`, `is_repo_meta`, `is_ignore`, `is_manifest`, `is_platform` fire alongside (e.g. `is_dockerfile` and `is_build` are both true for a Dockerfile). |
| `is_build`, `is_repo_meta`, `is_ignore`, `is_manifest`, `is_platform` | bool | Family predicates derived from `content_type` prefix (`build/*`, `repo/*`, `ignore/*`, `manifest/*`, `platform/*`) — mirrors the existing `is_image` / `is_audio` / `is_archive` family pattern |

### Document / markup

| Attribute | Type | Source |
| --- | --- | --- |
| `title` | string | Markdown front-matter / H1; HTML `<title>`; PDF `/Info`; EPUB `<dc:title>`; office `<dc:title>`; audio tags |
| `body` | string | Full file body for text content types (markdown / text / html / csv / json / xml / source/*). Populated only when `--body` (CLI) or `include_body` (MCP) is set; capped at `--body-max-bytes` (default 1 MiB). Combine with CEL string methods: `body.contains("X")`, `body.matches("...")` (RE2 regex), `body.startsWith("...")`. |
| `author` | string | Markdown front-matter, PDF `/Info`, EPUB / office `<dc:creator>` |
| `language` | string | EPUB / office `<dc:language>`; HTML `<html lang>`; markdown front-matter; PDF `/Lang` (XMP fallback) |
| `word_count` | int | Markdown body, plain text |
| `line_count` | int | Plain text |
| `page_count` | int | PDF |

### Data / structural

| Attribute | Type | Source |
| --- | --- | --- |
| `column_count`, `csv_columns` | int / `list<string>` | CSV/TSV header row |
| `json_kind` | string | JSON top-level: `"object"` or `"array"` |
| `yaml_kind` | string | YAML root node kind: `"object"` (mapping), `"array"` (sequence), or `"scalar"` |
| `yaml_document_count` | int | Number of `---`-separated YAML documents (1 for single-doc; >1 for K8s manifest bundles) |
| `root_element` | string | XML |

### Repo files (exact-name)

| Attribute | Type | Source |
| --- | --- | --- |
| `module` | string | Go module path declared in `go.mod`'s `module` directive |
| `go_version` | string | Go toolchain version from `go.mod`'s `go` directive (e.g. `"1.26.2"`) |
| `base_image` | string | First `FROM <image>` directive in a `Dockerfile` / `Containerfile` |

### Markdown front-matter (promoted)

| Attribute | Type | Notes |
| --- | --- | --- |
| `tags`, `categories` | `list<string>` | Bare strings are wrapped to single-element lists |
| `draft` | bool | Defaults to `false` when missing |
| `date` | timestamp | Native TOML dates + RFC3339 / `YYYY-MM-DD` strings |
| `frontmatter` | `map<string, dyn>` | Full parsed map — reach any custom key with `frontmatter.foo` |
| `frontmatter_format` | string | `"yaml"`, `"toml"`, `"json"`, or `""` |

### Images (EXIF)

| Attribute | Type | Source |
| --- | --- | --- |
| `img_width`, `img_height` | int | Pixel dimensions |
| `camera_make`, `camera_model`, `lens` | string | EXIF |
| `taken_at` | timestamp | EXIF DateTimeOriginal → CreateDate → ModifyDate fallback |
| `orientation` | int | EXIF orientation (1-8) |
| `gps_lat`, `gps_lon` | double | Decimal degrees, north / east positive |
| `iso` | int | EXIF ISO |
| `focal_length`, `f_stop`, `exposure_time` | double | mm, F-number, seconds |

### Audio

| Attribute | Type | Source |
| --- | --- | --- |
| `artist`, `album`, `album_artist`, `composer`, `genre` | string | Audio tags (ID3v1/v2, MP4 atoms, Vorbis comments) |
| `year`, `track` | int | Release year, track number |
| `duration` | double | Seconds |
| `bitrate` | int | kbps — computed average (`file_size × 8 / duration / 1000`) |
| `nominal_bitrate` | int | kbps — codec/container-stored. MP3 first-frame bitrate; OGG `bitrate_nominal`; M4A esds (avgBitrate, maxBitrate fallback) |
| `sample_rate` | int | Hz |
| `channels` | int | 1 = mono, 2 = stereo, … |
| `bit_depth` | int | Bits per sample. FLAC STREAMINFO + MP4 `stsd`; 0 for MP3 / OGG (not stored) |
| `replaygain_track_gain`, `replaygain_album_gain` | double | dB — Vorbis comments (FLAC + OGG), ID3v2 TXXX (MP3), and M4A iTunes `----` atoms (`com.apple.iTunes` namespace, surfaced under the inner `name` atom's value) |

### Video

| Attribute | Type | Source |
| --- | --- | --- |
| `video_codec`, `audio_codec` | string | h264, h265, av1, vp9, aac, opus, ... |
| `video_width`, `video_height` | int | Frame pixels |
| `frame_rate` | double | fps |
| `rotation` | int | Degrees (0 / 90 / 180 / 270) decoded from MP4 `tkhd` display matrix; 0 for non-MP4 or non-axis-aligned matrices |
| `duration` | double | Seconds (shared with audio) |
| `bitrate` | int | kbps — computed average (shared with audio) |
| `nominal_bitrate` | int | kbps — codec/container-stored. MP4 `btrt` avgBitrate; MKV `Bitrate` (0x4FB1); AVI `avih.maxBytesPerSec` |
| `sample_rate`, `channels` | int | First audio track inside the video container (shared keys with standalone audio) |
| `is_hdr` | bool | True when transfer is PQ (HDR10 / Dolby Vision base) or HLG |
| `color_primaries` | string | `bt709`, `bt2020`, `p3`, or `""` |
| `color_transfer` | string | `bt709`, `pq`, `hlg`, or `""` |
| `subtitles` | bool | At least one subtitle / closed-caption track present |
| `subtitle_languages` | `list<string>` | ISO 639-2 codes per subtitle track in declaration order |

### Archives

| Attribute | Type | Source |
| --- | --- | --- |
| `entry_count` | int | Number of entries inside ZIP / TAR / TAR.GZ. Always 1 for standalone `.gz` (single stream per RFC 1952) |
| `uncompressed_size` | int | Sum of per-entry uncompressed sizes (ZIP / TAR / TAR.GZ). For standalone `.gz` reads the 4-byte ISIZE footer — note this is mod 2³², so > 4 GiB payloads report a wrapped value (matches `gzip -l`) |
| `top_level_entries` | `list<string>` | Root-level entry names, sorted and deduplicated |
| `has_root_dir` | bool | True when the archive has a single top-level entry (Unix tarball convention; useful for spotting ZIP-bombs when false) |

### Binaries

Compiled executables — ELF (Linux/BSD), Mach-O (macOS, including universal/fat), PE (Windows). All three formats parsed via Go stdlib (`debug/elf`, `debug/macho`, `debug/pe`); no CGo, no third-party libraries.

| Attribute | Type | Source |
| --- | --- | --- |
| `architectures` | `list<string>` | Canonical CPU arch names — `x86_64`, `arm64`, `i386`, `arm`, `ppc64`, `riscv64`, …. Length 1 for thin binaries; length ≥ 2 for fat (universal) Mach-O. |
| `bitness` | int | 32 or 64. For fat Mach-O reflects the first slice. |
| `binary_format` | string | `elf`, `mach-o`, or `pe` — the format subtype lifted to a CEL string. |
| `binary_type` | string | `executable`, `shared_library`, `object`, `core`, or `unknown` — cross-format normalisation. |
| `is_dynamically_linked` | bool | ELF: `PT_INTERP` or `PT_DYNAMIC` segment. Mach-O: any `LC_LOAD_DYLIB` load command. PE: non-empty import directory. |
| `is_stripped` | bool | ELF: `Symbols()` returns `ErrNoSymbols`. Mach-O: `Symtab` nil or empty. PE: `IMAGE_FILE_DEBUG_STRIPPED` characteristic OR empty COFF symbol table. |
| `entry_point` | int | Virtual-address entry point (ELF `Entry` / PE `AddressOfEntryPoint`). Zero for shared libraries, object files, and Mach-O (debug/macho doesn't expose `LC_MAIN.entryoff`). |

**Detection**: ELF (`\x7FELF`), Mach-O thin magics (4 variants), and PE (`MZ`) are all extension-or-magic. Mach-O fat magic `0xCAFEBABE` is **not** registered to avoid the Java `.class` collision; fat binaries are recognised via extension (`.dylib`) or by being parsed once Mach-O detection has fired some other way.

### Email

RFC 5322 messages (`.eml`, `.email`) and Unix mbox archives (`.mbox`). Both parsed via stdlib `net/mail` for headers + `mime/multipart` for attachment counting; no third-party libs. For `.mbox` archives, the per-message attributes (`title`, `author`, `email_to`, `sent_at`, …) reflect the **first** message — `email_count` carries the multi-message shape.

| Attribute | Type | Source |
| --- | --- | --- |
| `title` | string | RFC 5322 `Subject` (RFC 2047 encoded-words decoded; reused with markdown/PDF/EPUB/office/audio titles) |
| `author` | string | RFC 5322 `From` — display name preferred, falls back to address (reused with markdown/PDF/EPUB/office authors) |
| `email_to` | `list<string>` | `To` header — addresses only; display names dropped to keep list shape predictable for `"alice@example.com" in email_to` |
| `email_cc` | `list<string>` | `Cc` header — addresses only |
| `email_message_id` | string | `Message-ID` header (angle brackets stripped) |
| `email_in_reply_to` | string | `In-Reply-To` header (angle brackets stripped) |
| `sent_at` | timestamp | `Date` header parsed via `mail.ParseDate` |
| `attachment_count` | int | top-level multipart parts with `Content-Disposition: attachment` or a `filename` parameter. Zero for non-multipart messages. |
| `email_count` | int | mbox archives — number of messages (count of `^From ` separator lines). Always 1 for single `.eml` files. |

**Detection**: `.eml` is extension-only (RFC 5322 messages can begin with any header — no canonical magic). `.mbox` is extension + magic `From ` (5 bytes — F, r, o, m, space) at offset 0. Outlook `.msg` is out of scope (proprietary OLE binary). Maildir directories are handled implicitly — each file inside is `.eml`-shaped.

### Source code

Nineteen languages registered: Go, Python, JS, TS, Rust, C, C++, Java, Ruby, Swift, Kotlin, Scala, Shell, Lua, Elixir, Clojure, Haskell, OCaml, Zig. Detection is extension-only (no third-party language-detection lib). Line classification uses per-language line + block comment markers; mixed lines (code with trailing comment) count as code, lines that BEGIN with a comment marker count as comment — matches the cloc / tokei convention.

| Attribute | Type | Source |
| --- | --- | --- |
| `language` | string | canonical programming-language name (`go`, `python`, `javascript`, `typescript`, `rust`, `c`, `cpp`, `java`, `ruby`, `swift`, `kotlin`, `scala`, `shell`, `lua`, `elixir`, `clojure`, `haskell`, `ocaml`, `zig`) — reused with markup/EPUB/office locale codes; locale codes are 2-letter ISO 639-1 and don't overlap with these names |
| `line_count` | int | total physical lines (reused with text family) |
| `loc` | int | non-blank, non-comment lines |
| `comment_loc` | int | comment-only lines (line-comment marker at start, plus every line wholly inside a block comment) |
| `blank_loc` | int | empty or whitespace-only lines |

Caveats: string literals containing `//` or `/*` are treated as code (no string-aware parsing). Block comments that open mid-line are not tracked — the line is classified by what it BEGINS with.

### Notebooks

Computational notebooks in JSON-on-disk formats: **Jupyter** (`.ipynb`) and **Apache Zeppelin** (`.zpln`). Detection is extension-only. Both expose a shared attribute set so agents can write content-type-agnostic filters like `is_notebook && cell_count > 20`.

| Attribute | Type | Source |
| --- | --- | --- |
| `cell_count` | int | total number of cells (Jupyter) / paragraphs (Zeppelin) |
| `code_cell_count` | int | code cells — Jupyter `cell_type == "code"`, Zeppelin paragraphs whose editor language isn't markdown and whose text doesn't begin with `%md` |
| `markdown_cell_count` | int | markdown cells — Jupyter `cell_type == "markdown"`, Zeppelin paragraphs marked as markdown or starting with `%md` |
| `kernel` | string | Jupyter `metadata.kernelspec.name` (falling back to `display_name`); Zeppelin `defaultInterpreterGroup` (typically `spark`, `python`, etc.) |
| `language` | string | reused — Jupyter `metadata.language_info.name` (falling back to `kernelspec.language`). Zeppelin doesn't surface a notebook-level language. |
| `title` | string | reused — Zeppelin notebook `name`. Jupyter notebooks don't have an explicit title; leave empty (or derive from filename in your query). |

Recipes (CLI + MCP) live in [examples/notebooks.md](./examples/notebooks.md). Detection caveat: a notebook saved with a generic `.json` extension is detected as JSON, not as a notebook — agents and tooling that emit JSON-extension notebooks should rename. The longest-suffix-match detector ensures `.ipynb` and `.zpln` always win over `.json` for those specific extensions.

### Built-in functions — fuzzy, phonetic, and geographic matching

Real-world metadata is messy. Tags get scraped, EXIF strings drift across capitalisations, names get transliterated half a dozen ways. Exact-equality queries (`artist == "Radiohead"`) miss `Radiohad`, `Radiohea`, and `RADIOHEAD`. GPS bounding boxes do fine for "the city of Cape Town" but break down for anything that isn't a rectangle. The CEL environment registers built-in functions to bridge those gaps. They compose with everything else — boolean operators, type predicates, attribute access.

| Function | Returns | What it does |
| --- | --- | --- |
| `levenshtein(a, b)` | int | Edit distance — rune-aware, case-sensitive. Counts insertions, deletions, and substitutions. `café`/`cafe` is one edit, not three. |
| `soundex(s)` | string | American Soundex (NARA standard) — 4-character phonetic code. Words that sound alike collide on the same code. Vowels reset same-code suppression but H/W are transparent (so `Ashcraft` and `Ashcroft` both encode to `A261`). |
| `ngrams(s, n)` | list&lt;string&gt; | Character-level n-grams as a list — sliding window, length `n`. Compose with CEL list operators (`.exists()`, `.size()`, `in`) for set-membership checks. |
| `ngram_similarity(a, b, n)` | double | Jaccard similarity over the deduplicated n-gram sets, ranging 0.0 (no overlap) to 1.0 (identical). The default ergonomic choice for substring-tolerant similarity. |
| `point_in_polygon(lat, lon, polygon)` | bool | Test whether `(lat, lon)` lies inside an arbitrary polygon. `polygon` is a flat `list<double>` of alternating `lat,lon` pairs. Planar ray-casting — good for neighbourhoods, cities, small countries; pre-project for very large or near-pole polygons. |

**Typo-tolerant equality** — within 2 edits of the target, no canonicalisation pass needed:

```sh
file-search-on 'is_audio && levenshtein(artist, "Radiohead") <= 2' -d ~/Music
file-search-on 'is_markdown && levenshtein(author, "Jane Doe") <= 1' -d ./posts
file-search-on 'is_image && levenshtein(camera_make, "FUJIFILM") <= 2' -d ~/Photos
```

**Phonetic match** — collapses spelling variants to one query:

```sh
# Matches Smith / Smyth / Smithe / Smit — all encode to S530.
file-search-on 'is_markdown && soundex(author) == soundex("Smith")'

# EXIF camera-make match across capitalisation and minor typos.
file-search-on 'is_image && soundex(camera_make) == soundex("Nikon")'

# Audio-artist phonetic match — Johnson / Jonson / Jansen all collide on J525.
file-search-on 'is_audio && soundex(artist) == soundex("Johnson")'
```

**Substring-tolerant similarity** — a single threshold catches paraphrases and mild reorderings:

```sh
# Titles with high n-gram overlap to "kubernetes" — survives "Kuburnates",
# "Kubrnates", and other transliterations.
file-search-on 'is_markdown && ngram_similarity(title, "kubernetes", 2) > 0.6'

# Filename match that survives small reorderings.
file-search-on 'ngram_similarity(name, "file-search-on", 3) > 0.5'

# Set-membership over n-grams — files whose title contains the trigram "kub" anywhere.
file-search-on 'is_markdown && "kub" in ngrams(title, 3)'
```

**Geographic point-in-polygon** — for photo searches that aren't rectangles. The polygon is a flat list of alternating `lat,lon` pairs, in order around the boundary; closing back to the first point is implicit:

```sh
# Photos taken inside Cape Town's City Bowl (rough quadrilateral).
file-search-on '
  is_image &&
  point_in_polygon(gps_lat, gps_lon, [
    -33.96, 18.40,
    -33.91, 18.40,
    -33.91, 18.45,
    -33.96, 18.45
  ])
' -d ~/Pictures

# Concave / arbitrary boundaries work — useful for "around the lake but not in
# it" or country borders that aren't rectangles.
file-search-on 'is_image && point_in_polygon(gps_lat, gps_lon, [<your-vertices>])'
```

Algorithm: planar ray-casting — accurate for neighbourhoods, cities, and small countries where curvature is negligible. Pre-project for very large or near-pole polygons.

**They compose naturally** — fuzzy and geographic operators are ordinary CEL functions, so they slot into any expression alongside the structural attributes:

```sh
file-search-on '
  is_image &&
  soundex(camera_make) == soundex("Nikon") &&
  iso < 800 &&
  f_stop < 2.8 &&
  taken_at > timestamp("2024-01-01T00:00:00Z") &&
  point_in_polygon(gps_lat, gps_lon, [-33.96, 18.40, -33.91, 18.40, -33.91, 18.45, -33.96, 18.45])
' -d ~/Photos
```

Pick `n` based on the length of your target string: `n=2` for short tokens (≤8 chars), `n=3` for typical words, `n=4+` for long phrases. Smaller `n` is more forgiving but matches more false positives. The full recipe collection lives at [`examples/fuzzy-search.md`](./examples/fuzzy-search.md), with cross-cutting recipes in [`examples/cookbook.md`](./examples/cookbook.md) and per-family hooks under [`examples/markdown.md`](./examples/markdown.md), [`examples/audio.md`](./examples/audio.md), and [`examples/images.md`](./examples/images.md).

## MCP server mode

The same binary can run as a [Model Context Protocol](https://modelcontextprotocol.io) server, exposing the search to any MCP-compatible client (Claude Desktop, IDE plugins, agents). Three transports:

```sh
file-search-on mcp                                       # stdio (default; for desktop clients)
file-search-on mcp --transport http --addr :8080         # Streamable HTTP (MCP 2025-03-26)
file-search-on mcp --transport sse  --addr :8080         # HTTP+SSE (DEPRECATED — MCP 2024-11-05)
file-search-on mcp --timeout 90s                         # raise the per-call default (60s out of the box)
```

| Transport | Spec version | When to use |
| --- | --- | --- |
| `stdio` | all | Desktop clients (Claude Desktop, IDE plugins) — the agent spawns the binary as a subprocess. |
| `http` | 2025-03-26 | Network-accessible servers, multi-client, or Docker deployments. |
| `sse` | 2024-11-05 | Legacy clients only. The HTTP+SSE transport was deprecated in the 2025-03-26 spec; new deployments should pick `http`. |

For HTTP and SSE, `--addr` (default `:8080`) is the bind address and `--path` (default `/`) is the URL prefix. `--timeout` (default `60s`) sets the per-tool-call deadline; per-call `timeout_seconds` on the `search` tool input overrides it. See [Timeouts and partial results](#timeouts-and-partial-results).

Four tools are exposed:

| Tool | Input | Output |
| --- | --- | --- |
| `search` | `expr`, `dir`, `dirs[]`, `workers`, `max_line_bytes`, `timeout_seconds`, `sort_by`, `order`, `limit`, `include_snippet`, `snippet_lines`, `include_body`, `body_max_bytes`, `excludes`, `respect_gitignore` | `matches[]` (full attribute set per match — includes `snippet` when requested), `count`, `cancelled`, `cancellation_reason`, `elapsed_seconds` |
| `read_attributes` | `path` | A single match — same shape as one `matches[]` entry from `search`. Use when the agent already has the path and wants metadata without walking. |
| `read_lines` | `path`, `start_line`, `end_line`, `max_lines` | `{path, start_line, end_line, total_lines, lines[], truncated}`. A specific line range from a file — pairs with `search` to fetch context around matches. |
| `stats` | `expr` (optional CEL scope), `dir`, `dirs[]`, `group_by`, `workers`, `max_line_bytes`, `timeout_seconds`, `excludes`, `respect_gitignore` | `total_count`, `total_size`, `group_by`, `groups[]` (each `{name, count, total_size}`), `content_types[]` (populated for default group_by only — back-compat), `cancelled`, `cancellation_reason`, `elapsed_seconds`. Reconnaissance histogram, bucketed by any attribute including time-bucket keys (`mtime_year/month/day`, `taken_at_*`, `sent_at_*`, `date_*`). |
| `find_duplicates` | `expr` (optional CEL scope), `dir`, `dirs[]`, `min_size`, `workers`, `max_line_bytes`, `timeout_seconds`, `excludes`, `respect_gitignore` | `total_files`, `duplicate_groups`, `wasted_bytes`, `duplicates[]` (each `{hash, size, count, wasted_bytes, paths[]}`), `cancelled`, `cancellation_reason`, `elapsed_seconds`. Sha256-keyed groups of byte-identical files; sorted by wasted_bytes descending. |
| `list_attributes` | none | `schema` (common, type_specific, frontmatter, functions) and `content_types[]` |
| `index_stats` | none | Cumulative cache counters for the running server: `hits`, `misses`, `puts`, `stales`, `errors`. Counters reset on server restart. |

Empty `expr` matches everything; empty `dir` defaults to `.`. `workers` falls back to `runtime.NumCPU()`. `read_attributes` requires `path`; relative paths resolve against the server's working directory, so absolute paths are preferred.

The MCP server keeps an attribute cache for its entire process lifetime. Repeated `search` and `read_attributes` calls against the same files skip the per-file parse step on the second-and-later invocations — agents that explore a tree iteratively get progressively faster responses. Pass `--index-path /path/to/file.db` to make the cache survive server restarts (see [Persistent attribute index](#persistent-attribute-index) for the file format and validation rules):

```sh
file-search-on mcp --index-path ~/.cache/fso/agent.db                    # stdio + persistent cache
file-search-on mcp --transport http --addr :8080 --index-path /var/lib/fso.db
```

Example Claude Desktop entry in `claude_desktop_config.json` (stdio):

```json
{
  "mcpServers": {
    "file-search-on": {
      "command": "file-search-on",
      "args": ["mcp"]
    }
  }
}
```

For HTTP-based clients, point at `http://<host>:<port>/` after starting the server with `--transport http`.

Built on [`github.com/modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk).

## Development

```sh
go build ./...                                  # build everything
go test -race -coverprofile=coverage.out ./...  # run the test suite
go vet ./...
go fix ./...                                    # apply Go 1.26 modernizers — see below
golangci-lint run
```

The codebase has three internal packages: `internal/content` (the pluggable type registry), `internal/celexpr` (the CEL evaluator and attribute builder), and `internal/search` (the parallel walker). See [CLAUDE.md](./CLAUDE.md) for an architecture overview and a step-by-step guide to adding a new content type.

### Keeping the code modern with `go fix`

Go 1.26 reintroduced [`go fix`](https://go.dev/blog/gofix) as a code-modernization tool that rewrites your sources to use newer language and standard-library features (`slices.Contains`, `any`, `min`/`max`, `range` over integers, `sync.WaitGroup.Go`, and more).

```sh
go fix -diff ./...   # preview the changes
go fix ./...         # apply them
go tool fix help     # list every available fixer
```

CI runs `go fix ./... && git diff --exit-code` after every build, so the project stays idiomatic for whichever Go release the toolchain is pinned to. After bumping the Go version, run `go fix ./...` from a clean working tree and commit the result on its own.

### Fuzz testing

High-risk parsers (frontmatter, MP3 headers, MKV EBML, MP4 boxes, CEL compile, gob decoder) have native [Go fuzz targets](https://go.dev/doc/security/fuzz). The seed corpus runs on every CI build (via the regular `go test` invocation); a scheduled workflow (`.github/workflows/fuzz.yml`) runs each target for 5 minutes nightly to discover new failures. Crashes are uploaded as workflow artifacts and should be committed to `testdata/fuzz/<FuzzName>/` for regression coverage.

Run locally:

```sh
go test -run=FuzzSplitFrontmatter ./internal/content/                 # seed corpus only (fast)
go test -fuzz=FuzzSplitFrontmatter -fuzztime=30s ./internal/content/  # mutate for 30 seconds
```

See [CLAUDE.md](./CLAUDE.md#fuzz-testing) for the list of current targets and a guide to adding new ones.

## License

[MIT](./LICENSE)
