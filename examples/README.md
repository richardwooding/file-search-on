# Recipes

CEL expression recipes by content family, plus cross-cutting cookbook patterns.

| File | Topic |
| --- | --- |
| [`markdown.md`](./markdown.md) | Markdown front-matter (YAML / TOML / JSON), tag membership, drafts, custom keys |
| [`images.md`](./images.md) | EXIF — camera, lens, GPS bounding boxes, ISO / aperture, taken_at ranges |
| [`audio.md`](./audio.md) | Audio tags (artist / album / genre / year), bitrate, sample rate, hi-res filtering |
| [`video.md`](./video.md) | Codec, resolution, frame rate, duration, MKV vs MP4 |
| [`office.md`](./office.md) | DOCX / XLSX / PPTX / ODT — title, author, language |
| [`epub.md`](./epub.md) | EPUB ebooks — title, author, language |
| [`pdf.md`](./pdf.md) | PDF — title, author, language, page_count, plus body-content search with caveats |
| [`data.md`](./data.md) | JSON / CSV / TSV / XML — `csv_columns`, `json_kind`, `root_element` |
| [`text.md`](./text.md) | Plain text and HTML — `line_count`, `word_count`, long-line caps |
| [`archives.md`](./archives.md) | ZIP / TAR / TAR.GZ / GZIP — Downloads triage, ZIP-bomb detection, compression ratios |
| [`binaries.md`](./binaries.md) | ELF / Mach-O / PE — architecture filtering, static-vs-dynamic, stripped triage, rogue `.exe` hunting |
| [`disk-images.md`](./disk-images.md) | DMG / ISO / VHD / VHDX / VMDK / QCOW2 / WIM — `virtual_size`, `disk_image_format`, `disk_type` (VHD/VMDK), `volume_label` (ISO), `is_encrypted` (QCOW2), `image_count` (WIM) |
| [`install-packages.md`](./install-packages.md) | macOS `.pkg` / Debian `.deb` / Red Hat `.rpm` / Linux `.appimage` — `package_format`, `package_name` (RPM), `package_version` (RPM), `package_arch` (RPM), `package_kind` |
| [`symlinks.md`](./symlinks.md) | `is_symlink`, `is_broken_symlink`, `target_path` — audit dangling links, follow Homebrew / asdf farms with `--follow-symlinks`, distinguish symlinked from real duplicates |
| [`email.md`](./email.md) | `.eml` / `.mbox` — inbox triage, find emails by sender / subject / date, mbox archive sniffing |
| [`source-code.md`](./source-code.md) | Go / Python / JS / TS / Rust / C / C++ / Java / Ruby / Swift / Kotlin / Scala / Shell / Lua / Elixir / Clojure / Haskell / OCaml / Zig — LOC counts, language filtering, comment density |
| [`notebooks.md`](./notebooks.md) | Jupyter (`.ipynb`) and Apache Zeppelin (`.zpln`) — `cell_count`, `code_cell_count`, `kernel`, `language` |
| [`cookbook.md`](./cookbook.md) | Cross-family queries, output-format pipelines, integration with find / jq / ffmpeg / rga |
| [`fuzzy-search.md`](./fuzzy-search.md) | Fuzzy / phonetic / n-gram similarity matching with `levenshtein`, `soundex`, `ngrams`, `ngram_similarity` |
| [`indexing.md`](./indexing.md) | Persistent attribute index — `--index-path` for the CLI, auto-on cache for MCP, refresh and inspection |
| [`timeouts.md`](./timeouts.md) | Timeouts and partial results — CLI `--timeout`, MCP `timeout_seconds`, exit codes, cancellation semantics |
| [`top-k.md`](./top-k.md) | Top-K queries — `--sort` / `--limit` for "biggest 5 videos", "10 most recent photos", etc. |
| [`snippets.md`](./snippets.md) | Body previews — `--snippet` returns the first N lines of text files alongside metadata |
| [`exclude.md`](./exclude.md) | Pruning the walk — `--exclude` basename globs and `--respect-gitignore` for monorepos |
| [`body-search.md`](./body-search.md) | Content filters — `--body` exposes file body to CEL; pair with `contains` / `matches` (RE2) / `startsWith` |
| [`stats.md`](./stats.md) | Directory reconnaissance — `file-search-on stats` aggregates a content-type histogram with totals |
| [`group-by.md`](./group-by.md) | Stats bucketed by any attribute — `--group-by camera_make`, `--group-by language`, `--group-by taken_at_year`, etc. |
| [`read-lines.md`](./read-lines.md) | Print a specific line range from a file — `file-search-on lines <path> --start N --end M` |
| [`duplicates.md`](./duplicates.md) | Find byte-identical files via sha256 — `file-search-on duplicates [--min-size N] [-d DIR ...]` |
| [`find-matches.md`](./find-matches.md) | Line-level regex matching with context — `file-search-on find-matches '<re>' --expr 'is_source' -C 2` |

Every recipe is a complete `file-search-on '<expr>'` invocation that you can paste and run. Most include a few variations and useful output-format snippets.

For the canonical, up-to-date attribute list run `file-search-on --list`. For the full attribute reference see the [README](../README.md#available-attributes).
