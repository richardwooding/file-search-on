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

## What's NOT covered

- **DOCX / PPTX content search** (text inside slides or paragraphs) — out of scope for v1; this is metadata-only.
- **XLSX cell content** — same.
- **Track changes / revisions** — not parsed.
- **Custom document properties** (the `app.xml` / custom properties beyond core.xml) — not surfaced.

For full-text search of office documents, pipe the matched paths to a tool like `ripgrep-all` (`rga`) or `pandoc`.
