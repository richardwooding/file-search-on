# Recipes — EPUB ebooks

EPUB content type: `epub` (extension `.epub`). Predicate: `is_epub`.

EPUBs are zip containers with a manifest at `META-INF/container.xml` pointing to the OPF rootfile, where Dublin Core metadata lives. The same `readDublinCore` scanner that handles DOCX/XLSX/PPTX/ODT also handles EPUB.

## Author and title

```sh
file-search-on 'is_epub && author == "Ursula K. Le Guin"' -d ~/Books
file-search-on 'is_epub && title.contains("Earthsea")'
```

Anonymous or untitled books (rare, but possible from poorly-prepared epubs):

```sh
file-search-on 'is_epub && author == ""'
file-search-on 'is_epub && title == ""'
```

## Language

`language` is cross-cutting. EPUB sets it via `<dc:language>` in the OPF.

```sh
file-search-on 'is_epub && language == "en"'           # English
file-search-on 'is_epub && language == "es"'           # Spanish
file-search-on 'is_epub && language.startsWith("en")'  # en, en-US, en-GB, etc.
```

Books with no language set:

```sh
file-search-on 'is_epub && language == ""'
```

## Combined queries

A reading-list curator's query — French-language sci-fi by a specific author:

```sh
file-search-on 'is_epub && language == "fr" && author == "Pierre Boulle"'
```

A library audit — books missing essential metadata:

```sh
file-search-on 'is_epub && (title == "" || author == "")'
```

A "find Le Guin's translated works" query:

```sh
file-search-on 'is_epub && author.contains("Le Guin") && language != "en"'
```

## File size as a proxy for length

EPUBs don't carry page count or word count metadata, so file size is the best available proxy:

```sh
file-search-on 'is_epub && size > 5000000'                    # > 5 MB (long novels with images)
file-search-on 'is_epub && size < 500000'                     # < 500 KB (short stories, novellas)
```

## Useful output formats

```sh
# A bibliographic summary
file-search-on 'is_epub' --format '{{.Title}} — {{.Author}} ({{.Language}})'

# JSON ready for Calibre import or similar
file-search-on 'is_epub' -o json | jq '{title, author, language, size}'

# All books by language, frequency-sorted
file-search-on 'is_epub' -o json | jq -r '.language // "(unset)"' | sort | uniq -c | sort -rn
```

## Body-content search

Pass `--body` (CLI) / `include_body: true` (MCP) and the `body` CEL variable carries the extracted chapter text. The extractor walks `META-INF/container.xml` → the OPF rootfile → spine order, opens each (X)HTML chapter, strips tags / `<script>` / `<style>`, and joins with paragraph breaks.

```sh
# Books mentioning "improbability drive" anywhere in their chapters
file-search-on 'is_epub && body.contains("improbability drive")' --body -d ~/Books

# Books that quote a specific phrase
file-search-on 'is_epub && body.matches("(?i)\\bDouglas Adams\\b")' --body

# Combine with metadata — only English-language sci-fi mentioning a theme
file-search-on 'is_epub && language.startsWith("en") && body.contains("dystop")' --body
```

The 1 MiB body cap (override via `--body-max-bytes`) applies to extracted text; the extractor stops mid-spine when the cap is reached, so an enormous ebook still reads cheaply.

## What's NOT covered

- **Series / volume metadata** (Calibre custom columns) — sometimes in the OPF but not standardised; not parsed.
- **Cover art** — not extracted (the OPF references it but we don't surface a path).
- **Reading progress** — that's a reader-side concept, not in the EPUB itself.
- **EPUB3 fixed-layout / SVG-only chapters** — text extractor returns empty for those; they're meant to be viewed, not searched.
- **DRM'd EPUBs** — out of scope; the ZIP is encrypted at the entry level for ADE/Adobe-DRM books, so the extractor sees garbage.

For text inside DRM-protected ebooks or other niche formats, an external converter (`pandoc`, Calibre's `ebook-convert`) remains the right tool.
