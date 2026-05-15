# Recipes — Search inside archives

Two subcommands / MCP tools let you query archives **without extracting**: `archive-contents` for per-entry CEL filtering, `archive-read` for fetching a single entry's bytes.

Supported formats: ZIP (incl. JAR / WAR / EAR), TAR, TAR.GZ, GZIP. The per-entry walker treats archive entries as "virtual files" — they go through the same content-type detection + per-format attribute extraction + CEL evaluation as real files. Every `is_X` predicate and per-family attribute that works on the filesystem works inside archives.

## When to use which

| Question | Tool |
|---|---|
| "Does this tarball contain `Dockerfile`?" | `archive-contents <archive> --glob Dockerfile` |
| "Find every Go file with loc > 200 inside this release tarball" | `archive-contents <archive> --expr 'is_source && language == "go" && loc > 200'` |
| "Read pyproject.toml out of source.tar.gz to check the Python version" | `archive-read <archive> pyproject.toml` |
| "Find every PDF inside this ZIP mentioning 'transformer'" | `archive-contents <archive> --expr 'is_pdf && body.contains("transformer")' --body` |

## `archive-contents` recipes

### Quick listing

```sh
file-search-on archive-contents ./release.tar.gz
file-search-on archive-contents ./project.zip --max 100
```

### Filter by name / pattern

```sh
# Cheap basename glob — runs BEFORE the per-entry attribute pass.
file-search-on archive-contents ./project.zip --glob '*.go'
file-search-on archive-contents ./project.zip --glob Dockerfile

# Filter via CEL — full vocabulary applies.
file-search-on archive-contents ./project.zip --expr 'is_dockerfile'
file-search-on archive-contents ./project.tar.gz --expr 'is_source && language == "python"'
file-search-on archive-contents ./node.zip --expr 'is_node_manifest'   # finds package.json + package-lock.json
```

### Filter by content (body)

Pass `--body` and the per-entry CEL gets the body string. Body extraction is supported for:

