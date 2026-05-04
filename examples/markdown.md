# Recipes — Markdown front-matter

Markdown front-matter search is a primary use case. Three formats are recognised, each detected by the very first bytes:

| Format | Opening | Closing | Common in |
| --- | --- | --- | --- |
| YAML | `---` line | `---` line | Jekyll, Obsidian, Hugo, MkDocs |
| TOML | `+++` line | `+++` line | Hugo, Zola |
| JSON | `{` at byte 0 | matching `}` | Hugo, Eleventy |

Seven keys are promoted to first-class CEL variables: `title`, `author`, `language`, `tags`, `categories`, `draft`, `date`. Anything else is reachable via the generic `frontmatter` map.

## Drafts and publishing state

Find drafts:

```sh
file-search-on 'is_markdown && draft' -d ./content
```

Find published posts (note `!draft` covers both `draft: false` and missing `draft`):

```sh
file-search-on 'is_markdown && !draft' -d ./content
```

## Tags and categories

Find Markdown tagged `go`:

```sh
file-search-on 'is_markdown && "go" in tags' -d ./posts
```

Find non-draft posts tagged `longread`:

```sh
file-search-on 'is_markdown && "longread" in tags && !draft'
```

Find posts in either of two categories:

```sh
file-search-on 'is_markdown && ("essays" in categories || "notes" in categories)'
```

A bare `tags: solo` is automatically wrapped to a single-element list, so `"solo" in tags` works without authors needing to write `[solo]`.

## Dates

Find posts from 2024 onward:

```sh
file-search-on 'is_markdown && date >= timestamp("2024-01-01T00:00:00Z")'
```

Find posts in a date range:

```sh
file-search-on 'is_markdown && date >= timestamp("2024-06-01T00:00:00Z") && date < timestamp("2024-09-01T00:00:00Z")'
```

Native TOML dates and common YAML/JSON string layouts (RFC3339, `YYYY-MM-DD`) are accepted.

## Custom front-matter keys

Reach any unpromoted key via `frontmatter.<key>`:

```sh
file-search-on 'is_markdown && frontmatter.category == "essay"'
file-search-on 'is_markdown && frontmatter.weight > 50'
file-search-on 'is_markdown && frontmatter.featured == true'
```

For nested keys like `seo.description`, use chained lookups:

```sh
file-search-on 'is_markdown && frontmatter.seo.description != ""'
```

## Front-matter format

The `frontmatter_format` variable reports which dialect was detected:

```sh
file-search-on 'is_markdown && frontmatter_format == "toml"'   # Hugo / Zola
file-search-on 'is_markdown && frontmatter_format == ""'       # No front-matter at all
```

## Body content

Long-form content thresholds:

```sh
file-search-on 'is_markdown && word_count > 1000'                          # longreads
file-search-on 'is_markdown && word_count < 100 && !draft'                 # stub posts
```

The H1 fallback for `title`: if there's no front-matter `title`, the first `# Heading` line wins. To find posts without an H1 *or* a front-matter title:

```sh
file-search-on 'is_markdown && title == ""'
```

## Combining

Long, tagged, non-draft posts (the canonical longread filter):

```sh
file-search-on 'is_markdown && word_count > 1500 && "longread" in tags && !draft'
```

Drafts that have been worked on (have a body but unfinished):

```sh
file-search-on 'is_markdown && draft && word_count > 200'
```

Foreign-language posts via the cross-cutting `language` variable (works for HTML and EPUB too):

```sh
file-search-on 'is_markdown && language == "fr"'
```

## Useful output formats

```sh
# Just paths, ready to pipe
file-search-on 'is_markdown && draft' -o bare | xargs $EDITOR

# Multi-line view of all metadata
file-search-on 'is_markdown && "longread" in tags' -o verbose | head -40

# Custom columns: path, title, word count
file-search-on 'is_markdown && !draft' --format '{{.Path}}\t{{.Title}}\t{{.WordCount}}'

# Full JSON for jq pipelines
file-search-on 'is_markdown' -o json | jq 'select(.frontmatter.weight > 50) | .path'
```

## Fuzzy matching

```sh
# Authors whose front-matter spelling is within 2 edits of the target.
file-search-on 'is_markdown && levenshtein(author, "Jane Doe") <= 2'

# Titles with high n-gram overlap to a topic — survives misspellings.
file-search-on 'is_markdown && ngram_similarity(title, "kubernetes", 2) > 0.6'
```

See [`fuzzy-search.md`](./fuzzy-search.md) for the full set of fuzzy / phonetic recipes.
