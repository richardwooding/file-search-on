# Recipes — Plain text and HTML

`text/plain` covers `.txt`, `.text`, `.log`. Predicate: `is_text`.

`html` covers `.html`, `.htm`, `.xhtml`. Predicate: `is_html`.

Both are loop-bound types: a single `bufio.Scanner` pass per file with a configurable per-line buffer cap (`-L` flag, default 1 MiB).

## Plain text

By line count — useful for log triage:

```sh
file-search-on 'is_text && line_count > 1000' -d /var/log
file-search-on 'is_text && line_count == 0'                       # empty files
file-search-on 'is_text && ext == ".log" && line_count > 100000'  # large logs
```

By word count:

```sh
file-search-on 'is_text && word_count > 5000'                     # long-form text
file-search-on 'is_text && word_count < 100 && size > 1000000'    # huge files with few words (binary garbage in .txt?)
```

Combined — recently-rotated logs that have content:

```sh
file-search-on 'is_text && ext == ".log" && line_count > 0 && size < 10485760' -d /var/log
```

## Long-line files

The default 1 MiB per-line cap silently truncates lines longer than that. For files with very long single lines (minified output, single-line JSON logs), bump the cap:

```sh
file-search-on 'is_text && line_count > 0' -L 16777216 -d /var/log/json
```

Or to detect files that *might* have such lines (low line count for their size):

```sh
file-search-on 'is_text && size > 100000 && line_count < 10'
```

## HTML

By title (extracted from `<title>` element):

```sh
file-search-on 'is_html && title.contains("404")' -d ./build
file-search-on 'is_html && title == ""'                           # untitled pages
```

By language (`<html lang="...">`):

```sh
file-search-on 'is_html && language == "en"'
file-search-on 'is_html && language == ""'                        # missing lang attribute (a11y red flag)
```

Combined — find untitled HTML in a build directory (broken pages):

```sh
file-search-on 'is_html && title == "" && dir.contains("build")'
```

## Combining with paths

Files in a specific directory tree:

```sh
file-search-on 'is_text && dir.contains("/var/log/nginx/")'
```

Files NOT in node_modules / vendor:

```sh
file-search-on 'is_text && !dir.contains("node_modules") && !dir.contains("vendor")'
```

## Useful output formats

```sh
# Bare paths to feed grep / awk
file-search-on 'is_text && line_count > 1000' -o bare | xargs grep -l "ERROR"

# Sorted listing — biggest log files first
file-search-on 'is_text && ext == ".log"' -o json | jq -s 'sort_by(-.size) | .[].path'

# Line count totals
file-search-on 'is_text' -o json | jq -s 'map(.line_count) | add'
```

## What's NOT covered

- **Encoding detection** — the scanner reads UTF-8 / ASCII bytes; UTF-16 / Latin-1 files give correct line counts but `word_count` may be misleading for non-ASCII content.
- **Binary detection** — files with `.txt` extension but binary content will pass through; line count will reflect byte boundaries.
- **`.md` detection** — Markdown files are matched by `is_markdown`, not `is_text`. See [`markdown.md`](./markdown.md).
