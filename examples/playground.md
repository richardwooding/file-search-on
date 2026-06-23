# Recipes — CEL playground (interactive TUI)

`file-search-on playground` is an **interactive terminal UI for authoring CEL expressions**. It snapshots a directory once, then filters that snapshot live as you type — the match list and a per-file attribute panel update on every keystroke, over the *exact* attribute vocabulary the `search` command uses. When you exit it prints the final expression to stdout, so a query you dial in here drops straight into a `search` / `stats` / `watch` invocation.

```sh
file-search-on playground [<expr>] -d <root> [flags]
```

## Why

Authoring a non-trivial CEL expression is normally a guess-run-reread loop: edit the string, re-run `search`, scan the output, tweak, repeat. The playground collapses that loop to zero — you see the effect of every character immediately, and the scrollable attributes panel (`ctrl+a`) reminds you what you can filter on. Great for exploration and for live demos.

## How it works

On launch it runs **one** walk (`search` with `Expr: "true"`, attributes included) and holds the results in memory. Every keystroke recompiles the expression (cel-go compiles in microseconds) and re-filters the cached snapshot — **no re-walk**, so filtering stays instant even on large trees. The snapshot is capped by `--limit` (default 5000) to keep memory and per-keystroke evaluation bounded; when the cap is hit the status line says so.

- **Empty expression** → matches everything.
- **Valid expression** → the list narrows to matches; the status line shows `N/M match`.
- **Compile error** → shown in red on the status line; the previous match set is kept so the list doesn't flash empty while you're mid-edit.

## Keys

| Key | Action |
|---|---|
| *(type)* | Edit the focused input (CEL filter, or the semantic query box) |
| `ctrl+a` | Toggle the **scrollable attributes panel** on the right (every attribute name, type, and description). Opening it gives it focus so `↑`/`↓` scroll it immediately |
| `tab` | Cycle focus between the input(s) and the attributes panel (when open) |
| `↑` / `↓` · `PgUp` / `PgDn` | Navigate the match list — or scroll the attributes panel when it has focus |
| `enter` / `esc` | Quit and print the current expression to stdout (in semantic mode `enter` on the query box runs the search; `esc` quits) |
| `ctrl+c` | Quit |

The attributes panel lists every CEL attribute and function with its type and a one-line description, and scrolls independently — so you can browse the full vocabulary without leaving the list. The selected file's populated attributes (`content_type`, `size`, `mod_time`, plus a few type-specific keys) show in the detail panel below the list, so you can see exactly what your predicate is matching against.

## Flags

| Flag | Meaning |
|---|---|
| `-d, --dir` | Directory to snapshot (repeatable; default `.`) |
| `--exclude` | Basename glob to skip (repeatable) |
| `--respect-gitignore` | Honour a `.gitignore` at each root |
| `--prune-build-artefacts` | Union canonical build-artefact dirs (`vendor` / `node_modules` / `target` / …) into the excludes |
| `-w, --workers` | Workers for the initial snapshot (0 = NumCPU) |
| `--limit` | Cap on files snapshotted (default 5000) |
| `--body` | Read file bodies so `body.contains(...)` works — expensive (reads every candidate during the snapshot) |
| `--body-max-bytes` | Per-file body cap (0 = 1 MiB default) |
| `--index-path` | Persistent bbolt index (caches file embeddings across semantic re-queries) |
| `--no-index` | Disable the on-disk index |
| `--embedding-model` | Ollama embedding model (e.g. `all-minilm`). **Setting this enables semantic mode.** |
| `--embedding-server` | Ollama base URL (env `OLLAMA_HOST`; default `http://localhost:11434`) |
| `--semantic-query` | Pre-fill the natural-language query box |
| `--similarity-threshold` | Default similarity floor carried into the printed `search` command (default 0.5) |
| `--embed-max-bytes` | Cap on body text handed to the embedder (0 = 8 KiB default) |

## Semantic mode

Pass `--embedding-model` and the playground gains a **second input box** for a natural-language query. Type a query, press `enter`, and the files are re-walked with a per-file cosine **`similarity`** score (computed by [Ollama](https://ollama.com)) and listed best-match first. The CEL box below then filters that ranked snapshot live — so `similarity > 0.6` or `is_source && similarity > 0.7` refine the semantic results without re-embedding. File embeddings are cached in the on-disk index, so changing the query only re-embeds the *query*, not every file.

Prerequisite: a running Ollama with the model pulled (the examples use [`all-minilm`](https://ollama.com/library/all-minilm)):

```sh
ollama pull all-minilm
```

```sh
# Semantic search over the codebase; refine with CEL live
file-search-on playground --embedding-model all-minilm -d ./internal

# Pre-fill the natural-language query
file-search-on playground --embedding-model all-minilm \
  --semantic-query "how does cancellation propagate" -d ./internal
```

On exit, semantic mode prints a **reproducible `search` command** (instead of the bare CEL expression) so the query you dialled in runs non-interactively:

```sh
file-search-on search --semantic-query 'how does cancellation propagate' \
  --embedding-model 'all-minilm' --similarity-threshold 0.5 \
  'is_source && similarity > 0.6'
```

## Examples

```sh
# Explore source files in the repo's internal packages
file-search-on playground -d ./internal 'is_source'

# Pre-fill a complexity predicate and refine it live
file-search-on playground -d . 'is_source && max_complexity > 15'

# Capture the expression you author and reuse it
expr=$(file-search-on playground -d ./docs 'is_markdown')
file-search-on "$expr" -d ./docs --json

# Author a body-content filter interactively (bodies loaded up front)
file-search-on playground --body -d ./internal 'is_source && body.contains("TODO")'
```

The printed expression on exit is the bridge back to the rest of the toolset — `search`, `stats`, `duplicates`, `watch`, and the MCP `search` tool all speak the same CEL.
