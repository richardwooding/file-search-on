# CEL vocabulary

Reference for the CEL expression surface used by the `expr` / `rank` inputs across every walking tool. Call `list_attributes` at runtime for the canonical (potentially newer) schema; this file is the agent-facing summary.

## Contents

- Operators and literals
- Family `is_*` predicates (use directly without `list_attributes`)
- Exact-name `is_*` predicates
- Specialised families (umbrellas + per-format flags)
- Forensic / state predicates
- Git-aware attributes (require `with_git: true`)
- Common attributes (every file)
- Attributes by content family (headline ones)
- Built-in functions
- Recipes for composition

## Operators and literals

CEL syntax is roughly Go-like: `&&` / `||` / `!`, `==` / `!=`, `<` / `<=` / `>` / `>=`, `+` for string concat and numeric add, `in` for list membership, ternary `cond ? a : b`. String literals are double-quoted with `\\` escapes. Lists are `[...]`; maps are `{"k": v}`. Timestamps are `timestamp("2025-01-01T00:00:00Z")`. Durations are `duration("24h")`.

Useful idioms:

- `"longread" in tags` — list membership
- `tags.exists(t, t.startsWith("draft-"))` — exists macro on lists
- `frontmatter.summary != ""` — map access for markdown front-matter keys
- `gps_lat > -34.1 && gps_lat < -33.7 && gps_lon > 18.3 && gps_lon < 18.7` — bbox filter

## Family `is_*` predicates

True for every file in the family — agents can use these directly without calling `list_attributes` first.

- `is_markdown` — `.md`, `.markdown`
- `is_pdf`
- `is_html` — `.html`, `.htm`
- `is_xml`
- `is_json` — also fires for `package.json` / `package-lock.json`
- `is_yaml` — `.yaml`, `.yml`
- `is_toml` — also fires for `Cargo.toml` / `Cargo.lock`
- `is_csv` — `.csv`, `.tsv`
- `is_text` — plain text + logs + `requirements.txt` / `LICENSE` / `CHANGELOG` / `CONTRIBUTING`
- `is_image` — `.jpg`, `.png`, `.gif`, `.tif`, `.heic`, `.webp`, `.bmp`, `.svg`, RAW formats…
- `is_audio` — `.mp3`, `.m4a`, `.flac`, `.ogg`, `.wav`
- `is_video` — `.mp4`, `.mov`, `.m4v`, `.mkv`, `.webm`, `.avi`
- `is_office` — `.docx`, `.xlsx`, `.pptx`, `.odt`
- `is_epub`
- `is_archive` — `.zip`, `.tar`, `.tar.gz`, `.gz`
- `is_binary` — ELF / Mach-O / PE compiled binaries
- `is_email` — `.eml`, `.mbox`
- `is_source` — Go / Python / JS / TS / Rust / C / C++ / Java / Ruby / Swift / Kotlin / Scala / Shell / Lua / Elixir / Clojure / Haskell / OCaml / Zig
- `is_notebook` — Jupyter `.ipynb`, Apache Zeppelin `.zpln`

## Exact-name `is_*` predicates

Matched by filename. Both the per-type predicate AND the family predicate fire — `package.json` is both `is_node_manifest` and `is_manifest` (and `is_json`).

- Build: `is_dockerfile` / `is_makefile` / `is_justfile` / `is_rakefile` — all also `is_build`
- Repo metadata: `is_license` / `is_changelog` / `is_contributing` / `is_codeowners` — all also `is_repo_meta`
- Ignore: `is_gitignore` / `is_dockerignore` — all also `is_ignore`
- Manifest: `is_gomod` (parses `module` + `go_version`) / `is_node_manifest` / `is_cargo_manifest` / `is_pipfile` / `is_python_reqs` / `is_gemfile` — all also `is_manifest`
- Platform: `is_procfile` / `is_vagrantfile` — both also `is_platform`

## Specialised families

Each umbrella + per-format flag both fire.

