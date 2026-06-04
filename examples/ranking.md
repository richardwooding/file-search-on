# Recipes — Custom ranking with `--rank`

`--rank <CEL-expression>` lets you compute a per-file score as the sort key. Higher values rank first by default. Composes with every other filter (`is_pdf`, `body.contains(...)`, `--semantic-query`, etc.) and with the existing top-K plumbing (`--limit`).

CEL semantics: the expression evaluates against every file that passes the filter. It may return:

- `double` — used directly as the rank value.
- `int` — coerced to `double` (so `--rank 'size'` works on the int-typed size attribute).
- `bool` — `true` becomes 1.0, `false` becomes 0.0 (so `--rank 'is_pdf'` surfaces PDFs first without ternary scaffolding).

Other return types (`string`, `list`, `map`, `timestamp`) error at evaluation. Per-file evaluation errors zero the rank rather than dropping the file from results — partial data beats missing matches.

Default order is descending (higher score first). Pass `--order asc` to flip. `--rank` always wins over `--sort` (the more expressive primitive).

## Boolean rerank — promote a class of files

Bare predicates as rank surface a category first without dropping anything:

```sh
# PDFs first, everything else by walk order
file-search-on 'is_text || is_pdf || is_markdown' --rank 'is_pdf' -d ~/Documents

# Markdown drafts first
file-search-on 'is_markdown' --rank 'draft' -d ~/notes

# Test files first
file-search-on 'is_source' --rank 'is_test_file' -d ./src
```

## Numeric rerank — biggest / longest / heaviest

```sh
# Largest text files first (composes with the dedicated --sort size,
# but works on richer expressions too)
file-search-on 'is_text' --rank 'size' -d /var/log

# Source files ranked by lines of code (LOC, not raw line count)
file-search-on 'is_source' --rank 'loc' -d ./src --limit 20

# Longest markdown notes
file-search-on 'is_markdown' --rank 'word_count' -d ~/notes --limit 10

# Photos with the highest ISO (noisy / low-light shots)
file-search-on 'is_image' --rank 'iso' -d ~/Pictures --limit 5
```

## Hybrid semantic + structural — the headline use case

`similarity` is a CEL variable populated when `--semantic-query` is set (issue #151). Compose with anything else:

```sh
# Semantic relevance, weighted by recency
file-search-on 'is_pdf || is_office' \
  --semantic-query "Q4 revenue forecast" \
  --embedding-model nomic-embed-text \
  --rank 'similarity * 0.7 + (mod_time > timestamp("2025-01-01T00:00:00Z") ? 0.3 : 0.0)' \
  --limit 10

# Boost PDFs in the semantic ranking
file-search-on 'is_pdf || is_office || is_markdown' \
  --semantic-query "annual planning" \
  --embedding-model nomic-embed-text \
  --rank '(is_pdf ? 1.0 : 0.0) + similarity' \
  --limit 10
```

`bm25` is a second ranking variable (issue #335), populated when `--keyword-query` is set — Okapi BM25 keyword relevance with IDF over the candidate set. Compose it with `similarity` for hybrid keyword+semantic ranking, or use `--hybrid` for automatic reciprocal-rank fusion (see [semantic-search.md](./semantic-search.md#hybrid-keyword--semantic-search-issue-335)):

```sh
# Manual hybrid blend: keyword precision + semantic recall
file-search-on 'is_pdf' \
  --semantic-query "http caching and proxies" \
  --keyword-query "http caching proxies" \
  --embedding-model nomic-embed-text \
  --rank 'bm25*0.4 + similarity*0.6' \
  --limit 10
```

## Inverse rank — smallest / oldest first

CEL allows unary minus on ints / doubles, so flipping the sign reverses the order while keeping desc the default:

```sh
# Smallest text files first
file-search-on 'is_text' --rank '-size' -d /var/log --limit 10

# Oldest files first (sort by negative mod_time epoch). Use the
# typed timestamp comparison rather than arithmetic for clarity.
file-search-on 'is_source' --order asc --rank 'size' -d ./src
```

The cleaner pattern for ascending-sort-without-negation is `--order asc`:

```sh
file-search-on 'is_image' --rank 'iso' --order asc -d ~/Pictures --limit 5
```

## Composing with `--limit`

`--rank` + `--limit` is the classic top-K query — rank EVERY matching file, then return the top N:

```sh
# 10 largest binaries
file-search-on 'is_binary' --rank 'size' --limit 10 -d ~/

# 5 most recently-taken HDR photos with GPS
file-search-on 'is_image && is_hdr && gps_lat != 0.0' \
  --rank 'taken_at == timestamp("0001-01-01T00:00:00Z") ? 0 : 1' \
  --limit 5 -d ~/Pictures
```

## Domain-specific scoring

```sh
# Code review priority: changed recently AND high LOC
file-search-on 'is_source' \
  --rank 'loc + (mod_time > timestamp("2025-04-01T00:00:00Z") ? 1000 : 0)' \
  --limit 20 -d ./src

# Audio: bitrate-weighted (high-quality first)
file-search-on 'is_audio' --rank 'bitrate' -d ~/Music --limit 10

# Email: large attachments first
file-search-on 'is_email' --rank 'attachment_count * 100 + size / 1000' -d ~/Mail
```

## Output

`--rank` populates `Match.Rank` in JSON output so agents can inspect the computed value:

```sh
file-search-on 'is_text' --rank 'size' -d /var/log -o json | head -3
```

```json
{"path":"/var/log/system.log","content_type":"text","size":12345,"rank":12345}
```

## Known limitations

- **Compile errors surface at walk entry**: malformed rank expressions are caught when the walker compiles the rank program, not silently per-file. You'll see the cel-go error message with line/column info.
- **`size +` style syntax errors are CEL syntax errors**, not Go errors. The error message points at the `+` token.
- **List / map / string / timestamp return types**: produce a clear per-file error and the rank zeroes for that file. The file still appears in results.
- **Streaming output modes** (`bare`, `json` without `--limit`, `template` without `--sort`) normally stream; `--rank` forces buffered mode like `--sort`.