- **Text-shaped**: markdown, text, html, csv, json, xml, source/* (raw `io.LimitReader` read)
- **Structured-document**: PDF, DOCX, XLSX, PPTX, ODT, EPUB, RFC 5322 email (`.eml`), Unix mbox (`.mbox`) — each format's existing extractor runs against the in-memory entry bytes via the same singleFileFS the walker uses

```sh
# Find every Python file in any tarball mentioning "TODO"
file-search-on archive-contents ./src.tar.gz --expr 'is_source && language == "python" && body.contains("TODO")' --body

# Find every PDF inside a tarball mentioning a topic
file-search-on archive-contents ./papers.tar.gz --expr 'is_pdf && body.contains("transformer")' --body

# Find every DOCX inside a release ZIP mentioning competitor names
file-search-on archive-contents ./reports.zip --expr 'content_type == "office/docx" && body.matches("(?i)\\b(competitor1|competitor2)\\b")' --body

# Find emails inside an mbox-in-archive mentioning a specific invoice
file-search-on archive-contents ./mail-backup.tar.gz --expr 'is_email && body.contains("INV-2026-0042")' --body
```

`--body` **bypasses the entry-list cache** — bodies are large and aren't cached by design. Use it only when you need body content; metadata-only filters (`is_X`, `loc`, `language`, etc.) hit the cache on repeat calls.

The per-entry byte cap (`--entry-read-cap`, default 8 MiB) limits how much of each entry is loaded into memory. Structured-document bodies need their full file to extract — raise the cap for archives with large PDFs / office docs; lower for memory pressure on giant tree walks. Combine with `--include-attributes` to see the extracted body alongside the rest of the per-format attributes.

### Filter by per-family attributes

The per-entry attribute pass populates the same fields the top-level walker does:

```sh
# Big Go source files inside any tarball under ./releases
for archive in ./releases/*.tar.gz; do
  file-search-on archive-contents "$archive" --expr 'is_source && language == "go" && loc > 500'
done

# PDF entries inside a ZIP archive larger than 10 pages
file-search-on archive-contents ./report-bundle.zip --expr 'is_pdf && page_count > 10'

# Image entries inside a ZIP with EXIF camera metadata
file-search-on archive-contents ./photos.zip --expr 'is_image && camera_make != ""' --include-attributes
```

### Output formats

```sh
file-search-on archive-contents ./project.zip                    # tabular default
file-search-on archive-contents ./project.zip -o json            # JSON for piping to jq
file-search-on archive-contents ./project.zip --include-attributes -o json | jq '.entries[] | {name, content_type, loc: .attributes.loc}'
```

## `archive-read` recipes

```sh
# Print the file's bytes to stdout (raw — pipe wherever)
file-search-on archive-read ./source.tar.gz pyproject.toml | grep '^python ='

# Cap bytes (useful for huge log files inside archives)
file-search-on archive-read ./logs.tar.gz access.log --max-bytes 4096

# JSON envelope with metadata + content
file-search-on archive-read ./project.zip src/main.go -o json | jq
```

## Caching

The first call against an archive walks every entry and detects content types — for a 1000-entry tarball this is sub-second. Subsequent calls with `--index-path` consult the per-archive entry-list cache (keyed on the OUTER archive's `(size, mtime)`); cache-hit calls filter without opening the archive at all.

```sh
file-search-on archive-contents ./project.zip --expr 'is_source' --index-path ~/.fso.bbolt
# First call: walks the archive, populates cache. cache_hit=false.
# Second call (any expr, same --index-path, archive unchanged): cache_hit=true.
file-search-on archive-contents ./project.zip --expr 'is_dockerfile' --index-path ~/.fso.bbolt
```

Archives with > 10000 entries skip the cache (the encoded record list would blow the 256 KiB bbolt soft cap). Agents asking about huge archives pay the walk cost every time — usually that's still fast.

The MCP server's in-memory index applies automatically — no setup required.

## MCP

```json
{
  "name": "mcp__file-search-on__list_archive_contents",
  "arguments": {
    "path": "/Users/me/release.tar.gz",
    "expr": "is_source && language == \"go\" && loc > 200",
    "include_attributes": true,
    "max_entries": 50,
    "timeout_seconds": 30
  }
}
```

Returns `{entries: [{archive_path, name, display_path, size, content_type, attributes}], scanned_entries, matched_entries, cache_hit, truncated, cancelled, elapsed_seconds}`. Honours the same partial-result contract as `search` / `stats` / `find_duplicates`.

```json
{
  "name": "mcp__file-search-on__read_file_in_archive",
  "arguments": {
    "archive_path": "/Users/me/source.tar.gz",
    "entry_path": "pyproject.toml",
    "max_bytes": 8192
  }
}
```

Returns `{content, size, truncated, content_type, attributes, ...}`. UTF-8 entries surface via `content`; non-UTF-8 entries surface as base64 in `content_base64`.

## Caveats

- **Read-only.** Neither tool modifies archives — there's no "write entry into archive" surface.
- **No nested-archive recursion.** A ZIP inside a TAR doesn't get walked transitively. Caller chains two `archive-contents` calls explicitly.
- **Default `--entry-read-cap` is 8 MiB.** PDF / DOCX / EPUB / email bodies inside archives are extracted via the same per-format pipeline real-filesystem files use, but the entry's bytes need to fit in the cap. Raise the cap for archives containing huge documents; lower for memory pressure on large collections.
- **Project-type resolution doesn't fire inside archives.** "Which project does `package.json` inside this tarball belong to?" is meaningless without a filesystem layout.
- **TAR is sequential.** The walker reads entries in archive order; random access (`archive-read foo.tar bar.txt` when `bar.txt` is at the end) is O(n) over the archive's entries. ZIP / TAR.GZ behave the same way for the iteration walker. For repeat `archive-read` calls on the same archive, the entry-list cache helps but the per-entry seek doesn't.
- **7Z / RAR / BZIP2 / XZ are unsupported.** Each needs a third-party library — out of scope.
- **Entry-read cap defaults to 1 MiB.** Bytes beyond the cap are truncated for detection and body-attribute extraction. Set `--entry-read-cap` higher for archives with large files where you need the full body.
- **Cache invalidation is whole-archive.** Any change to the outer archive (size or mtime) invalidates the entire cached entry list. Two archives with identical content but different mtimes don't share cache entries — keyed on the path, not the content hash.
