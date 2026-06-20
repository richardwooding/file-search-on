# Recipes — CEL playground (interactive TUI)

`file-search-on playground` is an **interactive terminal UI for authoring CEL expressions**. It snapshots a directory once, then filters that snapshot live as you type — the match list and a per-file attribute panel update on every keystroke, over the *exact* attribute vocabulary the `search` command uses. When you exit it prints the final expression to stdout, so a query you dial in here drops straight into a `search` / `stats` / `watch` invocation.

```sh
file-search-on playground [<expr>] -d <root> [flags]
```

## Why

Authoring a non-trivial CEL expression is normally a guess-run-reread loop: edit the string, re-run `search`, scan the output, tweak, repeat. The playground collapses that loop to zero — you see the effect of every character immediately, and the attribute cheat-sheet (`tab`) reminds you what you can filter on. Great for exploration and for live demos.

## How it works

On launch it runs **one** walk (`search` with `Expr: "true"`, attributes included) and holds the results in memory. Every keystroke recompiles the expression (cel-go compiles in microseconds) and re-filters the cached snapshot — **no re-walk**, so filtering stays instant even on large trees. The snapshot is capped by `--limit` (default 5000) to keep memory and per-keystroke evaluation bounded; when the cap is hit the status line says so.

- **Empty expression** → matches everything.
- **Valid expression** → the list narrows to matches; the status line shows `N/M match`.
- **Compile error** → shown in red on the status line; the previous match set is kept so the list doesn't flash empty while you're mid-edit.

## Keys

| Key | Action |
|---|---|
| *(type)* | Edit the CEL expression (the input is always focused) |
| `↑` / `↓` | Move the selection through the match list |
| `PgUp` / `PgDn` | Page the match list |
| `tab` | Toggle the attribute cheat-sheet (every available attribute name) |
| `enter` / `esc` | Quit and print the current expression to stdout |
| `ctrl+c` | Quit |

The selected file's populated attributes (`content_type`, `size`, `mod_time`, plus a few type-specific keys) show in the panel below the list, so you can see exactly what your predicate is matching against.

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
