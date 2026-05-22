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

Across **74 file formats** organised into thirteen content-type families (documents, data, images, audio, video, office, ebooks, plain text, archives, compiled binaries, email, source code, notebooks), with format-specific metadata extraction.

Built in the open — issues, PRs, and feature requests warmly welcomed. See [Contributing](#contributing).

## Features

- **Pluggable content-type detection** — extension-first with magic-byte fallback. New formats are a single registration call.
- **Thirteen content-type families**, each with its own metadata extractors:

  | Family | Formats | Bundle of attributes |
  | --- | --- | --- |
  | **Documents** | PDF, EPUB | title, author, language, page_count |
  | **Markup** | Markdown, HTML, XML | title, word_count, frontmatter, language, root_element |
  | **Data** | JSON, YAML, TOML, CSV, TSV | json_kind, yaml_kind, yaml_document_count, column_count, csv_columns |
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
  | **Disk images** | DMG (UDIF), ISO 9660, VHD, VHDX, VMDK (sparse), QCOW2, WIM | disk_image_format, virtual_size, disk_type, volume_label, disk_image_created_at, cluster_bits, is_encrypted, image_count |
  | **Install packages** | macOS `.pkg` (XAR), Debian `.deb`, Red Hat `.rpm`, Linux `.appimage` | package_format, package_name, package_version, package_release, package_arch, package_kind, appimage_version |
  | **VM bytecode** | Java `.class` (JVM), Python `.pyc` / `.pyo`, WebAssembly `.wasm` | bytecode_format, runtime_version, class_name (JVM), super_class (JVM), interfaces (JVM), method_count (JVM), field_count (JVM), access_flags (JVM), python_version, source_mtime, wasm_version, section_count, import_count, export_count |
  | **Science data** | FITS (Flexible Image Transport System), VOTable (IVOA astronomical tables), HDF5 (Hierarchical Data Format v5 — LSST, LIGO, NetCDF4, scientific simulations), PDS3 + PDS4 (NASA Planetary Data System — Voyager, Mars rovers, Perseverance, Lucy), CDF (NASA Common Data Format — heliophysics: ACE, Wind, MMS, Parker Solar Probe) | science_format, telescope, instrument, object (→ title), observer (→ author), date_obs (→ taken_at), exptime, filter, airmass, ra, dec, bitpix, naxis, naxis1, naxis2, hdu_count, fits_kind, votable_version, table_count, total_rows, field_names, field_units, field_ucds, votable_data_format, hdf5_format_version, hdf5_size_of_offsets, hdf5_size_of_lengths, pds_version, mission_name, spacecraft_name, instrument_name, target_name, product_id, start_time (→ taken_at), cdf_version, cdf_encoding, cdf_majority, variable_count, attribute_count |
  | **Databases** | SQLite v3 (the most-deployed database in the world — every iOS / Android app, every browser, every CLI with a local store) | database_format, sqlite_page_size, sqlite_format_version, sqlite_page_count, sqlite_schema_version, sqlite_text_encoding, sqlite_user_version, sqlite_application_id |

  Type predicates (`is_pdf`, `is_image`, `is_audio`, `is_video`, `is_office`, `is_epub`, …) light up automatically from the registered content type. See [examples/](./examples/) for recipes by family.

- **Exact-name content types** for common repo files — `Dockerfile`, `Makefile`, `LICENSE`, `.gitignore`, `go.mod`, `package.json`, `Cargo.toml`, `Pipfile`, `Gemfile`, `requirements.txt`, `Procfile`, `Vagrantfile`, and more — with per-type predicates (`is_dockerfile`, `is_gomod`, `is_node_manifest`, …) plus family predicates (`is_build`, `is_repo_meta`, `is_ignore`, `is_manifest`, `is_platform`). Predicates cross-fire: `package.json` is both `is_node_manifest` and `is_json`. See [examples/repo-files.md](./examples/repo-files.md).
- **OS-generated metadata files** — `.DS_Store` / `.localized` (macOS), `Thumbs.db` / `Desktop.ini` (Windows), `.directory` (KDE) — with per-type predicates (`is_ds_store`, `is_localized`, `is_thumbs_db`, `is_desktop_ini`, `is_kde_directory`), OS-specific family predicates (`is_macos_metadata`, `is_windows_metadata`, `is_linux_metadata`), and the cross-OS `is_system_metadata`. Lets agents answer "find every macOS leftover under `~/Code`" or "what platform-cruft is in this archive?" in one query.
- **Project-type detection** — `detect-project` / `find-projects` / `which-project` subcommands identify Go / Node / Rust / Python / Ruby / Java / .NET / Terraform / Docker Compose / Hugo / Jekyll / Eleventy / Astro / Gatsby / MkDocs / Docusaurus / Pelican projects (8 SSG types + 10 others). Pair with `--resolve-projects` (file-level `project_type` filter) and `--prune-build-artefacts` (skip `vendor`/`node_modules`/`target`/`__pycache__`/`public`/`_site` etc. automatically). The `is_static_site` CEL predicate addresses any SSG as a group. Define custom project types via CEL in YAML — see [examples/projects.md](./examples/projects.md).
- **First-class Markdown front-matter** — YAML (`---`), TOML (`+++`), and JSON (`{ ... }`) are recognised by leading bytes. Common keys (`title`, `author`, `language`, `tags`, `categories`, `draft`, `date`) become top-level CEL variables; everything else lives in a generic `frontmatter` map. See [examples/markdown.md](./examples/markdown.md).
- **CEL expressions** — the full Common Expression Language: comparisons, `&&`/`||`, string functions, list membership, timestamp arithmetic. Composes naturally with structural attributes.
- **Fuzzy, phonetic, and geographic matching** — built-in `levenshtein`, `soundex`, `ngrams`, `ngram_similarity`, and `point_in_polygon` (for GPS bboxes / city outlines) let you write typo-tolerant and "sounds-like" queries against any string attribute. EXIF camera make in `Nikkon` instead of `Nikon`? Artist tag mistyped as `Radiohad`? Same query catches all of them. See [examples/fuzzy-search.md](./examples/fuzzy-search.md).
- **Multiple output formats** — `bare` (paths only), `default`, `verbose` (multi-line), `json` (NDJSON), or a Go `text/template` via `--format`.
- **MCP server mode** — same binary doubles as a [Model Context Protocol](https://modelcontextprotocol.io) server (stdio, HTTP, or SSE). Fourteen tools exposed: `search`, `read_attributes`, `read_lines`, `stats`, `find_duplicates`, `find_near_duplicates`, `find_matches`, `list_archive_contents`, `read_file_in_archive`, `detect_project`, `find_projects`, `resolve_project_for_path`, `list_attributes`, `index_stats`.
- **Pure Go, no CGO** — cross-compiles cleanly to all six release targets. No image/audio/video decoder dependencies.
- **Parallel walking** — files are evaluated across a worker pool (defaults to `NumCPU`).

## Install

### Homebrew (macOS / Linux)

```sh
brew install richardwooding/tap/file-search-on
```

The cask is published from this repo on every tagged release to [`richardwooding/homebrew-tap`](https://github.com/richardwooding/homebrew-tap).

> **macOS note:** the binary isn't signed with an Apple Developer ID (yet — happy to accept a sponsor!). The Homebrew cask's post-install hook strips the quarantine xattr automatically. If macOS still blocks it on first run:
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

`search` is the default subcommand. Pass a CEL expression and a directory:

```sh
file-search-on 'is_markdown && word_count > 500' -d ./docs
file-search-on 'is_image && iso > 1600' -d ~/Pictures -o json
file-search-on 'is_video && duration > 1800 && video_height >= 2160' -d ~/Movies
file-search-on -d .                                   # empty expression matches every file
```

### Subcommands

| Command | Purpose | Deep dive |
|---|---|---|
| `search` *(default)* | CEL expression over file metadata | every page in [examples/](./examples/) |
| `preset [name]` | Run a named search recipe — `recent_changes`, `large_files`, `suspicious_files`, etc. Without args, lists all presets. | [examples/presets.md](./examples/presets.md) |
| `attrs <path>` | Print attributes for one file (no walk, no CEL) | [examples/cookbook.md](./examples/cookbook.md) |
| `stats [expr]` | Histogram + totals, bucketed by `group_by` | [examples/group-by.md](./examples/group-by.md) |
| `duplicates [expr]` | Byte-identical files by sha256 | [examples/duplicates.md](./examples/duplicates.md) |
| `near-duplicates [expr]` | Similar files by SimHash fingerprint of extracted body | [examples/near-duplicates.md](./examples/near-duplicates.md) |
| `archive-contents <path> [--expr]` | List or filter entries inside ZIP / TAR / TAR.GZ / GZIP — full CEL vocabulary on per-entry attributes | [examples/archive-search.md](./examples/archive-search.md) |
| `archive-read <path> <entry>` | Read a single entry's bytes out of an archive without extracting | [examples/archive-search.md](./examples/archive-search.md) |
| `find-matches <re> --expr <cel> -C N` | Line-level regex hits with context | [examples/find-matches.md](./examples/find-matches.md) |
| `lines <path> --start --end` | Print a line range | [examples/read-lines.md](./examples/read-lines.md) |
| `detect-project [dir]` | Identify project type(s) of a directory | [examples/projects.md](./examples/projects.md) |
| `find-projects [root]` | Walk a tree listing every project subdirectory | [examples/projects.md](./examples/projects.md) |
| `which-project <path>` | Walk UP from a file/dir to its nearest enclosing project root | [examples/projects.md](./examples/projects.md) |
| `config-paths` | Print platform-specific project-type config paths | [examples/projects.md](./examples/projects.md) |
| `mcp` | Run as a Model Context Protocol server | [MCP server mode](#mcp-server-mode) |

`file-search-on --list` prints the canonical schema (every attribute, every built-in function, every registered content type) — useful for "what can I filter on?" exploration.

### Output formats

```sh
file-search-on '...' -o bare        # paths only — pipes well into xargs / fzf
file-search-on '...' -o default     # path \t [content-type] \t size
file-search-on '...' -o verbose     # multi-line per match with every attribute
file-search-on '...' -o json        # NDJSON, one match per line
file-search-on '...' --format '{{.Path}} ({{.WordCount}} words)'
```

### Content search

CEL's standard string methods (`contains`, `startsWith`, `endsWith`, `matches`) work on every string attribute. Pass `--body` to populate the `body` variable from text-based files (markdown, source, csv, json, xml, html, plus `is_text`) and filter on full file content:

```sh
file-search-on 'is_source && body.contains("panic")' --body -d ./internal
file-search-on 'is_source && body.matches("(?i)\\bTODO\\b")' --body
file-search-on '...' --sort word_count --order desc --limit 5
```

Top-K queries (`--sort` + `--limit`) buffer the full result set, sort, then truncate. Without `--sort`, `--limit` returns the first N in walk order.

For custom ranking — combining multiple attributes or semantic similarity into a single score — pass a CEL expression to `--rank`:

```sh
# Hybrid semantic + recency: weight similarity at 70%, fresh files at 30%
file-search-on 'is_pdf' \
  --semantic-query "Q4 revenue forecast" \
  --embedding-model nomic-embed-text \
  --rank 'similarity * 0.7 + (mod_time > timestamp("2025-01-01T00:00:00Z") ? 0.3 : 0.0)' \
  --limit 10

# Promote PDFs to the top of a mixed result set
file-search-on 'is_pdf || is_office || is_markdown' --rank 'is_pdf' --limit 20
```

The rank expression evaluates per file (after the filter). Higher values rank first; `--order asc` flips. See [examples/ranking.md](./examples/ranking.md) for the full cookbook.

### Stats and reconnaissance

```sh
file-search-on stats -d ~/Downloads                                    # by content_type (default)
file-search-on stats 'is_image' -d ~/Pictures --group-by camera_make
file-search-on stats 'is_source' -d ./src --group-by language
file-search-on stats 'is_image' -d ~/Pictures --group-by taken_at_year
file-search-on stats --dir ~/docs --dir ~/posts --group-by ext         # multi-root aggregation
```

`group_by` keys: `content_type` (default), `ext`, `dir`, `language`, `camera_make`, `camera_model`, `lens`, `artist`, `album`, `genre`, `kernel`, `binary_format`, `binary_type`, `frontmatter_format`, plus time-bucket keys (`mtime_year/month/day`, `taken_at_*`, `sent_at_*`, `date_*`). Unrecognised keys silently fall back to `content_type`.

### Project-type detection

```sh
file-search-on detect-project ~/my-app
file-search-on find-projects ~/Code --type go --type rust
file-search-on 'is_source && project_type == "go"' \
    --resolve-projects --prune-build-artefacts -d ~/Code
file-search-on config-paths                       # where to drop user-wide / per-project YAML
```

`--resolve-projects` walks up from each file's directory to the nearest project root and sets `project_type` (string), `project_types` (list), and `is_static_site` (bool — fires for `hugo` / `jekyll` / `eleventy` / `astro` / `gatsby` / `mkdocs` / `docusaurus` / `pelican`). `--prune-build-artefacts` does a pre-walk to discover all project subdirectories under the search root and skips their canonical artefact directories (`vendor`, `node_modules`, `target`, `__pycache__`, `.venv`, `bin`, `obj`, `.terraform`, `public`, `_site`, `dist`, …). Custom project types are user-definable via CEL — drop a YAML at the path printed by `config-paths`. Full guide: [examples/projects.md](./examples/projects.md).

### Duplicates and disk-eaters

```sh
file-search-on duplicates -d ~/Pictures                   # all duplicates under a tree
file-search-on duplicates 'is_image' -d ~/Pictures        # scope to photos
file-search-on duplicates -d /Volumes/backup --min-size 1048576  # skip files < 1 MiB
file-search-on duplicates -d ~/Downloads -o json
```

Two-pass: files with unique sizes are skipped before any hashing. With `--index-path`, hashes are cached alongside `(size, mtime)` so repeat runs are free.

For SIMILAR (not identical) files — catching typo edits, regenerated headers, template copies that exact-hash dedup misses — use the SimHash-based `near-duplicates` subcommand:

```sh
file-search-on near-duplicates -d ~/notes                          # 0.85 similarity default
file-search-on near-duplicates 'is_markdown' -d ~/notes --threshold 0.95   # whitespace/typo only
file-search-on near-duplicates 'is_source && language == "go"' -d ./src --threshold 0.75
```

Fingerprints cache via `--index-path` alongside the exact hash; repeat runs skip body extraction AND SimHash compute. See [examples/near-duplicates.md](./examples/near-duplicates.md).

### Common flags

`-d <dir>` (repeatable for multi-root walks), `--exclude <glob>` (basename, repeatable), `--respect-gitignore`, `--timeout 30s` (partial results returned on expiry), `--workers N`, `--index-path <file.db>` (persistent attribute cache — see [examples/indexing.md](./examples/indexing.md)).

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
| [`examples/projects.md`](./examples/projects.md) | Project type detection — `detect-project` / `find-projects` for go / node / rust / python / terraform / docker-compose / … |
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
| [`examples/near-duplicates.md`](./examples/near-duplicates.md) | Find SIMILAR files by SimHash fingerprint — `file-search-on near-duplicates --threshold 0.85` |

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

`file-search-on --list` prints the canonical schema with descriptions. The summary below names every attribute so you know what you can reach in a CEL expression; for recipes and detailed semantics see the per-family pages under [examples/](./examples/).

### On every file

`name`, `path`, `dir`, `size`, `ext`, `content_type`, `mod_time`, `created_at` (filesystem birth time / btime — modern fs only), `metadata_changed_at` (ctime — last permission / ownership change), `is_btime_anomaly` (true when `created_at > mod_time`).

### Type predicates

**By format** — `is_markdown`, `is_json`, `is_yaml`, `is_toml`, `is_xml`, `is_html`, `is_pdf`, `is_csv`, `is_text`, `is_image`, `is_audio`, `is_video`, `is_office`, `is_epub`, `is_archive`, `is_binary`, `is_email`, `is_source`, `is_notebook`, `is_disk_image`, `is_dmg`, `is_iso`, `is_vhd`, `is_vhdx`, `is_vmdk`, `is_qcow2`, `is_wim`, `is_install_package`, `is_pkg`, `is_deb`, `is_rpm`, `is_appimage`, `is_test_file`, `is_symlink`, `is_broken_symlink`, `is_bytecode`, `is_class`, `is_pyc`, `is_wasm`, `is_science_data`, `is_fits`, `is_votable`, `is_hdf5`, `is_pds`, `is_pds3`, `is_pds4`, `is_cdf`, `is_database`, `is_sqlite`.

**By exact filename** — `is_dockerfile`, `is_makefile`, `is_justfile`, `is_rakefile`, `is_license`, `is_changelog`, `is_contributing`, `is_codeowners`, `is_gitignore`, `is_dockerignore`, `is_gomod`, `is_node_manifest`, `is_cargo_manifest`, `is_pipfile`, `is_python_reqs`, `is_gemfile`, `is_procfile`, `is_vagrantfile`, `is_ds_store`, `is_localized`, `is_thumbs_db`, `is_desktop_ini`, `is_kde_directory`.

**By family** — `is_build`, `is_repo_meta`, `is_ignore`, `is_manifest`, `is_platform`, `is_macos_metadata`, `is_windows_metadata`, `is_linux_metadata`, `is_system_metadata`. Fire alongside the per-type predicate (a `Dockerfile` is both `is_dockerfile` and `is_build`; a `.DS_Store` is `is_ds_store`, `is_macos_metadata`, AND `is_system_metadata`). Same shape as `is_image` covering every `image/*` subtype.

**Cross-firing**: a `package.json` matches `is_node_manifest` AND `is_json`; `Cargo.toml` matches `is_cargo_manifest` AND `is_toml`; `LICENSE` / `CHANGELOG` / `CONTRIBUTING` / `requirements.txt` match their per-type predicate AND `is_text`.

### Per-family attributes

| Family | Attributes |
|---|---|
| **Documents / markup** | `title`, `author`, `language`, `word_count`, `line_count`, `page_count`, `column_count` |
| **Data** | `json_kind`, `yaml_kind`, `yaml_document_count`, `csv_columns`, `root_element` |
| **Markdown frontmatter** | `tags`, `categories`, `draft`, `date`, `frontmatter`, `frontmatter_format` (plus the document `title`/`author`/`language` keys are promoted) |
| **Body filter** | `body` (text content types; opt-in via `--body` CLI / `include_body` MCP). Use CEL string methods: `body.contains(...)`, `body.matches(...)` (RE2), `body.startsWith(...)`, `size(body)`. |
| **Images** | `img_width`, `img_height`, `camera_make`, `camera_model`, `lens`, `taken_at`, `orientation`, `gps_lat`, `gps_lon`, `iso`, `focal_length`, `f_stop`, `exposure_time` |
| **Audio** | `artist`, `album`, `album_artist`, `composer`, `year`, `track`, `genre`, `duration`, `bitrate`, `nominal_bitrate`, `sample_rate`, `channels`, `bit_depth`, `replaygain_track_gain`, `replaygain_album_gain` |
| **Video** | `video_codec`, `audio_codec`, `video_width`, `video_height`, `frame_rate`, `rotation`, `duration`, `bitrate`, `nominal_bitrate`, `is_hdr`, `color_primaries`, `color_transfer`, `subtitles`, `subtitle_languages` |
| **Archives** | `entry_count`, `uncompressed_size`, `top_level_entries`, `has_root_dir` |
| **Binaries** | `architectures`, `bitness`, `binary_format`, `binary_type`, `is_dynamically_linked`, `is_stripped`, `entry_point` |
| **Email** | `email_to`, `email_cc`, `email_message_id`, `email_in_reply_to`, `sent_at`, `attachment_count`, `email_count` (plus shared `title` / `author`) |
| **Source code** | `language`, `line_count`, `loc`, `comment_loc`, `blank_loc` |
| **Notebooks** | `cell_count`, `code_cell_count`, `markdown_cell_count`, `kernel` (plus shared `language` / `title`) |
| **Disk images** | `disk_image_format`, `virtual_size`, `disk_type` (VHD / VMDK), `volume_label` (ISO), `disk_image_created_at` (VHD / ISO; in-header creation time, distinct from filesystem `created_at`), `cluster_bits` (QCOW2), `is_encrypted` (QCOW2), `image_count` (WIM) |
| **Install packages** | `package_format`, `package_name` (RPM), `package_version` (RPM), `package_release` (RPM), `package_arch` (RPM), `package_kind`, `appimage_version` |
| **Repo metadata** | `license_id` (SPDX id detected from LICENSE / LICENCE / COPYING / UNLICENSE body) |
| **Symlinks** | `is_symlink`, `is_broken_symlink`, `target_path` (raw `ln -s` target; relative or absolute as recorded on disk) |
| **Forensic hashes** | `md5`, `sha1`, `sha256` — populated only when `--with-hashes` (CLI) or `compute_hashes: true` (MCP) is set. Single io.MultiWriter pass over the file; cached alongside (size, mtime). Forensic / NSRL / VirusTotal / threat-intel-feed interop. |
| **Disguise detection** | `magic_content_type`, `extension_content_type`, `is_disguised` — populated only when `--check-disguised` (CLI) or `check_disguised: true` (MCP) is set. `is_disguised` fires when the bytes disagree with the extension (classic "this `.txt` contains a PE binary" indicator). Cached alongside (size, mtime). |
| **Hash allowlist / denylist** | `is_known_good`, `is_known_bad` — populated when `--hash-allowlist` / `--hash-denylist` (CLI) or `hash_allowlist_path` / `hash_denylist_path` (MCP) is set. Both auto-detect text vs pre-built bbolt format. NSRL / VirusTotal / threat-intel-feed interop; combine with `!is_known_good && is_binary` to cut forensic disk-image review surfaces by 80-95%. |
| **Semantic similarity** | `similarity` (double, 0-1) — populated when `--semantic-query` (CLI) / `search_semantic` tool (MCP) is set. Cosine similarity between the file's body embedding and the query embedding, computed via local Ollama. Compose with type predicates: `is_pdf && similarity > 0.7` finds PDFs conceptually related to the query. Vectors cache in the index alongside `(size, mtime)`. |
| **VM bytecode** | `bytecode_format`, `runtime_version`, `class_name` (JVM), `super_class` (JVM), `interfaces` (JVM), `method_count` (JVM), `field_count` (JVM), `access_flags` (JVM), `python_version`, `source_mtime`, `wasm_version`, `section_count`, `import_count`, `export_count` |
| **Science data — FITS** | `science_format`, `telescope`, `instrument`, `object`, `observer`, `date_obs`, `exptime`, `filter`, `airmass`, `ra`, `dec`, `bitpix`, `naxis`, `naxis1`, `naxis2`, `hdu_count`, `fits_kind` (plus shared `title` ← `OBJECT`, `author` ← `OBSERVER`, `taken_at` ← parsed `DATE-OBS`) |
| **Science data — VOTable** | `votable_version`, `table_count`, `total_rows`, `field_names`, `field_units`, `field_ucds`, `votable_data_format` (plus shared `title` ← root `DESCRIPTION`, `author` ← `INFO[@name='creator']`) |
| **Science data — HDF5** | `hdf5_format_version`, `hdf5_size_of_offsets`, `hdf5_size_of_lengths` (v1 scope is superblock-only; recursive hierarchy walk — `group_count`, `dataset_count`, `top_level_groups` — is a follow-up) |
| **Science data — PDS** | `pds_version` (PDS3 or PDS4), `mission_name`, `spacecraft_name`, `instrument_name`, `target_name`, `product_id`, `start_time` (plus shared `title` ← composed from instrument + target, or PDS4 explicit title; `taken_at` ← parsed `start_time`) |
| **Science data — CDF** | `cdf_version`, `cdf_encoding`, `cdf_majority` (row / column), `variable_count` (NrVars + NzVars), `attribute_count`. v1 surfaces CDR + GDR header fields; the ISTP global-attribute walk for `title` / `author` / `taken_at` is a follow-up. |
| **Databases — SQLite** | `database_format`, `sqlite_page_size`, `sqlite_format_version` (1 legacy / 2 WAL), `sqlite_page_count`, `sqlite_schema_version`, `sqlite_text_encoding` (utf-8 / utf-16le / utf-16be), `sqlite_user_version`, `sqlite_application_id`. v1 surfaces the 100-byte header only; schema introspection (table names, counts, fingerprint) is a follow-up. |
| **Project context** | `module`, `go_version`, `base_image`, `project_types`, `project_type` (the last two populated by `--resolve-projects`) |

### Built-in CEL functions

| Function | Returns | What it does |
|---|---|---|
| `levenshtein(a, b)` | int | Edit distance, rune-aware |
| `soundex(s)` | string | NARA-standard phonetic 4-char code |
| `ngrams(s, n)` | list&lt;string&gt; | Character n-grams as a list |
| `ngram_similarity(a, b, n)` | double | Jaccard similarity over n-gram sets, 0.0–1.0 |
| `point_in_polygon(lat, lon, polygon)` | bool | Ray-casting; `polygon` is a flat `lat,lon,lat,lon,…` list |

CEL's standard string methods (`contains`, `startsWith`, `endsWith`, `matches`, `size`) work on every string attribute. Recipes: [examples/fuzzy-search.md](./examples/fuzzy-search.md).

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

For HTTP and SSE, `--addr` (default `:8080`) is the bind address and `--path` (default `/`) is the URL prefix. `--timeout` (default `60s`) sets the per-tool-call deadline; per-call `timeout_seconds` on the `search` tool input overrides it.

Eleven tools are exposed:

| Tool | What it does |
| --- | --- |
| `search` | CEL expression over a directory tree. Supports `sort_by` / `limit` (top-K), `include_body` (full body filter), `include_snippet` (preview), `resolve_projects` (`project_type` per match), `prune_build_artefacts`, `fields` (project response to a subset of attributes; path / content_type / size always-on). Returns matches with the full attribute set + partial-result fields. |
| `read_attributes` | Attributes for a single path — same shape as one `search` match. Accepts `fields` for the same token-saving projection. |
| `read_lines` | A specific line range of a file — pairs with `search` for context around matches. |
| `stats` | Histogram + totals for a directory tree, bucketed by `group_by` (default `content_type`; full set documented in [Usage § Stats](#stats-and-reconnaissance)). |
| `find_duplicates` | Byte-identical files keyed by sha256 — two-pass (size-bucket then hash). Sorted by `wasted_bytes` desc. |
| `find_near_duplicates` | Similar files by SimHash fingerprint of extracted body. Catches typo edits, regenerated headers, template copies. Configurable similarity threshold (default 0.85). |
| `list_archive_contents` | Per-entry CEL filtering inside ZIP / TAR / TAR.GZ / GZIP without extracting. Same vocabulary as top-level search; cache-aware. |
| `read_file_in_archive` | Read one named entry's bytes out of an archive. Returns content + content_type + attributes. |
| `find_matches` | Line-level regex (RE2) hits across a tree with `context_before` / `context_after` windows. CEL pre-prune (e.g. `is_source && language == "go"`) keeps the regex pass narrow. Replaces the search-then-`read_lines` dance with one call. |
| `detect_project` | Project type(s) of one directory. |
| `find_projects` | Walk a tree, list every project subdirectory. |
| `resolve_project_for_path` | Walk UP from a file/dir path to the nearest enclosing project root. Useful when an agent has a stray path and needs to know the project context. |
| `list_attributes` | The full canonical schema (`common`, `type_specific`, `frontmatter`, `functions`) plus registered content types. |
| `index_stats` | Cache counters for the running server (hits, misses, puts, stales, errors). |

Every walking tool (`search`, `stats`, `find_duplicates`, `find_near_duplicates`, `find_matches`, `find_projects`) honours the same partial-result contract: on timeout the call returns `cancelled=true` with the results gathered so far, never an error. Agents inspect the flag rather than catching exceptions.

The MCP server keeps an attribute cache for its process lifetime — repeated `search` / `read_attributes` calls against the same files skip the parse step on the second and later invocations. Pass `--index-path` to persist the cache across restarts:

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

## Contributing

The project is small enough to read in an afternoon and welcoming to first-time contributors. See [CONTRIBUTING.md](./CONTRIBUTING.md) for setup, branch/commit conventions, the local CI matrix, and PR expectations. A few quick entry points:

- Open issues filtered by [`good first issue`](https://github.com/richardwooding/file-search-on/labels/good%20first%20issue), [`help wanted`](https://github.com/richardwooding/file-search-on/labels/help%20wanted), [`enhancement`](https://github.com/richardwooding/file-search-on/labels/enhancement).
- New content type or CEL function? [CLAUDE.md](./CLAUDE.md) has step-by-step recipes — search for "Adding a new content type" and "Adding a CEL function".
- Security issue? Please don't open a public issue — see [SECURITY.md](./SECURITY.md) for the private reporting channel.

Local CI matrix:

```sh
go build ./...
go test -race ./...
go vet ./...
golangci-lint run
go fix -diff ./...   # CI enforces empty diff
```

That's the whole CI matrix locally. Tests run in under 10 seconds; the race detector is on by default.

### Architecture map

[CLAUDE.md](./CLAUDE.md) is the canonical architecture map — five internal packages, the CEL evaluator's data shape, the walker's cancellation contract, the MCP server's tool surface, the release pipeline, and where every gotcha is documented. Written for both human and LLM contributors; either audience should find it readable.

The repo also ships with [`.claude/skills/`](./.claude/skills/) — step-by-step templates for the repetitive contributions: adding a content type, extending the CEL schema, adding an MCP tool, cutting a release. Useful whether you're working solo or pairing with an LLM agent.

### Releases

Tag-driven via GoReleaser v2 + ko. Pushing `vX.Y.Z` to `main` triggers six platform archives, an OCI image at `ghcr.io/richardwooding/file-search-on:X.Y.Z`, and an auto-commit to the Homebrew tap. Full pipeline documented in [CLAUDE.md § Releases](./CLAUDE.md#releases).

## License

[MIT](./LICENSE)
