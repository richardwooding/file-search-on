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
| [`data.md`](./data.md) | JSON / CSV / TSV / XML — `csv_columns`, `json_kind`, `root_element` |
| [`text.md`](./text.md) | Plain text and HTML — `line_count`, `word_count`, long-line caps |
| [`archives.md`](./archives.md) | ZIP / TAR / TAR.GZ / GZIP — Downloads triage, ZIP-bomb detection, compression ratios |
| [`binaries.md`](./binaries.md) | ELF / Mach-O / PE — architecture filtering, static-vs-dynamic, stripped triage, rogue `.exe` hunting |
| [`email.md`](./email.md) | `.eml` / `.mbox` — inbox triage, find emails by sender / subject / date, mbox archive sniffing |
| [`source-code.md`](./source-code.md) | Go / Python / JS / TS / Rust / C / C++ / Java / Ruby / Swift / Kotlin / Shell / Lua / Elixir / Clojure / Haskell / OCaml / Zig — LOC counts, language filtering, comment density |
| [`cookbook.md`](./cookbook.md) | Cross-family queries, output-format pipelines, integration with find / jq / ffmpeg / rga |
| [`fuzzy-search.md`](./fuzzy-search.md) | Fuzzy / phonetic / n-gram similarity matching with `levenshtein`, `soundex`, `ngrams`, `ngram_similarity` |

Every recipe is a complete `file-search-on '<expr>'` invocation that you can paste and run. Most include a few variations and useful output-format snippets.

For the canonical, up-to-date attribute list run `file-search-on --list`. For the full attribute reference see the [README](../README.md#available-attributes).