- **Disk images** — `is_dmg`, `is_iso`, `is_vhd`, `is_vhdx`, `is_vmdk`, `is_qcow2`, `is_wim` — all also `is_disk_image`
- **Install packages** — `is_pkg`, `is_deb`, `is_rpm`, `is_appimage` — all also `is_install_package`
- **VM bytecode** — `is_class` (Java), `is_pyc` (Python), `is_wasm` (WebAssembly) — all also `is_bytecode`
- **Science data** — `is_fits`, `is_votable`, `is_hdf5`, `is_pds3`, `is_pds4`, `is_cdf` — all also `is_science_data`; `is_pds` is the umbrella over PDS3+4
- **Database** — `is_sqlite` (+ `is_sqlite_wal` / `is_sqlite_shm` sidecars). Only `is_sqlite` fires `is_database`
- **Browser bookmarks** — `is_chromium_bookmarks`, `is_safari_bookmarks` — both also `is_bookmark_file`
- **Chat exports** — `is_slack_export`, `is_discord_export`, `is_signal_export` — all also `is_chat_export`
- **Fonts** — `is_ttf`, `is_otf`, `is_font_collection`, `is_woff`, `is_woff2` — all also `is_font`; trait predicates `is_variable_font` / `is_color_font` / `is_monospace_font` / `is_italic_font` / `is_bold_font`
- **RAW photos** — `is_cr2`, `is_cr3`, `is_nef`, `is_arw`, `is_dng`, `is_raf`, `is_orf`, `is_rw2` — all also `is_raw_photo` + `is_image`
- **3D models** — `is_stl`, `is_obj`, `is_gltf` — all also `is_3d_model`
- **OS metadata** — `is_ds_store` / `is_localized` (macOS), `is_thumbs_db` / `is_desktop_ini` (Windows), `is_kde_directory` (Linux). Family umbrellas: `is_macos_metadata` / `is_windows_metadata` / `is_linux_metadata`, cross-OS `is_system_metadata`. `is_plist` is the Apple property-list type (binary + XML)

## Forensic / state predicates

Some require a tool opt-in to populate:

- `is_symlink`, `is_broken_symlink`, `target_path` — filesystem-level symlink awareness; populated regardless of `follow_symlinks`
- `is_test_file` — source files matching per-language test convention (`*_test.go`, `test_*.py`, `*.test.ts`, …)
- `is_generated_code` — source files emitted by a codegen tool (protoc / mockery / easyjson / `//go:generate` / `DO NOT EDIT` headers and friends). Detected by scanning the first ~20 lines for codegen markers across 14 languages
- `is_btime_anomaly` — `created_at` > `mod_time` (file placed after being modified elsewhere)
- `is_disguised` — magic disagrees with extension. Requires `check_disguised: true`
- `is_known_good` / `is_known_bad` — MD5 / SHA1 / SHA256 in the loaded allowlist / denylist. Requires `compute_hashes: true` + `hash_allowlist_path` / `hash_denylist_path`
- `is_quarantined`, `is_xattr_rich`, `quarantine_source_url`, `finder_tags`, … — Darwin-only. Requires `with_xattrs: true`
- `is_codesigned`, `is_apple_signed`, `is_third_party_signed`, `codesign_identifier`, `codesign_team_id`, `entitlements`, … — Mach-O code signature
- `is_live_photo`, `is_live_photo_video`, `live_photo_video_path` — HEIC + sibling MOV pairing

## Git-aware attributes

Require the per-call `with_git: true` opt-in. The MCP server keeps a HEAD-sha-validated cache (`gitmeta.Pool`) so the second `with_git: true` call against the same repo is sub-10ms; the first call after process start or after a `git commit` rebuilds. Auto-enables when `expr`, `sort_by`, or `rank` references any `git_*` attribute (`celexpr.NeedsGit`). Files outside a git tree get the zero values; the `recent_commits` preset will silently return nothing on a non-repo. The CLI counterpart is `--with-git`.

