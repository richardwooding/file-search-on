# Recipes — Organize by query

`file-search-on organize` builds a **templated symlink (or copy) tree** from search results — a virtual organized *view* of a flat directory without moving or touching the originals. Git-safe, undo-safe (delete the view, the source is untouched), and cheap (symlinks are free).

```sh
file-search-on organize '<expr>' --link-into '<template>' [flags] -d <root>
```

## The `--link-into` template

The destination is a `{token}` brace template. Literal `/` are real path separators; `{token}` placeholders are substituted per file. Token values are sanitised (a `/` inside a value becomes `-`, empty values become `unknown`) so a stray separator can't inject extra nesting. A leading `~/` expands to your home directory.

**Path / file tokens:**

| Token | Value |
|---|---|
| `{basename}` | Full filename (`IMG_1234.HEIC`) |
| `{stem}` | Filename without extension (`IMG_1234`) |
| `{ext}` | Extension without dot (`heic`); `none` when extensionless |
| `{dir}` | Parent directory's basename |
| `{content_type}` | e.g. `image-heic` (slash sanitised) |
| `{size}` | Byte size |

**Time-bucket tokens** (`<attr>_<year|month|day>`): `{mtime_year}`, `{mtime_month}`, `{mtime_day}`, plus `taken_at_*`, `created_at_*`, `metadata_changed_at_*`, `sent_at_*`, `date_*`. Empty when the file has no such timestamp → `unknown`.

**Any attribute by its CEL name:** `{camera_make}`, `{camera_model}`, `{raw_vendor}`, `{language}`, `{ocr_language}`, `{license_id}`, `{project_type}`, … — anything that shows up in `file-search-on --list` resolves here.

## Photo libraries

```sh
# RAW masters sorted by vendor then shoot-year
file-search-on organize 'is_raw_photo' \
  --link-into '~/sorted/raw/{raw_vendor}/{taken_at_year}/{basename}' -d ~/Pictures

# Every photo grouped by the camera that took it
file-search-on organize 'is_image' \
  --link-into '~/by-camera/{camera_make}/{camera_model}/{basename}' -d ~/Pictures

# Photos by the year + month they were SHOT (taken_at), not modified
file-search-on organize 'is_image && taken_at_year != ""' \
  --link-into '~/timeline/{taken_at_year}/{taken_at_month}/{basename}' -d ~/Pictures
```

> Note `{mtime_year}` is the file's modification time; `{taken_at_year}` is the EXIF capture date. For "when did I shoot this?" use `taken_at_*`.

## Code + documents

```sh
# Source files bucketed by language
file-search-on organize 'is_source' \
  --link-into '~/code-by-lang/{language}/{basename}' -d ~/Code

# Markdown drafts vs published, by front-matter draft flag
file-search-on organize 'is_markdown' \
  --link-into '~/posts/{frontmatter_format}/{basename}' -d ~/blog
```

## Screenshots by detected language (with OCR)

```sh
file-search-on organize 'is_image' --ocr \
  --link-into '~/screenshots-by-lang/{ocr_language}/{basename}' -d ~/Desktop
```

`--ocr` runs the OCR provider so `{ocr_language}` resolves (macOS Vision today; no-op on platforms without a provider).

## Preview first — `--dry-run`

Always safe to preview. Prints the planned `source -> target` actions and a summary, creating nothing:

```sh
file-search-on organize 'is_raw_photo' \
  --link-into '~/sorted/{raw_vendor}/{basename}' -d ~/Pictures --dry-run
```

```
[dry-run] /Users/me/Pictures/a.cr2 -> /Users/me/sorted/canon/a.cr2
[dry-run] /Users/me/Pictures/b.nef -> /Users/me/sorted/nikon/b.nef
would link 2, skipped 0, failed 0
```

## Symlink vs copy

The default creates **symlinks** (absolute, so they resolve regardless of where the view lives). Pass `--copy-instead` to materialise real file copies — useful when the view needs to survive the source being moved, or to hand someone a self-contained folder:

```sh
file-search-on organize 'is_image && taken_at_year == "2024"' \
  --link-into '~/2024-export/{basename}' --copy-instead -d ~/Pictures
```

## Collision handling — `--on-conflict`

When two source files render to the same target path (e.g. same basename from different folders):

| Mode | Behaviour |
|---|---|
| `skip` (default) | Leave the existing target, skip the new one |
| `overwrite` | Replace the existing target |
| `number` | Append ` (1)`, ` (2)`, … before the extension |

```sh
# Keep every collision as a numbered variant
file-search-on organize 'is_image' \
  --link-into '~/flat/{basename}' --on-conflict number -d ~/Pictures/nested
```

## Composes with the walk flags

`organize` shares the walk machinery, so the usual pruning / filtering applies: `--exclude`, `--respect-gitignore`, `--prune-build-artefacts`, `--follow-symlinks`, `--index-path` (cache the attribute parse across repeat organizes), `-w` workers, and `--body` (only needed if the selecting expression uses `body.contains` / `has_secrets` / etc.).

```sh
file-search-on organize 'is_source && has_secrets(body)' --body \
  --link-into '~/quarantine/{basename}' --prune-build-artefacts -d ~/Code
```

## Known limitations

- **Read-only view.** The symlink/copy tree is a one-way projection. Editing through a symlink edits the original (expected); editing a `--copy-instead` file does NOT propagate back. There's no sync.
- **No Finder-tag / xattr-based organisation.** Tokens resolve from parsed attributes, not macOS tags. (Tag-based organising is a possible follow-up.)
- **Case-insensitive filesystems.** On default macOS / Windows volumes, `{camera_make}` values differing only in case (`Canon` vs `canon`) collapse into one directory. Usually what you want; noted for completeness.
- **`organize` is CLI-only.** It's a filesystem-mutating command, intentionally not exposed as an MCP tool.
