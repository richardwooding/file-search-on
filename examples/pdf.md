# Recipes — PDF

PDF content type: `pdf` (extension `.pdf`, magic `%PDF`). Umbrella boolean `is_pdf`.

Pure-Go parser ([`github.com/ledongthuc/pdf`](https://github.com/ledongthuc/pdf)) — no CGO, no external services. Metadata pulled from the `/Info` dictionary plus an XMP fallback for `language`; body text pulled per page from content streams against the document's pre-cached font / ToUnicode CMap.

Out of scope: OCR for scanned/image-only PDFs, encrypted-PDF password handling, layout-aware extraction (multi-column research papers, tables), PDF form fields and annotations. Caveats are listed at the bottom of this file.

## All-PDF triage

```sh
file-search-on 'is_pdf' -d ~/Documents
```

## Find by metadata

`page_count`, `title`, `author`, and `language` are populated for every PDF:

```sh
# PDFs longer than 10 pages
file-search-on 'is_pdf && page_count > 10' -d ~/Papers

# PDFs authored by a specific person (fuzzy fallback if the metadata is messy)
file-search-on 'is_pdf && author == "Jane Doe"' -d ~/Documents
file-search-on 'is_pdf && soundex(author) == soundex("Smith")' -d ~/Documents

# Title-based search — same `title` vocabulary as markdown / office / epub / email
file-search-on 'is_pdf && title.contains("invoice")' -d ~/Bills

# Only English-language PDFs (catalog /Lang, XMP fallback for the language)
file-search-on 'is_pdf && language.startsWith("en")' -d ~/Papers
```

## Sort + top-K

```sh
# 10 longest PDFs by page count
file-search-on 'is_pdf' -d ~/Papers --sort-by page_count --order desc --limit 10

# Most recent PDFs
file-search-on 'is_pdf' -d ~/Documents --sort-by mod_time --order desc --limit 20
```

## Body-content search

Pass `--body` (CLI) / `include_body: true` (MCP) and the `body` CEL variable carries the document text. The extractor walks every page in numeric order, decoding glyph-to-Unicode mappings against the page fonts. Page text is joined with a newline so `body.contains(...)` and `body.matches(...)` find content that spans pages.

```sh
# Find every PDF mentioning a topic
file-search-on 'is_pdf && body.contains("transformer architecture")' --body -d ~/Papers

# Combine metadata + body — papers by a specific author about a topic
file-search-on 'is_pdf && author.contains("Vaswani") && body.contains("attention")' --body -d ~/Papers

# Case-insensitive regex (CEL's matches uses RE2)
file-search-on 'is_pdf && body.matches("(?i)\\bGDPR\\b")' --body -d ~/Compliance

# Line-level hits with surrounding context (1 line before/after)
file-search-on find-matches '(?i)\bGDPR\b' --expr 'is_pdf' -d ~/Compliance -C 1
```

The 1 MiB body cap (`--body-max-bytes`) applies to extracted text, not raw bytes — a 50 MB PDF with sparse text reads cheaply; a dense academic paper may surface only the first ~100 pages of dense prose before the cap is reached.

## Audit: PDFs we can't search

Image-only PDFs (scanned documents, screenshots-as-PDF), encrypted PDFs, and severely malformed PDFs all surface as empty body. Use that as a feature — list candidates that need external OCR or password unlock:

```sh
# PDFs the extractor couldn't read — feed these to OCR or skip
file-search-on 'is_pdf && size(body) == 0' --body -d ~/Documents -o bare

# PDFs that DID extract — combine with size for "long PDFs we can search"
file-search-on 'is_pdf && size(body) > 1000 && page_count > 20' --body -d ~/Papers
```

The boundary is the document's content stream: a PDF whose visible text is rasterised pixels (a scanned book) has no text in its content stream and yields empty body, even though metadata (title / author / page_count) still surfaces. Pair `is_pdf && size(body) == 0` with `--sort-by page_count --order desc` to surface the biggest OCR candidates first.

## Caveats

- **OCR is out of scope.** Image-only / scanned PDFs return empty body. Pipe candidates to Tesseract or a cloud OCR service externally.
- **Encrypted PDFs return empty body.** The underlying parser's encrypted-PDF support is documented as weak, and there's no clean way to surface a password prompt through the MCP transport.
- **CJK / complex-script fidelity is best-effort.** ToUnicode CMap interpretation can be partial; `(cid:N)` glyph fallbacks are stripped automatically so they don't pollute search hits, but some sequences still come out garbled.
- **Layout-aware extraction is not done.** Multi-column research papers, tables, and footnotes flow into the body in reading order as the content stream presents them — which is often, but not always, the visual reading order. For "does this PDF mention X" this is fine; for structured table extraction you'll want a dedicated tool.
- **PDF form fields and annotations aren't extracted.** Body is page content stream text only.
