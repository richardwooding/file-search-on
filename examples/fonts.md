# Recipes — Fonts

Font files (`.ttf`, `.otf`, `.ttc`, `.otc`, `.woff`, `.woff2`) carry rich metadata in their sfnt tables — family, designer, weight, width, variable-font axes, embedding permissions, license. The parser surfaces them as typed CEL attributes. With no `is_font` ancestor in the project until v0.50, every designer's Mac and every web project's `assets/fonts/` was invisible to CEL queries beyond filename + size.

Content types under the `font/` family: `font/ttf`, `font/otf`, `font/collection` (TTC/OTC), `font/woff`, `font/woff2`.

The shared `font_family` and `font_designer` attributes also dual-surface to the cross-family `title` and `author` variables — `title.contains("Inter")` fires on fonts as well as documents. Matches the FITS-pattern shared vocabulary (`OBJECT → title`, `OBSERVER → author`).

## Find variable fonts

```sh
# Every variable font on the system
file-search-on 'is_variable_font' -d /System/Library/Fonts -d ~/Library/Fonts -d /Library/Fonts

# Variable fonts with a weight axis
file-search-on 'is_font && "wght" in font_axes'

# Variable fonts with an optical-size axis (display-vs-text optimized)
file-search-on 'is_font && "opsz" in font_axes'

# Variable fonts with 3+ axes (richer design space than wght+slnt)
file-search-on 'is_variable_font && font_axis_count >= 3'
```

## Weight + style filters

```sh
# Find all bold fonts (CSS-equivalent threshold)
file-search-on 'is_font && font_weight >= 700'

# Find italic fonts
file-search-on 'is_italic_font'

# Find programming fonts (monospace)
file-search-on 'is_monospace_font'

# Find colour fonts (Apple Color Emoji / Noto Color Emoji / etc.)
file-search-on 'is_color_font'

# Find fonts with both a weight and slant axis — proper italic VFs
file-search-on 'is_variable_font && "wght" in font_axes && ("slnt" in font_axes || "ital" in font_axes)'
```

## License audit

```sh
# Fonts WITHOUT a SIL Open Font License declaration
file-search-on 'is_font && !font_license.contains("SIL Open Font License")' -d ~/Code

# Fonts requiring permission to embed (legal-not-technical signal)
file-search-on 'is_font && font_embedding == "restricted"'

# Fonts where the vendor stamped a license URL
file-search-on 'is_font && font_license_url != ""'

# All licenses across a project — group_by gives a histogram
file-search-on stats 'is_font' -d ~/Code --group-by font_embedding
```

## Vendor and designer queries

```sh
# Find every Adobe font
file-search-on 'is_font && font_manufacturer.contains("Adobe")'

# Find fonts by a specific designer (cross-surfaces to author too)
file-search-on 'is_font && font_designer.contains("Frere-Jones")'
# Equivalent using cross-family vocabulary:
file-search-on 'is_font && author.contains("Frere-Jones")'

# Find every font in a family (cross-surfaces to title too)
file-search-on 'is_font && title.contains("Roboto")'
# Equivalent typed form:
file-search-on 'is_font && font_family == "Roboto"'
```

## Collection inventory

```sh
# Heavy collections — Apple's Helvetica.ttc has ~12 members
file-search-on 'is_font_collection && font_collection_count > 5'

# All families inside system collections
file-search-on 'is_font_collection' -d /System/Library/Fonts -o json | \
  jq -r '"\(.path)\t\(.font_collection_families | join(", "))"'

# OpenType collections specifically (CFF outlines)
file-search-on 'is_font_collection && font_format == "otc"'
```

## Unicode coverage

```sh
# Fonts covering CJK Unified Ideographs (large East Asian fonts)
file-search-on 'is_font && "CJK Unified Ideographs" in font_unicode_ranges'

# Fonts covering Arabic
file-search-on 'is_font && "Arabic" in font_unicode_ranges'

# Latin-only fonts (no extended scripts)
file-search-on 'is_font && "Basic Latin" in font_unicode_ranges && !("CJK Unified Ideographs" in font_unicode_ranges)'
```

## Web / WOFF queries

```sh
# Every WOFF1 file in a frontend project (attrs extract fully)
file-search-on 'is_woff' -d ./assets/fonts

# Every WOFF2 file (v1 detect-only — attrs limited to header)
file-search-on 'is_woff2' -d ./assets/fonts

# Compressed-vs-uncompressed ratio for WOFF2 collections
file-search-on 'is_woff2' -d ./assets/fonts -o json | \
  jq -r '"\(.path)\t\(.woff2_total_compressed_size)/\(.woff2_total_sfnt_size)"'
```

## Group-by + sort

```sh
# All fonts on the system grouped by family
file-search-on stats 'is_font' --group-by font_family -d /System/Library/Fonts

# Largest fonts by glyph count
file-search-on 'is_font' --sort font_glyph_count --order desc --limit 10

# Heavyweight fonts (Black / ExtraBold) sorted by weight desc
file-search-on 'is_font && font_weight >= 800' --sort font_weight --order desc
```

## Known limitations

- **WOFF2 is detect-only in v1.** `.woff2` files surface `is_woff2`, `font_format = "woff2"`, and the header byte counts (`woff2_total_sfnt_size`, `woff2_total_compressed_size`), but `font_family` / `font_designer` / `font_weight` / `font_axes` stay UNSET. Full WOFF2 attribute extraction requires brotli decompression plus the WOFF2 transformed-table encoding — tracked as a follow-up issue. Web designers querying `~/Project/assets/fonts/*.woff2` collections will see only the umbrella predicate in v1.
- **`font_embedding` is informational only.** The OS/2 `fsType` bits are a legal declaration by the foundry, not a technical enforcement mechanism. A font marked `restricted` is no harder to embed than one marked `installable` — agents auditing fonts on a deployed codebase should treat the field as documentation.
- **TTC `font_family` is the primary (first) member.** Collections carry multiple families (e.g. Helvetica + Helvetica Neue can coexist). The full list lives in `font_collection_families`; query that when family is ambiguous.
- **Name-table encoding priority is opinionated.** The decoder picks Windows Unicode English (3, 1, 0x409) when available, falling through to any Windows Unicode encoding, then Mac Roman English, then any other Windows Unicode, then the first record. Non-English fonts with only their native-language name records will surface that name as `font_family` — generally what users want, but worth knowing.
- **Cross-family pollution.** `title.contains("Inter")` fires on fonts AND documents. Same trade-off as the FITS work (`OBJECT → title`). When you want font-only queries, use `is_font && font_family == "..."` explicitly.
- **No font-fingerprint stability.** Two builds of the same font (e.g. monthly Inter releases) will differ in `font_revision` and possibly in glyph count — they're not stable identifiers. Use file hashes (`--with-hashes`) for fingerprinting.
- **Type 1 / EOT / BDF / PCF / SVG fonts are out of scope** for v1. Type 1 PostScript fonts (PFB/PFA) are extremely rare on modern systems; EOT is a Microsoft IE legacy format; BDF/PCF are X11 bitmap formats; SVG fonts are XML and already detect as `xml`.
