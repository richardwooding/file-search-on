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

## Quick start

Install with Homebrew, then hand the search tools to **Claude Code** — two commands:

```sh
# 1. Install the binary (macOS / Linux)
brew install richardwooding/tap/file-search-on

# 2. Register it as an MCP server in Claude Code
claude mcp add file-search-on -- file-search-on mcp
```

That's it. Claude Code can now query your files by typed content-type attributes — ask it things like *"find every PDF over 10 pages I haven't opened this year"*, *"which Go files have the highest git churn?"*, or *"are there any AWS keys in this repo?"* and it drives the [twenty MCP tools](#mcp-server-mode) behind the scenes.

- Make it available in **every** project (not just the current one) with the user scope: `claude mcp add -s user file-search-on -- file-search-on mcp`.
- Confirm the connection with `claude mcp list`, or `/mcp` inside a Claude Code session.

Prefer the command line? The same binary is a standalone CLI — `file-search-on 'is_pdf && page_count > 10'`. See [Usage](#usage). Other install methods (Docker, pre-built binaries, `go install`) are under [Install](#install).

## Features

- **Pluggable content-type detection** — extension-first with magic-byte fallback. New formats are a single registration call.
- **Thirteen content-type families**, each with its own metadata extractors:

  | Family | Formats | Bundle of attributes |
  | --- | --- | --- |
  | **Documents** | PDF, EPUB | title, author, language, page_count |
  | **Markup** | Markdown, HTML, XML | title, word_count, frontmatter, language, root_element |
  | **Data** | JSON, YAML, TOML, CSV, TSV | json_kind, yaml_kind, yaml_document_count, column_count, csv_columns |
  | **Plain text** | TXT, log, … | line_count, word_count |
  | **Images** | JPEG, PNG, GIF, WebP, TIFF, BMP, SVG, HEIC, RAW (Canon CR2 / CR3, Nikon NEF, Sony ARW, Adobe DNG, Fujifilm RAF, Olympus ORF, Panasonic RW2) — predicates `is_raw_photo`, `is_cr2`, `is_cr3`, `is_nef`, `is_arw`, `is_dng`, `is_raf`, `is_orf`, `is_rw2`. HEIC + sibling MOV → Apple Live Photo pairing (`is_live_photo`, `is_live_photo_video`). | dimensions + EXIF: camera, lens, GPS, ISO, focal_length, taken_at; RAW adds `raw_kind`, `raw_vendor`; Live Photo adds `live_photo_video_path`, `live_photo_video_size`, `live_photo_image_path` |
  | **Audio** | MP3, M4A, FLAC, OGG, WAV | tags (artist, album, genre, year, …) + duration, bitrate / nominal_bitrate, sample_rate, channels, bit_depth, ReplayGain |
  | **Video** | MP4, MOV, MKV, WebM, AVI | duration, bitrate / nominal_bitrate, video_codec, audio_codec, video_width/height, frame_rate, rotation, HDR / colour-space, subtitles |
  | **Office** | DOCX, XLSX, PPTX, ODT | title, author, language (Dublin Core) |
  | **Archives** | ZIP (incl. JAR / WAR / EAR), TAR, TAR.GZ, GZIP | entry_count, uncompressed_size, top_level_entries, has_root_dir |
  | **Binaries** | ELF (Linux/BSD), Mach-O (macOS, incl. universal), PE (Windows). Mach-O code signature parsing surfaces team ID + entitlements. | architectures, bitness, binary_format, binary_type, is_dynamically_linked, is_stripped, entry_point, is_codesigned, is_apple_signed, is_third_party_signed, codesign_identifier, codesign_team_id, codesign_hash_type, codesign_hardened_runtime, codesign_library_validation, codesign_killed, codesign_adhoc, entitlements, entitlement_app_sandbox, entitlement_full_disk_access, entitlement_network_client, entitlement_network_server |
  | **Email** | RFC 5322 (`.eml`), Unix mbox (`.mbox`) | title (subject), author (from), email_to, email_cc, sent_at, attachment_count, email_count |
  | **Source code** | Go, Python, JS/TS, Rust, C/C++, Java, Ruby, Swift, Kotlin, Scala, Shell, Lua, Elixir, Clojure, Haskell, OCaml, Zig, C#, PHP, Perl, R, Ada, SQL, Visual Basic, Fortran, MATLAB, Assembly, Pascal/Delphi | language, line_count, loc, comment_loc, blank_loc, functions / type_names / imports (Go / Python / Java / C# / PHP / Perl / R / MATLAB only) |
  | **Notebooks** | Jupyter `.ipynb`, Apache Zeppelin `.zpln` | cell_count, code_cell_count, markdown_cell_count, kernel, language, title |
  | **Disk images** | DMG (UDIF), ISO 9660, VHD, VHDX, VMDK (sparse), QCOW2, WIM | disk_image_format, virtual_size, disk_type, volume_label, disk_image_created_at, cluster_bits, is_encrypted, image_count |
  | **Install packages** | macOS `.pkg` (XAR), Debian `.deb`, Red Hat `.rpm`, Linux `.appimage` | package_format, package_name, package_version, package_release, package_arch, package_kind, appimage_version |
  | **VM bytecode** | Java `.class` (JVM), Python `.pyc` / `.pyo`, WebAssembly `.wasm` | bytecode_format, runtime_version, class_name (JVM), super_class (JVM), interfaces (JVM), method_count (JVM), field_count (JVM), access_flags (JVM), python_version, source_mtime, wasm_version, section_count, import_count, export_count |
  | **Science data** | FITS (Flexible Image Transport System), VOTable (IVOA astronomical tables), HDF5 (Hierarchical Data Format v5 — LSST, LIGO, NetCDF4, scientific simulations), PDS3 + PDS4 (NASA Planetary Data System — Voyager, Mars rovers, Perseverance, Lucy), CDF (NASA Common Data Format — heliophysics: ACE, Wind, MMS, Parker Solar Probe) | science_format, telescope, instrument, object (→ title), observer (→ author), date_obs (→ taken_at), exptime, filter, airmass, ra, dec, bitpix, naxis, naxis1, naxis2, hdu_count, fits_kind, votable_version, table_count, total_rows, field_names, field_units, field_ucds, votable_data_format, hdf5_format_version, hdf5_size_of_offsets, hdf5_size_of_lengths, pds_version, mission_name, spacecraft_name, instrument_name, target_name, product_id, start_time (→ taken_at), cdf_version, cdf_encoding, cdf_majority, variable_count, attribute_count |
  | **Databases** | SQLite v3 + WAL / SHM sidecars + FTS3/4/5 body extraction (the most-deployed database in the world — every iOS / Android app, every browser, every CLI with a local store) | database_format, sqlite_page_size, sqlite_format_version, sqlite_page_count, sqlite_schema_version, sqlite_text_encoding, sqlite_user_version, sqlite_application_id, sqlite_application_name, sqlite_fts_table_count, sqlite_fts_table_names, sqlite_wal_format_version, sqlite_wal_page_size, sqlite_wal_checkpoint_seq, sqlite_wal_frame_count, sqlite_wal_byte_order |
  | **Apple property lists** | Binary (`bplist00`) + XML `.plist` — Info.plist, LaunchAgents, LaunchDaemons, Preferences, `.webloc` | plist_format, plist_root_kind, plist_kind, plist_bundle_identifier, plist_bundle_name, plist_bundle_version, plist_bundle_short_version, plist_executable, plist_min_os_version, plist_label, plist_program, plist_program_arguments, plist_run_at_load, plist_keep_alive |
  | **Browser bookmarks** | Chromium-family (Chrome / Brave / Edge / Chromium / Opera / Vivaldi / Arc) `Bookmarks` JSON + Safari `Bookmarks.plist` | bookmark_count, bookmark_folder_count, bookmark_folders, bookmark_urls, bookmark_titles, browser_vendor, bookmark_profile |
  | **Chat exports** | Slack workspace exports, Discord (DiscordChatExporter) dumps, signal-cli `--json` — detected by JSON shape (`is_chat_export` / `is_slack_export` / `is_discord_export` / `is_signal_export`) | chat_message_count, chat_participants, chat_channel, chat_workspace, chat_start_at, chat_end_at |
  | **Fonts** | TTF, OTF, TTC / OTC collections, WOFF1, WOFF2 (brotli decompression — full attribute extraction) | font_format, font_outline_kind, font_family, font_subfamily, font_full_name, font_version, font_postscript_name, font_manufacturer, font_designer, font_license, font_license_url, font_typographic_family, font_weight, font_width, font_embedding, font_panose, font_unicode_ranges, font_revision, font_units_per_em, font_mac_style, font_italic_angle, font_glyph_count, font_axis_count, font_axes, font_collection_count, font_collection_families, woff2_total_sfnt_size, woff2_total_compressed_size |
  | **3D models** | STL (ASCII + binary), Wavefront OBJ, glTF 2.0 (`.gltf` + `.glb`) — predicates `is_3d_model`, `is_stl`, `is_obj`, `is_gltf` | model3d_format, vertex_count, face_count, has_normals, has_textures, materials, bounding_box |

  Type predicates (`is_pdf`, `is_image`, `is_audio`, `is_video`, `is_office`, `is_epub`, …) light up automatically from the registered content type. See [examples/](./examples/) for recipes by family.

- **Exact-name content types** for common repo files — `Dockerfile`, `Makefile`, `LICENSE`, `.gitignore`, `go.mod`, `package.json`, `Cargo.toml`, `Pipfile`, `Gemfile`, `requirements.txt`, `Procfile`, `Vagrantfile`, and more — with per-type predicates (`is_dockerfile`, `is_gomod`, `is_node_manifest`, …) plus family predicates (`is_build`, `is_repo_meta`, `is_ignore`, `is_manifest`, `is_platform`). Predicates cross-fire: `package.json` is both `is_node_manifest` and `is_json`. See [examples/repo-files.md](./examples/repo-files.md).
- **OS-generated metadata files** — `.DS_Store` / `.localized` (macOS), `Thumbs.db` / `Desktop.ini` (Windows), `.directory` (KDE) — with per-type predicates (`is_ds_store`, `is_localized`, `is_thumbs_db`, `is_desktop_ini`, `is_kde_directory`), OS-specific family predicates (`is_macos_metadata`, `is_windows_metadata`, `is_linux_metadata`), and the cross-OS `is_system_metadata`. Lets agents answer "find every macOS leftover under `~/Code`" or "what platform-cruft is in this archive?" in one query.
- **Apple property lists** (`.plist`) — binary (`bplist00`) and XML variants. Surfaces `is_plist` plus a typed attribute set (`plist_format`, `plist_root_kind`, `plist_kind`, `plist_bundle_identifier`, `plist_bundle_name`, `plist_bundle_version`, `plist_bundle_short_version`, `plist_executable`, `plist_min_os_version`, `plist_label`, `plist_program`, `plist_program_arguments`, `plist_run_at_load`, `plist_keep_alive`). Path-based `plist_kind` registry labels Info.plist / LaunchAgents / LaunchDaemons / Preferences / .webloc files. Lets agents answer "which LaunchAgents run on login?", "what apps require macOS 14+?", or "find the Info.plist for `com.example.bundle`" in one query.
- **Browser bookmarks** — Chromium-family `Bookmarks` (Chrome / Brave / Edge / Chromium / Opera / Vivaldi / Arc) and Safari `Bookmarks.plist`. Surfaces `is_bookmark_file` / `is_chromium_bookmarks` / `is_safari_bookmarks` plus `bookmark_count`, `bookmark_folder_count`, `bookmark_folders`, `bookmark_urls`, `bookmark_titles`, `browser_vendor` (chrome / chromium / edge / brave / opera / vivaldi / arc / safari), and `bookmark_profile`. With `--body`, the `body` CEL variable carries `title\turl` lines so `body.contains("kubernetes")` answers "did I bookmark anything about kubernetes?" across every profile in one query.
- **Chat exports** — offline Slack workspace exports, Discord (DiscordChatExporter) JSON dumps, and signal-cli `--json` output. All three are plain `.json` files with arbitrary names, so they're detected by a streaming top-level-JSON-shape discriminator rather than by extension. Surfaces `is_chat_export` plus per-format `is_slack_export` / `is_discord_export` / `is_signal_export`, and a shared attribute set: `chat_message_count`, `chat_participants` (distinct authors), `chat_channel`, `chat_workspace` (guild for Discord; empty for Signal), `chat_start_at`, and `chat_end_at`. With `--body`, the `body` CEL variable carries one `{timestamp}\t{author}\t{text}` line per message so `is_chat_export && body.contains("kubernetes")` greps the conversation text across an entire export. See [examples/chat-exports.md](./examples/chat-exports.md).
- **Screenshot OCR** — `--ocr` runs OCR over `image/*` files via the registered provider (macOS Vision today; Linux Tesseract / Windows.Media.Ocr deferred under the same hook). The recognized text populates the `body` CEL variable so `body.contains("kubernetes")` queries work over `~/Desktop` screenshots the same way they do over markdown files. Plus three new attributes: `ocr_confidence` (0..1 average across recognized lines), `ocr_language` (BCP-47 dominant language), `ocr_provider` (registered engine name). On macOS the OCR helper is bundled in the Homebrew cask; for local dev `make ocr-helper` builds it. On platforms without a registered provider, `--ocr` is a clean no-op. Cached in the body cache (`bodies_v1`) so subsequent walks are free. See [examples/ocr.md](./examples/ocr.md).
- **Fonts** — TrueType (`.ttf`), OpenType (`.otf`), TTC / OTC collections, WOFF1 (`.woff`), and WOFF2 (`.woff2`). WOFF2 attribute extraction runs the brotli decompression hop, then slices the metadata tables (`name` / `OS/2` / `head` / `post` / `maxp` / `fvar`) from the decompressed stream and dispatches to the same per-table decoders as the bare-sfnt path — `font_family`, `font_designer`, `font_weight`, `font_axes` all populate for `.woff2` collections in modern frontend projects. Surfaces format-family predicates (`is_font`, `is_ttf`, `is_otf`, `is_font_collection`, `is_woff`, `is_woff2`) plus trait predicates (`is_variable_font`, `is_color_font`, `is_monospace_font`, `is_italic_font`, `is_bold_font`). Extracted attributes cover the `name` table (family, designer, version, manufacturer, license), `OS/2` (weight, width, embedding permissions, panose, Unicode ranges), `head` (revision, units-per-em, mac style), `post` (italic angle), `maxp` (glyph count), and `fvar` (variable-font axes — `wght` / `wdth` / `slnt` / `ital` / `opsz`). Lets agents answer "find every variable font with an optical-size axis", "license audit — fonts without OFL", or "find Adobe-designed bold fonts" in one query. See [examples/fonts.md](./examples/fonts.md).
- **Project-type detection** — `detect-project` / `find-projects` / `which-project` subcommands identify Go / Node / Rust / Python / Ruby / Java / .NET / Terraform / Docker Compose / Hugo / Jekyll / Eleventy / Astro / Gatsby / MkDocs / Docusaurus / Pelican projects (8 SSG types + 10 others). Pair with `--resolve-projects` (file-level `project_type` filter) and `--prune-build-artefacts` (skip `vendor`/`node_modules`/`target`/`__pycache__`/`public`/`_site` etc. automatically). The `is_static_site` CEL predicate addresses any SSG as a group. Define custom project types via CEL in YAML — see [examples/projects.md](./examples/projects.md).
- **First-class Markdown front-matter** — YAML (`---`), TOML (`+++`), and JSON (`{ ... }`) are recognised by leading bytes. Common keys (`title`, `author`, `language`, `tags`, `categories`, `draft`, `date`) become top-level CEL variables; everything else lives in a generic `frontmatter` map. See [examples/markdown.md](./examples/markdown.md).
- **CEL expressions** — the full Common Expression Language: comparisons, `&&`/`||`, string functions, list membership, timestamp arithmetic. Composes naturally with structural attributes.
- **Fuzzy, phonetic, and geographic matching** — built-in `levenshtein`, `soundex`, `ngrams`, `ngram_similarity`, and `point_in_polygon` (for GPS bboxes / city outlines) let you write typo-tolerant and "sounds-like" queries against any string attribute. EXIF camera make in `Nikkon` instead of `Nikon`? Artist tag mistyped as `Radiohad`? Same query catches all of them. See [examples/fuzzy-search.md](./examples/fuzzy-search.md).
- **Multiple output formats** — `bare` (paths only), `default`, `verbose` (multi-line), `json` (NDJSON), or a Go `text/template` via `--format`.
- **MCP server mode** — same binary doubles as a [Model Context Protocol](https://modelcontextprotocol.io) server (stdio, HTTP, or SSE). Twenty tools exposed: `search`, `search_semantic`, `read_attributes`, `read_lines`, `stats`, `find_duplicates`, `find_near_duplicates`, `diff_trees`, `find_matches`, `watch_search`, `list_archive_contents`, `read_file_in_archive`, `detect_project`, `find_projects`, `resolve_project_for_path`, `list_attributes`, `list_presets`, `query_preset`, `index_stats`, `monitor_info`.
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
| `watch [expr] -d <dir>` | Continuously watch directories; emit each new / changed file that matches — the inverse of `search` | [examples/watch.md](./examples/watch.md) |
| `diff <tree-a> <tree-b> --op <set-op>` | Cross-tree set operations by sha256 — what's in A but not B, the intersection, content drift between same-named files | [examples/diff.md](./examples/diff.md) |
| `organize <expr> --link-into <template>` | Build a templated symlink / copy tree from results — `{raw_vendor}/{taken_at_year}/{basename}` etc. | [examples/organize.md](./examples/organize.md) |
| `lines <path> --start --end` | Print a line range | [examples/read-lines.md](./examples/read-lines.md) |
| `detect-project [dir]` | Identify project type(s) of a directory | [examples/projects.md](./examples/projects.md) |
| `find-projects [root]` | Walk a tree listing every project subdirectory | [examples/projects.md](./examples/projects.md) |
| `which-project <path>` | Walk UP from a file/dir to its nearest enclosing project root | [examples/projects.md](./examples/projects.md) |
| `config-paths` | Print platform-specific project-type config paths | [examples/projects.md](./examples/projects.md) |
| `monitors` | List the dashboard URLs of every running instance (mcp / watch started with `--monitor`) | [examples/monitoring.md](./examples/monitoring.md) |
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

`-d <dir>` (repeatable for multi-root walks), `--exclude <glob>` (basename, repeatable), `--respect-gitignore`, `--timeout 30s` (partial results returned on expiry), `--workers N`, `--index-path <file.db>` (override the per-cwd default index — see [examples/indexing.md](./examples/indexing.md)), `--no-index` (opt out of on-disk caching for hermetic runs).

### Pointing at a non-default Ollama

For semantic search and `search_semantic` (MCP), the embedding HTTP endpoint resolves in this order:

1. `--embedding-server <url>` flag (CLI or `mcp` subcommand)
2. `$OLLAMA_HOST` environment variable
3. `http://localhost:11434` (built-in default)

So a remote Ollama box on the LAN works without a per-invocation flag: `export OLLAMA_HOST=http://gpu-box:11434`. See [examples/semantic-search.md](./examples/semantic-search.md) for the full setup.

## Recipes

Focused recipe collections live under [`examples/`](./examples/):

| Recipe file | What's in it |
| --- | --- |
| [`examples/markdown.md`](./examples/markdown.md) | Front-matter (YAML / TOML / JSON), draft flags, tag membership, custom keys |
| [`examples/images.md`](./examples/images.md) | EXIF camera/lens, GPS bounding boxes, ISO / aperture / focal length, taken-at ranges |
| [`examples/ocr.md`](./examples/ocr.md) | Screenshot OCR via macOS Vision — `body.contains(...)` queries against screenshots (macOS only; Linux / Windows providers are deferred under the same hook) |
| [`examples/audio.md`](./examples/audio.md) | Artist / album / genre / year, bitrate, sample rate, hi-res filtering |
| [`examples/video.md`](./examples/video.md) | Codec, resolution, frame rate, duration, MKV vs MP4 |
| [`examples/3d-models.md`](./examples/3d-models.md) | STL / OBJ / glTF — vertex / face counts, materials, bounding box, printability triage |
| [`examples/office.md`](./examples/office.md) | DOCX / XLSX / PPTX / ODT — title, author, language |
| [`examples/epub.md`](./examples/epub.md) | EPUB books — title, author, language; XMP fallback |
| [`examples/data.md`](./examples/data.md) | JSON arrays vs objects, CSV column membership, XML root elements |
| [`examples/text.md`](./examples/text.md) | Plain text / log files — line count, word count, big-line caps |
| [`examples/notebooks.md`](./examples/notebooks.md) | Jupyter (`.ipynb`) and Apache Zeppelin (`.zpln`) — `cell_count`, `code_cell_count`, `kernel`, `language` |
| [`examples/projects.md`](./examples/projects.md) | Project type detection — `detect-project` / `find-projects` for go / node / rust / python / terraform / docker-compose / … |
| [`examples/cookbook.md`](./examples/cookbook.md) | Cross-cutting recipes — dedupe, mixed media filters, pipeline integration |
| [`examples/fuzzy-search.md`](./examples/fuzzy-search.md) | Fuzzy / phonetic / n-gram similarity matching — `levenshtein`, `soundex`, `ngrams`, `ngram_similarity`; perceptual image similarity (`image_similar_to`) |
| [`examples/secret-scan.md`](./examples/secret-scan.md) | Credential / token triage — `has_secrets(body)` + `secret_kinds(body)` over file content |
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
| [`examples/organize.md`](./examples/organize.md) | Organize by query — templated symlink / copy trees from search results (`organize … --link-into '{raw_vendor}/{taken_at_year}/{basename}'`) |

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

**By format** — `is_markdown`, `is_json`, `is_yaml`, `is_toml`, `is_xml`, `is_html`, `is_pdf`, `is_csv`, `is_text`, `is_image`, `is_audio`, `is_video`, `is_office`, `is_epub`, `is_archive`, `is_binary`, `is_email`, `is_source`, `is_notebook`, `is_disk_image`, `is_dmg`, `is_iso`, `is_vhd`, `is_vhdx`, `is_vmdk`, `is_qcow2`, `is_wim`, `is_install_package`, `is_pkg`, `is_deb`, `is_rpm`, `is_appimage`, `is_test_file`, `is_generated_code`, `is_symlink`, `is_broken_symlink`, `is_bytecode`, `is_class`, `is_pyc`, `is_wasm`, `is_science_data`, `is_fits`, `is_votable`, `is_hdf5`, `is_pds`, `is_pds3`, `is_pds4`, `is_cdf`, `is_database`, `is_sqlite`, `is_sqlite_wal`, `is_sqlite_shm`.

**By exact filename** — `is_dockerfile`, `is_makefile`, `is_justfile`, `is_rakefile`, `is_license`, `is_changelog`, `is_contributing`, `is_codeowners`, `is_gitignore`, `is_dockerignore`, `is_gomod`, `is_node_manifest`, `is_cargo_manifest`, `is_pipfile`, `is_python_reqs`, `is_gemfile`, `is_procfile`, `is_vagrantfile`, `is_ds_store`, `is_localized`, `is_thumbs_db`, `is_desktop_ini`, `is_kde_directory`, `is_plist`.

**By family** — `is_build`, `is_repo_meta`, `is_ignore`, `is_manifest`, `is_platform`, `is_macos_metadata`, `is_windows_metadata`, `is_linux_metadata`, `is_system_metadata`. Fire alongside the per-type predicate (a `Dockerfile` is both `is_dockerfile` and `is_build`; a `.DS_Store` is `is_ds_store`, `is_macos_metadata`, AND `is_system_metadata`). Same shape as `is_image` covering every `image/*` subtype.

**Cross-firing**: a `package.json` matches `is_node_manifest` AND `is_json`; `Cargo.toml` matches `is_cargo_manifest` AND `is_toml`; `LICENSE` / `CHANGELOG` / `CONTRIBUTING` / `requirements.txt` match their per-type predicate AND `is_text`.

### Per-family attributes

| Family | Attributes |
|---|---|
| **Documents / markup** | `title`, `author`, `language`, `word_count`, `line_count`, `page_count`, `column_count` |
| **Data** | `json_kind`, `yaml_kind`, `yaml_document_count`, `csv_columns`, `root_element` |
| **Markdown frontmatter** | `tags`, `categories`, `draft`, `date`, `frontmatter`, `frontmatter_format` (plus the document `title`/`author`/`language` keys are promoted) |
| **Body filter** | `body` (text content types; opt-in via `--body` CLI / `include_body` MCP). Use CEL string methods: `body.contains(...)`, `body.matches(...)` (RE2), `body.startsWith(...)`, `size(body)`. With `--ocr` (CLI) / `ocr_images: true` (MCP), `body` is also populated for `image/*` files via the registered OCR provider (macOS Vision); see `ocr_confidence`, `ocr_language`, `ocr_provider` below. |
| **OCR (image text)** | `ocr_confidence` (0..1 average per-line confidence), `ocr_language` (BCP-47 detected dominant language), `ocr_provider` (registered provider name: `vision-macos` today). Populated only when `--ocr` is set AND an OCR provider is available on the platform. macOS Vision via a bundled Swift helper; Linux Tesseract / Windows.Media.Ocr are future providers under the same hook. Issue #189. |
| **Images** | `img_width`, `img_height`, `camera_make`, `camera_model`, `lens`, `taken_at`, `orientation`, `gps_lat`, `gps_lon`, `iso`, `focal_length`, `f_stop`, `exposure_time`. RAW photos additionally stamp `raw_kind` (`cr2` / `cr3` / `nef` / `arw` / `dng` / `raf` / `orf` / `rw2`) and `raw_vendor` (`canon` / `nikon` / `sony` / `adobe` / `fujifilm` / `olympus` / `panasonic`) — the camera EXIF fields populate via the same `imagemeta` path as JPEG / TIFF. HEIC files paired with a sibling MOV (Apple Live Photos) surface `live_photo_video_path` + `live_photo_video_size`; the MOV side surfaces `live_photo_image_path` and `is_live_photo_video`. With `--with-phash` (CLI) or `with_phash: true` (MCP) — auto-enabled when `image_similar_to(...)` appears in the expression — every image gets a 16-char hex `phash` attribute for perceptual-similarity queries. |
| **Audio** | `artist`, `album`, `album_artist`, `composer`, `year`, `track`, `genre`, `duration`, `bitrate`, `nominal_bitrate`, `sample_rate`, `channels`, `bit_depth`, `replaygain_track_gain`, `replaygain_album_gain` |
| **Video** | `video_codec`, `audio_codec`, `video_width`, `video_height`, `frame_rate`, `rotation`, `duration`, `bitrate`, `nominal_bitrate`, `is_hdr`, `color_primaries`, `color_transfer`, `subtitles`, `subtitle_languages` |
| **Archives** | `entry_count`, `uncompressed_size`, `top_level_entries`, `has_root_dir` |
| **Binaries** | `architectures`, `bitness`, `binary_format`, `binary_type`, `is_dynamically_linked`, `is_stripped`, `entry_point`. Mach-O code signature (macOS-specific): `is_codesigned`, `is_apple_signed`, `is_third_party_signed`, `codesign_identifier`, `codesign_team_id`, `codesign_hash_type`, `codesign_hardened_runtime`, `codesign_library_validation`, `codesign_killed`, `codesign_adhoc`, `entitlements`, `entitlement_app_sandbox`, `entitlement_full_disk_access`, `entitlement_network_client`, `entitlement_network_server` |
| **Email** | `email_to`, `email_cc`, `email_message_id`, `email_in_reply_to`, `sent_at`, `attachment_count`, `email_count` (plus shared `title` / `author`) |
| **Source code** | `language`, `line_count`, `loc`, `comment_loc`, `blank_loc`, `functions`, `type_names`, `imports` (last three populated for Go via stdlib AST + Python / Java / C# / PHP / Perl / R / MATLAB via regex — agents querying "where is X defined?" / "which files import Y?" hit cached attributes instead of grep) |
| **Notebooks** | `cell_count`, `code_cell_count`, `markdown_cell_count`, `kernel` (plus shared `language` / `title`) |
| **Disk images** | `disk_image_format`, `virtual_size`, `disk_type` (VHD / VMDK), `volume_label` (ISO), `disk_image_created_at` (VHD / ISO; in-header creation time, distinct from filesystem `created_at`), `cluster_bits` (QCOW2), `is_encrypted` (QCOW2), `image_count` (WIM) |
| **Install packages** | `package_format`, `package_name` (RPM), `package_version` (RPM), `package_release` (RPM), `package_arch` (RPM), `package_kind`, `appimage_version` |
| **Repo metadata** | `license_id` (SPDX id detected from LICENSE / LICENCE / COPYING / UNLICENSE body) |
| **Symlinks** | `is_symlink`, `is_broken_symlink`, `target_path` (raw `ln -s` target; relative or absolute as recorded on disk) |
| **Forensic hashes** | `md5`, `sha1`, `sha256` — populated only when `--with-hashes` (CLI) or `compute_hashes: true` (MCP) is set. Single io.MultiWriter pass over the file; cached alongside (size, mtime). Forensic / NSRL / VirusTotal / threat-intel-feed interop. |
| **Disguise detection** | `magic_content_type`, `extension_content_type`, `is_disguised` — populated only when `--check-disguised` (CLI) or `check_disguised: true` (MCP) is set. `is_disguised` fires when the bytes disagree with the extension (classic "this `.txt` contains a PE binary" indicator). Cached alongside (size, mtime). |
| **Hash allowlist / denylist** | `is_known_good`, `is_known_bad` — populated when `--hash-allowlist` / `--hash-denylist` (CLI) or `hash_allowlist_path` / `hash_denylist_path` (MCP) is set. Both auto-detect text vs pre-built bbolt format. NSRL / VirusTotal / threat-intel-feed interop; combine with `!is_known_good && is_binary` to cut forensic disk-image review surfaces by 80-95%. |
| **Extended attributes (macOS)** | `xattr_keys`, `xattr_count`, `is_xattr_rich`, `is_quarantined`, `quarantine_agent`, `quarantine_event_id`, `quarantine_source_url`, `quarantine_referrer_url`, `quarantine_download_date`, `quarantine_user_approved`, `finder_tags`, `finder_color`, `has_finder_comment` — populated only when `--with-xattrs` (CLI) or `with_xattrs: true` (MCP) is set. Darwin-only; non-Darwin walks silently leave these empty. Forensic-grade — quarantine carries the source URL + download date + Gatekeeper approval state for every file downloaded from the web. Compose with `is_codesigned` for malware-triage one-liners: `binary_format == "mach-o" && !is_codesigned && is_quarantined`. |
| **Semantic similarity** | `similarity` (double, 0-1) — populated when `--semantic-query` (CLI) / `search_semantic` tool (MCP) is set. Cosine similarity between the file's body embedding and the query embedding, computed via local Ollama. Compose with type predicates: `is_pdf && similarity > 0.7` finds PDFs conceptually related to the query. Vectors cache in the index alongside `(size, mtime)`. |
| **VM bytecode** | `bytecode_format`, `runtime_version`, `class_name` (JVM), `super_class` (JVM), `interfaces` (JVM), `method_count` (JVM), `field_count` (JVM), `access_flags` (JVM), `python_version`, `source_mtime`, `wasm_version`, `section_count`, `import_count`, `export_count` |
| **Science data — FITS** | `science_format`, `telescope`, `instrument`, `object`, `observer`, `date_obs`, `exptime`, `filter`, `airmass`, `ra`, `dec`, `bitpix`, `naxis`, `naxis1`, `naxis2`, `hdu_count`, `fits_kind` (plus shared `title` ← `OBJECT`, `author` ← `OBSERVER`, `taken_at` ← parsed `DATE-OBS`) |
| **Science data — VOTable** | `votable_version`, `table_count`, `total_rows`, `field_names`, `field_units`, `field_ucds`, `votable_data_format` (plus shared `title` ← root `DESCRIPTION`, `author` ← `INFO[@name='creator']`) |
| **Science data — HDF5** | `hdf5_format_version`, `hdf5_size_of_offsets`, `hdf5_size_of_lengths` (v1 scope is superblock-only; recursive hierarchy walk — `group_count`, `dataset_count`, `top_level_groups` — is a follow-up) |
| **Science data — PDS** | `pds_version` (PDS3 or PDS4), `mission_name`, `spacecraft_name`, `instrument_name`, `target_name`, `product_id`, `start_time` (plus shared `title` ← composed from instrument + target, or PDS4 explicit title; `taken_at` ← parsed `start_time`) |
| **Science data — CDF** | `cdf_version`, `cdf_encoding`, `cdf_majority` (row / column), `variable_count` (NrVars + NzVars), `attribute_count`. v1 surfaces CDR + GDR header fields; the ISTP global-attribute walk for `title` / `author` / `taken_at` is a follow-up. |
| **Fonts** | `font_format` (`ttf` / `otf` / `ttc` / `otc` / `woff` / `woff2`), `font_outline_kind` (`truetype` / `cff` / `cff2`), `font_family`, `font_subfamily`, `font_full_name`, `font_version`, `font_postscript_name`, `font_manufacturer`, `font_designer`, `font_license`, `font_license_url`, `font_typographic_family`, `font_weight` (100–900), `font_width` (1–9), `font_embedding` (`installable` / `restricted` / `preview-print` / `editable` — informational, not enforced), `font_panose` (10-byte hex), `font_unicode_ranges`, `font_revision`, `font_units_per_em`, `font_mac_style`, `font_italic_angle`, `font_glyph_count`, `font_axis_count`, `font_axes` (variable-font axes — `wght` / `wdth` / `slnt` / `ital` / `opsz`), `font_collection_count`, `font_collection_families`. WOFF2 surfaces the full set above plus the header byte counts `woff2_total_sfnt_size`, `woff2_total_compressed_size` for compression-ratio queries. The shared `font_family` and `font_designer` also dual-surface to the cross-family `title` and `author` variables. |
| **Databases — SQLite** | Header: `database_format`, `sqlite_page_size`, `sqlite_format_version` (1 legacy / 2 WAL), `sqlite_page_count`, `sqlite_schema_version`, `sqlite_text_encoding` (utf-8 / utf-16le / utf-16be), `sqlite_user_version`, `sqlite_application_id`, `sqlite_application_name` (curated human-readable label from a known-app registry — `firefox-places`, `chrome-history`, `apple-imessage`, `apple-keychain`, `macos-libcache`, `fossil-scm`, …). Schema (via hand-rolled `sqlite_master` b-tree walker): `sqlite_table_count`, `sqlite_view_count`, `sqlite_index_count`, `sqlite_trigger_count`, `sqlite_table_names` (sorted, capped at 100), `sqlite_schema_fingerprint` (SHA256 of sorted CREATE statements). FTS3/4/5 detection: `sqlite_fts_table_count`, `sqlite_fts_table_names`. With `--body`, the `body` CEL variable is populated with the concatenated text from every FTS `_content` shadow table — `body.contains("transformer")` works inside browser history, chat archives, and any other FTS-backed store. Pure-Go via the `modernc.org/sqlite` driver in read-only `immutable=1` mode (no journal / WAL touches). WAL sidecar (`is_sqlite_wal`): `sqlite_wal_format_version`, `sqlite_wal_page_size`, `sqlite_wal_checkpoint_seq`, `sqlite_wal_frame_count`, `sqlite_wal_byte_order` (be / le — checksum byte order). SHM sidecar (`is_sqlite_shm`): extension-only detection, no extra fields. Sidecars deliberately do NOT fire `is_sqlite` / `is_database` — they accompany a database, they aren't one. |
| **3D models** | `model3d_format` (`stl` / `obj` / `gltf`), `vertex_count`, `face_count`, `has_normals`, `has_textures`, `materials` (list — OBJ `usemtl` / glTF `materials[].name`), `bounding_box` (`[minX, minY, minZ, maxX, maxY, maxZ]`). Binary STL reads counts O(1) from the header; glTF reads counts + bbox from the accessor table (no buffer decode). Predicates: `is_3d_model` (umbrella), `is_stl`, `is_obj`, `is_gltf`. |
| **Project context** | `module`, `go_version`, `base_image`, `project_types`, `project_type` (the last two populated by `--resolve-projects`) |
| **Git metadata** | `git_last_commit_time`, `git_last_commit_author`, `git_last_commit_subject`, `git_first_seen`, `git_commit_count`, `is_git_tracked`, `is_git_ignored` — populated when `--with-git` (CLI) / `with_git: true` (MCP) is set AND the walk root is inside a git working tree. One `git log` pass per walk root via the `gitmeta` package — cheap up front, free per-file lookup. Use for repo-aware queries that filesystem `mod_time` can't answer on a fresh clone (every file's mtime is checkout time). Examples: `git_last_commit_time > timestamp("2026-05-01T00:00:00Z")` (recently edited), `is_source && git_commit_count > 50` (high-churn / hot files), `is_source && is_git_tracked && !is_test_file` (production code only). Silent no-op when the root isn't a git tree or when `git` isn't on PATH. Issue #271. |

### Built-in CEL functions

| Function | Returns | What it does |
|---|---|---|
| `levenshtein(a, b)` | int | Edit distance, rune-aware |
| `soundex(s)` | string | NARA-standard phonetic 4-char code |
| `ngrams(s, n)` | list&lt;string&gt; | Character n-grams as a list |
| `ngram_similarity(a, b, n)` | double | Jaccard similarity over n-gram sets, 0.0–1.0 |
| `point_in_polygon(lat, lon, polygon)` | bool | Ray-casting; `polygon` is a flat `lat,lon,lat,lon,…` list |
| `image_similar_to(phash, ref_path, threshold)` | bool | Perceptual image similarity via pHash Hamming distance; auto-enables `--with-phash` |
| `has_secrets(body)` | bool | True when the body contains a credential / token / key (AWS, GitHub, Slack, Stripe, PEM, JWT, …). Requires `--body` |
| `secret_kinds(body)` | list&lt;string&gt; | The secret categories matched in the body — `["aws-access-key", "private-key-pem", …]`. Requires `--body` |

CEL's standard string methods (`contains`, `startsWith`, `endsWith`, `matches`, `size`) work on every string attribute. Recipes: [examples/fuzzy-search.md](./examples/fuzzy-search.md).

## MCP server mode

The same binary can run as a [Model Context Protocol](https://modelcontextprotocol.io) server, exposing the search to any MCP-compatible client (Claude Code, Claude Desktop, IDE plugins, agents). Three transports:

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

### Home-directory safety guard

By default both long-running subcommands (`mcp` and `watch`) **refuse to start unless the directory they operate on is inside your home directory.** This stops an agent-driven server or a background watcher from being aimed — by an errant `cwd`, `--warm-dir`, or `-d` — at system paths or an entire volume.

- For `mcp`, the guarded roots are the **cwd** (the default any tool call inherits when it omits `dir`) plus any explicit `--warm-dir` / `--watch-index-dir` / `--sandbox-dir`. So `cd /etc && file-search-on mcp` is refused.
- For `watch`, every `-d` directory is checked.
- `$HOME` itself and anything beneath it pass; symlinks are resolved on both sides before the check.

Opt out with **`--allow-outside-home`** when you genuinely need to serve or watch a directory elsewhere:

```sh
cd /opt/data && file-search-on mcp --allow-outside-home     # serve a non-home root
file-search-on watch -d /Volumes/Media --allow-outside-home  # watch another volume
```

The guard is **fail-closed**: if `$HOME` can't be determined (e.g. a container or CI runner with no `HOME` set), startup is refused until you set `HOME` or pass `--allow-outside-home`. It's independent of `--sandbox` — the guard is a startup check on the server's own root(s), while `--sandbox` governs the per-call `dir` inputs an agent supplies at runtime.

Twenty tools are exposed, grouped by family:

**Search & inspect**

| Tool | What it does |
| --- | --- |
| `search` | CEL expression over a directory tree. Supports `sort_by` / `limit` (top-K), `rank` (custom CEL sort key), `cursor` / `next_cursor` (stable keyset pagination — page a large match set in bounded chunks), `include_body` (full body filter), `include_snippet` (preview), `ocr_images` (run OCR before evaluating), `with_phash` (perceptual hash + `image_similar_to` function), `compute_hashes`, `check_disguised`, `with_xattrs`, `resolve_projects`, `prune_build_artefacts`, `fields` (token-saving projection — path / content_type / size always-on). Returns matches with the full attribute set + partial-result fields. |
| `search_semantic` | Natural-language similarity search via local Ollama embeddings. Pre-prunes with an optional `expr`, embeds the query, ranks files by cosine similarity, applies a `threshold` cap, and supports `cursor` / `next_cursor` pagination. Embeddings cache per file. |
| `read_attributes` | Attributes for a single path — same shape as one `search` match. Accepts `fields` for token-saving projection. |
| `read_lines` | A specific line range of a file — pairs with `search` for context around matches. |

**Aggregate**

| Tool | What it does |
| --- | --- |
| `stats` | Histogram + totals for a directory tree, bucketed by `group_by` (default `content_type`; recognised: `ext`, `dir`, `language`, `camera_make`, `camera_model`, `lens`, `artist`, `album`, `genre`, time buckets like `taken_at_month`, …). |

**Dedup & diff**

| Tool | What it does |
| --- | --- |
| `find_duplicates` | Byte-identical files keyed by sha256 — two-pass (size-bucket then hash). Sorted by `wasted_bytes` desc. |
| `find_near_duplicates` | Similar files by SimHash fingerprint of extracted body. Catches typo edits, regenerated headers, template copies. Configurable similarity threshold (default 0.85). |
| `diff_trees` | Cross-tree set operations by sha256 content hash — `a-minus-b`, `b-minus-a`, `intersect`, `union`, `mismatch` (same relative path, different content). Read-only; never mutates either tree. |

**Archive**

| Tool | What it does |
| --- | --- |
| `list_archive_contents` | Per-entry CEL filtering inside ZIP / TAR / TAR.GZ / GZIP without extracting. Same vocabulary as top-level search; cache-aware. |
| `read_file_in_archive` | Read one named entry's bytes out of an archive. Returns content + content_type + attributes. |

**Pattern + watch**

| Tool | What it does |
| --- | --- |
| `find_matches` | Line-level regex (RE2) hits across a tree with `context_before` / `context_after` windows. CEL pre-prune (e.g. `is_source && language == "go"`) keeps the regex pass narrow. Replaces the search-then-`read_lines` dance with one call. |
| `watch_search` | Bounded "tell me when X appears" subscription — block up to `duration_seconds` (default 30, capped at 600), return every new / changed file that matches the CEL filter. |

**Project + introspection + monitoring**

| Tool | What it does |
| --- | --- |
| `detect_project` | Project type(s) of one directory. |
| `find_projects` | Walk a tree, list every project subdirectory. |
| `resolve_project_for_path` | Walk UP from a file/dir path to the nearest enclosing project root. Useful when an agent has a stray path and needs to know the project context. |
| `list_attributes` | The full canonical schema (`common`, `type_specific`, `frontmatter`, `functions`) plus registered content types. |
| `list_presets` | Discover the eight built-in named search recipes (`recent_changes`, `recent_photos`, `old_drafts`, `large_files`, `large_binaries`, `suspicious_files`, `failed_tests`, `system_metadata`). |
| `query_preset` | Run a named preset; per-call overrides for `dir`, `limit`, `excludes`, etc. |
| `index_stats` | Cache counters for the running server (hits, misses, puts, stales, errors; same for body + embedding caches). When `--watch-index` is on, also reports `watch_refreshed` / `watch_evicted` / `watch_errors`. |
| `monitor_info` | This server's monitoring-dashboard URL + the registry of sibling instances. Pass `enable: true` to start the dashboard on demand if it isn't already running. |

Every walking tool (`search`, `stats`, `find_duplicates`, `find_near_duplicates`, `find_matches`, `find_projects`, `diff_trees`) honours the same partial-result contract: on timeout the call returns `cancelled=true` with the results gathered so far, never an error. Agents inspect the flag rather than catching exceptions.

**Pagination.** `search` and `search_semantic` support stateless **cursor pagination** for large result sets. Pass `limit` to cap a page; when the set is truncated the response carries an opaque `next_cursor`. Pass it back as `cursor` (with the *same* `sort_by` / `order` / `rank` / `query`) to fetch the next page. The cursor is a keyset over the sort key + path, so paging is stable under an unchanged tree and survives a server restart — there's no server-side cached result set. Each page re-walks the tree, but attribute extraction is index-cached, so re-walks are cheap. An agent can stream a 10k-match set in bounded pages without blowing its context or losing the tail to a hard `limit`.

Since v0.64.0 the on-disk index is **on by default**. The MCP server (like every other long-running subcommand) auto-creates a per-cwd bbolt cache at `<UserCacheDir>/file-search-on/indexes/<basename>-<sha1[:6]>.db` — repeated `search` / `read_attributes` calls against unchanged files skip parsing entirely. The default path is per-cwd so concurrent agents in different projects never collide; same-cwd contention falls back gracefully to in-memory (logged on stderr, surfaced on the dashboard as `index_fallback_reason: "lock_contention"`). Override with `--index-path`; opt out with `--no-index` for hermetic CI runs:

```sh
file-search-on mcp                                                       # default: per-cwd persistent cache
file-search-on mcp --index-path /var/lib/fso.db                          # explicit path (e.g. shared across cwd)
file-search-on mcp --no-index                                            # in-memory only (process lifetime)
file-search-on mcp --transport http --addr :8080
```

**Keeping the cache fresh while it runs — `--watch-index`.** The index is validated lazily by `(size, mtime)`, so a changed file is always re-parsed on its next lookup — results are never stale. `--watch-index` adds a background fsnotify watcher that proactively (1) **re-parses already-cached files when they change** so the *next* query is a warm hit instead of a cold parse, and (2) **evicts cache entries for deleted files** — the one bit of hygiene lazy validation never does (dead paths otherwise accumulate in the bbolt file forever). It's a latency/hygiene optimisation, **not** a correctness fix. Auto-enabled when you `--warm` a root ("warm once, then keep it warm"); off otherwise because watching a huge tree (e.g. `$HOME`) exhausts the OS per-directory watch limit. The watcher honours `.gitignore` and prunes build-artefact dirs (`node_modules`, `target`, …) so churny output doesn't thrash the cache.

```sh
file-search-on mcp --warm                                                # warm cwd, then keep it warm (watcher auto-on)
file-search-on mcp --warm-dir ~/Code/myproj --watch-index                # warm + watch a specific root
file-search-on mcp --watch-index-dir ~/Code/myproj                       # watch a root without a startup warm pass
file-search-on mcp --warm --no-watch-index                               # warm once, but don't keep watching
```

The watcher's activity is reported by the `index_stats` MCP tool as `watch_refreshed` / `watch_evicted` / `watch_errors`; evictions also show as a shrinking attribute-entry count on the monitor dashboard.

### Registering with Claude Code

The fastest path — one command, no config files to hand-edit:

```sh
claude mcp add file-search-on -- file-search-on mcp           # current project
claude mcp add -s user file-search-on -- file-search-on mcp   # every project (user scope)
```

`claude mcp list` shows registered servers and their connection status; `claude mcp remove file-search-on` unregisters it. Inside a session, `/mcp` lists the live tools. If you installed via Homebrew the `file-search-on` command is already on `PATH`; otherwise pass an absolute path as the command.

### Registering with Claude Desktop

Add a stdio entry to `claude_desktop_config.json`:

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

## Monitoring dashboard

Both long-running modes (`mcp` and `watch`) expose a read-only monitoring dashboard. Since v0.65.0 it's **on by default** on a dynamic OS-assigned localhost port — many concurrent stdio agents each get their own dashboard without colliding. The server binds **127.0.0.1 only** (the host part of any address is ignored — only the port is used), needs no auth, and adds no dependencies — the UI is a single embedded page that polls a small JSON API.

```sh
file-search-on mcp                                            # default: dashboard on dynamic port
file-search-on mcp --monitor-addr :9090                       # pin a fixed port instead
file-search-on mcp --no-monitor                               # opt out (hermetic CI / sandboxed runs)
file-search-on mcp --transport http --addr :8080              # dashboard still auto-starts
file-search-on watch 'is_image' -d ~/Screenshots              # default: dashboard auto-starts
file-search-on monitors                                       # list active dashboards across all instances
```

Find the URL in the stderr log line (`monitor dashboard: http://127.0.0.1:<port>/`), via the `monitors` subcommand, or — for an `mcp` server — by calling the **`monitor_info` MCP tool**, which also reports sibling instances. The legacy `--monitor` bool is kept as a no-op for back-compat (same effect as no flag).

Open the URL. Five panels:

- **Overview** — version, uptime, run mode, PID / Go version / GOMAXPROCS, default worker count, **index backend** (🔒 persistent path / 🧠 in-memory with reason — `--no-index` opt-out or lock-contention fallback), body-cache cap.
- **Cache** — the attribute / body / embedding cache counters as live cards with derived **hit-rate %** and sparklines; body evictions / oversize rejects / embed model-mismatches flagged.
- **Activity** — live MCP tool-call feed (tool, elapsed, outcome, result count), per-tool call / error / cancel counts and p50 / p95 / max latency, and an in-flight gauge. (Watch mode has no MCP calls, so this panel shows a notice.)
- **Capabilities** — registered content types grouped by family, project types, OCR provider availability, embedder model / server + a reachability check.
- **Peer switcher** — when more than one instance is running, a header dropdown lists every sibling dashboard (mode · working dir · port) and switches to it. Instances discover each other through a shared registry under the user cache dir; crashed instances self-prune.

### Multiple concurrent instances

Each instance with a dashboard registers itself, so they're mutually discoverable. For `mcp` servers, the **`monitor_info`** tool is the entry point: it returns this server's dashboard URL + the peer list, and `monitor_info{enable:true}` **starts the dashboard on demand** (a dynamic port) even if the server was launched without a monitor flag. That makes monitoring reachable per-agent without editing every launch config.

The JSON API is scriptable too: `curl -s localhost:<port>/api/cache | jq`, plus `/api/overview`, `/api/activity`, `/api/capabilities`, `/api/peers`, and `/healthz` (liveness). See [examples/monitoring.md](./examples/monitoring.md).

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