- `is_git_tracked` — file is in `git ls-files`
- `is_git_ignored` — file matches a `.gitignore` rule (via `git ls-files --ignored --exclude-standard`)
- `git_last_commit_time` — timestamp of the most recent commit touching this file (also a sort key)
- `git_first_seen` — timestamp the file was first committed (also a sort key)
- `git_commit_count` — int64 commit count for this file (also a sort key — see #299 fix for the regression where this used to silently no-op)
- `git_last_commit_author` — author name on the most recent commit
- `git_last_commit_subject` — first-line subject of the most recent commit

Composite predicates built from these (no special opt-in beyond `with_git`):

- `is_source && !is_git_tracked && !is_git_ignored` — "did I forget to commit?" (the `untracked_code` preset)
- `is_source && is_git_tracked && !is_test_file && !is_generated_code` — "production code" (the `prod_code` preset)
- `is_git_tracked && git_commit_count > 0` sorted by `git_commit_count desc` — churn / refactor candidates across all tracked files (source, docs, config), the `hot_files` preset

## Common attributes (every file)

- `name` — basename
- `path` — full path
- `dir` — parent dir
- `ext` — extension (with leading dot, lowercased)
- `size` — bytes
- `content_type` — detected (e.g. `image/jpeg`, `source/go`)
- `mod_time` — filesystem mtime (timestamp)
- `created_at` — birth time / btime (timestamp; empty when the FS doesn't track it)
- `metadata_changed_at` — ctime (timestamp)
- `title`, `author`, `language` — surface across many families (PDF / office / image EXIF artist / audio tag / front-matter title)

## Attributes by content family

Headline attributes per family — call `list_attributes` for the full catalogue including the long-tail ones.

**Markdown** — `word_count`, `line_count`, `tags` (list), `categories` (list), `draft` (bool), `date` (timestamp), `frontmatter` (map), `frontmatter_format` ("yaml"/"toml"/"json")

**Documents** (PDF / EPUB / office) — `page_count`, `language`, `title`, `author`. Office adds Dublin Core fields via the `office/*` family.

**Data** (JSON / YAML / CSV / XML) — `json_kind` ("object"/"array"), `yaml_kind` + `yaml_document_count`, `csv_columns` (list), `root_element`, `column_count`

**Manifests** — `module`, `go_version` (gomod); other manifest types currently surface as detection-only

**Images** — `img_width`, `img_height`, `camera_make`, `camera_model`, `lens`, `taken_at` (timestamp), `iso`, `focal_length`, `f_stop`, `exposure_time`, `gps_lat`, `gps_lon`, `orientation`. RAW adds `raw_kind`, `raw_vendor`.

**Audio** — `artist`, `album`, `album_artist`, `composer`, `year`, `track`, `genre`, `duration` (seconds), `bitrate`, `nominal_bitrate`, `sample_rate`, `channels`, `bit_depth`, `replaygain_track_gain`, `replaygain_album_gain`

**Video** — `video_codec`, `audio_codec`, `video_width`, `video_height`, `frame_rate`, `duration`, `rotation`, `color_primaries`, `color_transfer`, `is_hdr`, `subtitles` (list)

**Archives** — `entry_count`, `uncompressed_size`, `top_level_entries` (list), `has_root_dir`

**Binaries** — `architectures` (list), `bitness`, `binary_format` (`elf`/`mach-o`/`pe`/`mach-o-universal`), `binary_type` (`executable`/`shared-library`/…), `is_dynamically_linked`, `is_stripped`, `entry_point`. Mach-O code-signature subset listed under Forensic above.

**Email** — `email_to` (list), `email_cc` (list), `email_message_id`, `email_in_reply_to`, `sent_at` (timestamp), `attachment_count`, `email_count` (mbox)

**Source code** — `language`, `line_count`, `loc`, `comment_loc`, `blank_loc`, `function_count`, `function_names` (list), `import_count`, `import_paths` (list), `type_names` (list), `is_test_file` (bool), `is_generated_code` (bool). The `profile: "code"` search input skips non-source per-format parsing for ~5–10× speedup on monorepos.

**Notebooks** — `cell_count`, `code_cell_count`, `markdown_cell_count`, `kernel`, `language`, `title`

**Disk images** — `disk_image_format`, `virtual_size`, `disk_type`, `volume_label`, `disk_image_created_at`, `cluster_bits`, `is_encrypted`, `image_count`

**Install packages** — `package_format`, `package_name`, `package_version`, `package_release`, `package_arch`, `package_kind`, `appimage_version`

**Bytecode** — `bytecode_format`, `runtime_version`, `class_name`, `super_class`, `interfaces` (list), `method_count`, `field_count`, `access_flags` (list), `python_version`, `source_mtime`, `wasm_version`, `section_count`, `import_count`, `export_count`

**Fonts** — `font_format`, `font_outline_kind`, `font_family`, `font_subfamily`, `font_full_name`, `font_version`, `font_weight`, `font_width`, `font_axes` (list, for variable fonts), `font_axis_count`, `font_glyph_count`, `font_designer`, `font_manufacturer`, `font_license`

**Science data** — Per format: `science_format`, `telescope`, `instrument`, `object`, `observer`, `date_obs` (timestamp), `exptime`, `filter`, `airmass`, `ra`, `dec`, `bitpix`, `naxis`, `naxis1`, `naxis2`, `hdu_count`, `fits_kind`, `votable_version`, `table_count`, `total_rows`, `field_names` (list), `pds_version`, `mission_name`, `spacecraft_name`, `instrument_name`, `target_name`, `product_id`, `start_time`, `cdf_version`, `variable_count`, `attribute_count`

**Database (SQLite)** — `database_format`, `sqlite_page_size`, `sqlite_format_version`, `sqlite_page_count`, `sqlite_schema_version`, `sqlite_user_version`, `sqlite_application_id`, `sqlite_application_name`, `sqlite_table_names` (list), `sqlite_fts_table_count`, `sqlite_fts_table_names`. WAL sidecars expose `sqlite_wal_*`.

**Apple property lists** — `plist_format`, `plist_root_kind`, `plist_kind`, `plist_bundle_identifier`, `plist_bundle_name`, `plist_bundle_version`, `plist_executable`, `plist_min_os_version`, `plist_label`, `plist_program`, `plist_program_arguments` (list), `plist_run_at_load`, `plist_keep_alive`

**Browser bookmarks** — `bookmark_count`, `bookmark_folder_count`, `bookmark_folders` (list), `bookmark_urls` (list), `bookmark_titles` (list), `browser_vendor`, `bookmark_profile`

**Chat exports** — `chat_message_count`, `chat_participants` (list), `chat_channel`, `chat_workspace`, `chat_start_at`, `chat_end_at`

**3D models** — `model3d_format`, `vertex_count`, `face_count`, `has_normals`, `has_textures`, `materials` (list), `bounding_box` (list)

**OCR / body** — `body` (when `include_body` or `ocr_images`), `ocr_confidence`, `ocr_language`, `ocr_provider`

**Hashes / fingerprints** — `md5`, `sha1`, `sha256` (require `compute_hashes`), `phash` (require `with_phash`), `similarity` (semantic search)

**Project context** — `project_types` (list), `project_type` (first match), `is_static_site` — require `resolve_projects: true`

**Forensic xattrs** (Darwin, require `with_xattrs`) — `xattr_keys` (list), `xattr_count`, `quarantine_agent`, `quarantine_event_id`, `quarantine_source_url`, `quarantine_referrer_url`, `quarantine_download_date`, `quarantine_user_approved`, `finder_tags` (list), `finder_color`, `has_finder_comment`

## Built-in functions

**String methods** (CEL built-ins on every string variable, including `body`):

- `s.contains(substring)` — bool
- `s.matches(re)` — RE2 regex
- `s.startsWith(prefix)`, `s.endsWith(suffix)`
- `size(s)` — length in bytes

**Fuzzy / phonetic**:

- `levenshtein(a, b) -> int` — Damerau-style edit distance
- `soundex(s) -> string` — phonetic encoding
- `ngrams(s, n) -> list<string>` — n-gram decomposition
- `ngram_similarity(a, b, n) -> double` — n-gram Jaccard similarity (0..1)

**Geo**:

- `point_in_polygon(lat, lon, polygon) -> bool` — polygon is a flat `[lat0, lon0, lat1, lon1, …]` list

**Image similarity**:

- `image_similar_to(phash, ref_path, threshold) -> bool` — perceptual-hash Hamming distance ≤ `(1 - threshold) × 64`. Auto-enables `with_phash` when referenced in `expr`.

**Secret detection** (require `include_body: true`):

- `has_secrets(body) -> bool` — true if any AWS / GitHub / Stripe / Twilio / Slack / GCP / Azure / SSH-private-key / JWT / Bearer-token / Stripe-secret-key pattern matches the body
- `secret_kinds(body) -> list<string>` — returns the sorted list of matched kind names (e.g. `["aws-access-key", "github-pat"]`)

## Recipes for composition

A few non-obvious patterns:

- **Compose `rank` with semantic similarity**: `"rank": "similarity * 0.7 + (mod_time > timestamp(\"2025-01-01T00:00:00Z\") ? 0.3 : 0.0)"` — re-rank semantic results by recency.
- **Polygon GPS filter** instead of bbox: `"expr": "is_image && point_in_polygon(gps_lat, gps_lon, [-33.96, 18.41, -33.93, 18.41, -33.93, 18.45, -33.96, 18.45])"`.
- **Fuzzy name match** on potentially misspelled tags: `"expr": "is_audio && levenshtein(artist, \"Radiohead\") <= 2"`.
- **Time-bucketed photos**: pass `group_by: "taken_at_month"` to `stats` with `expr: "is_image"`.
- **Find Live Photos that lost their video**: `"expr": "is_live_photo && !is_live_photo_video"`.
