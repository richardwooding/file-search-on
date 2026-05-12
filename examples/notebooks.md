# Notebooks — Jupyter and Zeppelin

`file-search-on` recognises two JSON-on-disk notebook formats:

- **Jupyter** (`.ipynb`) — the de-facto Python / R / Julia notebook format
- **Apache Zeppelin** (`.zpln`) — Spark / Flink / Hive notebook format

Both expose a shared attribute set (`cell_count`, `code_cell_count`, `markdown_cell_count`, `kernel`, `language`, `title`) so you can write content-type-agnostic filters like `is_notebook && cell_count > 20`.

## Find notebooks

```sh
# All notebooks in a tree
file-search-on 'is_notebook' -d ~/notebooks

# Just Jupyter
file-search-on 'content_type == "notebook/jupyter"' -d ~/notebooks

# Just Zeppelin
file-search-on 'content_type == "notebook/zeppelin"' -d ~/zeppelin-notes
```

## Filter by size and language

```sh
# Big notebooks (> 30 cells) — candidates for splitting
file-search-on 'is_notebook && cell_count > 30' -d ~/notebooks --sort cell_count --order desc

# Mostly-prose notebooks (tutorials, reports — more markdown than code)
file-search-on 'is_notebook && markdown_cell_count > code_cell_count' -d ~/notebooks

# Python-kernel notebooks specifically
file-search-on 'is_notebook && kernel == "python3"' -d ~/notebooks

# Any non-Python notebook (R, Julia, Scala, …)
file-search-on 'is_notebook && language != "" && language != "python"' -d ~/notebooks

# Empty / stub notebooks
file-search-on 'is_notebook && cell_count <= 1' -d ~/notebooks
```

## Combining with body / content filters

`include_body` doesn't apply to notebooks (their JSON structure makes "body" ambiguous), so substring search on notebook content doesn't work directly. Two workarounds:

```sh
# Treat the notebook as JSON for raw content search
file-search-on 'content_type == "notebook/jupyter" && body.contains("scikit-learn")' \
    -d ~/notebooks --body

# (Yes — this works because the .ipynb file IS JSON. The 'body' read
# captures the raw JSON text including all cell source code.)
```

Caveat: the cap (`--body-max-bytes`, default 1 MiB) truncates large notebooks. For a research project's master notebook with embedded output images, bump the cap or use external grep tools.

## MCP

```json
{
  "name": "search",
  "arguments": {
    "expr": "is_notebook && cell_count > 20",
    "dir": "/Users/me/notebooks",
    "sort_by": "cell_count",
    "order": "desc",
    "limit": 10
  }
}
```

The `stats` tool gives an at-a-glance notebook count in a directory:

```json
{
  "name": "stats",
  "arguments": {
    "dir": "/Users/me/notebooks",
    "expr": "is_notebook"
  }
}
```

→ output buckets like `{"name": "notebook/jupyter", "count": 42, ...}` and `{"name": "notebook/zeppelin", "count": 3, ...}`.

## Attributes reference

| Attribute | Jupyter | Zeppelin |
| --- | --- | --- |
| `cell_count` | total cells (top-level `cells[]` + nested in `worksheets[]` for pre-v4) | total paragraphs |
| `code_cell_count` | cells with `cell_type == "code"` | paragraphs not classified as markdown |
| `markdown_cell_count` | cells with `cell_type == "markdown"` | paragraphs with editor language `markdown` OR text starting with `%md` |
| `kernel` | `metadata.kernelspec.name` (falls back to `display_name`) | `defaultInterpreterGroup` |
| `language` | `metadata.language_info.name` (falls back to `kernelspec.language`) | not surfaced (paragraphs use per-cell interpreters) |
| `title` | not surfaced (Jupyter has no notebook title field) | top-level `name` |

## Caveats

- **Per-cell language detection isn't surfaced.** Zeppelin paragraphs each declare their own interpreter (`%spark`, `%pyspark`, `%md`); we don't aggregate these into a list. Use the body workaround if you need fine-grained per-cell filtering.
- **Output cells aren't parsed.** A Jupyter notebook with images embedded as base64 in cell outputs will have a much larger byte size than a notebook with the same number of "useful" cells. `size` reflects raw file size; `cell_count` reflects structure.
- **Pre-v4 Jupyter notebooks** (the ancient `nbformat: 3` format) nested cells under `worksheets[]`. Both layouts are summed for robustness.
- **Malformed JSON degrades silently** — broken notebooks return empty attrs (the file is still detected as `notebook/jupyter` or `notebook/zeppelin` via extension, but the per-cell counts and kernel fields are zero/empty). Matches the cross-codebase "broken file doesn't fail the walk" pattern.
