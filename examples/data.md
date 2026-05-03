# Recipes — Data formats (JSON, CSV, TSV, XML)

Three structural file types with lightweight metadata extraction:

| Type | Predicate | Attributes |
| --- | --- | --- |
| JSON | `is_json` | `json_kind` (`"object"` / `"array"`) |
| CSV / TSV | `is_csv` | `column_count`, `csv_columns` |
| XML | `is_xml` | `root_element` |

Detection: extension first (`.json`, `.csv`, `.tsv`, `.xml`, `.xsl`, `.xslt`, `.xsd`, `.rss`, `.atom`), then magic bytes (`{`/`[` for JSON, `<?xml` for XML, no magic for CSV).

## JSON

Top-level shape — array vs object:

```sh
file-search-on 'is_json && json_kind == "array"' -d ./data       # NDJSON-style logs, lists
file-search-on 'is_json && json_kind == "object"' -d ./config    # config files, single records
```

Empty / malformed JSON (`json_kind == "unknown"`):

```sh
file-search-on 'is_json && json_kind == "unknown"'
```

## CSV / TSV

By column count:

```sh
file-search-on 'is_csv && column_count > 10'                     # wide tables
file-search-on 'is_csv && column_count == 1'                     # single-column lists
file-search-on 'is_csv && column_count >= 50'                    # often spreadsheet exports
```

By column name (the killer feature):

```sh
# CSVs that have a "revenue" column
file-search-on 'is_csv && csv_columns.exists(c, c == "revenue")'

# CSVs with both customer_id and email
file-search-on 'is_csv && csv_columns.exists(c, c == "customer_id") && csv_columns.exists(c, c == "email")'

# CSVs whose first column is named "id" (case-sensitive)
file-search-on 'is_csv && size(csv_columns) > 0 && csv_columns[0] == "id"'

# CSVs with a column whose name contains "date"
file-search-on 'is_csv && csv_columns.exists(c, c.contains("date"))'
```

The CSV parser uses Go's `encoding/csv` with `LazyQuotes = true`, so files with embedded commas in quoted fields (`"a,b",c,d` → 3 columns) are handled correctly.

By extension — CSV vs TSV:

```sh
file-search-on 'is_csv && ext == ".tsv"'
file-search-on 'is_csv && ext == ".csv"'
```

## XML

By root element — useful for distinguishing format families:

```sh
file-search-on 'is_xml && root_element == "rss"'                # RSS feeds
file-search-on 'is_xml && root_element == "feed"'               # Atom feeds
file-search-on 'is_xml && root_element == "svg"'                # SVG files (also caught by is_image)
file-search-on 'is_xml && root_element == "config"'             # generic config XML
file-search-on 'is_xml && root_element == "configuration"'      # Spring / .NET style
```

Find Atom or RSS together:

```sh
file-search-on 'is_xml && (root_element == "rss" || root_element == "feed")' -d ./feeds
```

## Combined queries

A data-quality audit — empty CSVs or tiny JSON files:

```sh
file-search-on '(is_csv && column_count == 0) || (is_json && size < 10)'
```

Find all data files in a `dist/` directory:

```sh
file-search-on '(is_json || is_csv || is_xml) && dir.contains("dist")'
```

Spreadsheet exports likely to be NDJSON-friendly (one JSON array, plenty of rows — file size as a proxy):

```sh
file-search-on 'is_json && json_kind == "array" && size > 1000000'
```

## Useful output formats

```sh
# All CSV column names, deduplicated
file-search-on 'is_csv' -o json | jq -r '.csv_columns[]?' | sort -u

# Path + column count, sorted by column count
file-search-on 'is_csv' --format '{{.ColumnCount}}\t{{.Path}}' | sort -n

# Find which columns appear in which CSVs (inverted index)
file-search-on 'is_csv' -o json | \
  jq -r '. as $r | .csv_columns[]? | "\(.)|\($r.path)"' | \
  sort | awk -F'|' '{c[$1]=c[$1] " " $2} END {for(k in c) print k ":" c[k]}'
```

## Interplay with full-text search

For content search inside JSON/CSV/XML, this tool's output composes naturally with `jq`, `grep`, `xsv`, etc.:

```sh
# Find CSVs with "revenue" column then filter for rows above threshold
file-search-on 'is_csv && csv_columns.exists(c, c == "revenue")' -o bare | \
  while read f; do xsv search -s revenue '^[5-9][0-9]{4,}' "$f"; done

# JSON arrays containing a specific key in any element
file-search-on 'is_json && json_kind == "array"' -o bare | \
  xargs -I {} jq 'map(select(.status == "active"))' {}
```
