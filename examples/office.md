# Recipes — Office documents

Office content types: `office/docx`, `office/xlsx`, `office/pptx`, `office/odt`. Umbrella boolean `is_office`.

All four are zip-based containers with metadata stored as Dublin Core elements (`dc:title`, `dc:creator`, `dc:language`). DOCX/XLSX/PPTX read from `docProps/core.xml`; ODT reads from `meta.xml`. Same parser handles all four since the inner element names are identical.

## By format

```sh
file-search-on 'is_office && content_type == "office/docx"' -d ~/Documents
file-search-on 'is_office && content_type == "office/xlsx"' -d ~/Documents
file-search-on 'is_office && content_type == "office/pptx"' -d ~/Documents
file-search-on 'is_office && content_type == "office/odt"' -d ~/Documents
```

## Title

```sh
file-search-on 'is_office && title == "Quarterly Report"'
file-search-on 'is_office && title.contains("Draft")'
file-search-on 'is_office && title.startsWith("2024")'
```

Untitled documents (often defaults from templates that nobody filled in):

```sh
file-search-on 'is_office && title == ""'
```

## Author

```sh
file-search-on 'is_office && author == "Jane Doe"'
file-search-on 'is_office && author.contains("Smith")'
```

Anonymous documents:

```sh
file-search-on 'is_office && author == ""'
```

## Language

`language` is cross-cutting — same variable as Markdown / EPUB / HTML / PDF.

```sh
file-search-on 'is_office && language == "en-GB"'        # British English
file-search-on 'is_office && language == "fr"'           # French
file-search-on 'is_office && language.startsWith("en")'  # any English variant
```

## Combined queries

A compliance audit — find all DOCX/XLSX without an author:

```sh
file-search-on 'is_office && (content_type == "office/docx" || content_type == "office/xlsx") && author == ""'
```

A "draft" cleanup — find office files whose title contains "Draft" or "Copy":

```sh
file-search-on 'is_office && (title.contains("Draft") || title.contains("Copy"))'
```

Find documents from a specific author across all office formats:

```sh
file-search-on 'is_office && author == "Jane Doe"' --format '{{.Path}}\t{{.ContentType}}\t{{.Title}}'
```

## Useful output formats

```sh
# Sorted listing of all spreadsheets with title and author
file-search-on 'is_office && content_type == "office/xlsx"' --format '{{.Path}}\t{{.Title}}\t{{.Author}}' | sort

# JSON for analytics — frequency by author
file-search-on 'is_office' -o json | jq -r '.author // "(unknown)"' | sort | uniq -c | sort -rn
```

## Body-content search

Office documents support the `body` CEL variable when you pass `--body` (CLI) / `include_body: true` (MCP). The extractor walks the ZIP envelope and pulls plain text out of the format's content XML — `word/document.xml` for DOCX, `xl/sharedStrings.xml` + inline-string cells from `xl/worksheets/sheet*.xml` for XLSX, `ppt/slides/slide*.xml` for PPTX, `content.xml` for ODT — strips styling, joins with newlines, and surfaces the result as the `body` string.

```sh
# Every spreadsheet that mentions "Q3 revenue" in a cell
file-search-on 'is_office && content_type == "office/xlsx" && body.contains("Q3 revenue")' --body -d ~/Documents

# DOCX drafts mentioning a competitor
file-search-on 'is_office && content_type == "office/docx" && body.matches("(?i)competitor")' --body

# PPTX decks containing a specific phrase
file-search-on 'is_office && content_type == "office/pptx" && body.contains("user research")' --body

# Cross-format search across an entire Documents tree
file-search-on 'is_office && body.contains("invoice")' --body -d ~/Documents
```

The body cap (default 1 MiB, override via `--body-max-bytes`) applies to the EXTRACTED text, not the raw file size — a 50 MB PPTX with sparse text still reads cheaply.

## What's NOT covered

- **PDF text extraction** — different code path; tracked separately.
- **Track changes / revisions** — not surfaced.
- **Custom document properties** (the `app.xml` / custom properties beyond core.xml) — not surfaced.
- **Embedded images / OLE objects** — only text content is extracted.
- **DOCX comments / footnotes / endnotes** — only the body paragraphs are walked today. Add `word/comments.xml` to the entry list in `internal/content/body.go` if needed.
- **ODT `<text:h>` (heading) elements** — only `<text:p>` is walked today; headings extract empty. Real documents have headings inside paragraphs OR `<text:h>` standalone. Trade-off for code simplicity; address if real corpora need it.

For richer text extraction (PDFs, embedded archives, OCR'd images), pipe the matched paths to `ripgrep-all` (`rga`) or `pandoc`.
