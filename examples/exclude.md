# Excludes and .gitignore: prune the walk

By default the walker visits everything **except `.git`** (see below). For dev repos, `node_modules`, `target`, `dist`, and friends often dwarf the actual source — pruning them up front is a huge real-world win. Two mechanisms cover the common cases:

- `--exclude <pattern>` — explicit glob list, matched against the basename of each entry. Repeatable.
- `--respect-gitignore` — parse a `.gitignore` at the walk root and honour standard gitignore semantics (including `**` and negation).

Both can be combined.

## `.git` is always skipped

Every walk — `search`, `stats`, `duplicates`, `find-matches`, the code-analysis commands, and the MCP equivalents — **prunes `.git` directories by default**, so VCS internals (objects, refs, logs) are never searched or indexed. You don't need `--exclude .git`. To override:

- `search` / `find-matches` accept `--include-git` (CLI) / `include_git` (MCP) to walk into `.git`.
- Pointing `-d` directly at a `.git` directory works without any flag — the walk root itself is exempt from the prune.

## CLI

```sh
# Skip the usual suspects in a dev repo (.git is already skipped by default)
file-search-on 'is_source' -d . \
    --exclude node_modules \
    --exclude target \
    --exclude dist

# Use the repo's own .gitignore (typical "search what's checked in")
file-search-on 'is_source && language == "rust"' -d . --respect-gitignore

# Combine — gitignore + explicit ad-hoc additions
file-search-on 'true' -d . --respect-gitignore --exclude '*.bak' --exclude '*.swp'

# Triage a Downloads directory without descending into archives that
# the shell mounted as directories
file-search-on 'is_image' -d ~/Downloads --exclude '*.dmg.mounted'
```

## Pattern semantics

`--exclude` uses Go's `filepath.Match`. **Matching is against the basename**, not the full path:

| Pattern | Matches | Notes |
| --- | --- | --- |
| `node_modules` | any dir or file named exactly `node_modules` | basename-only |
| `*.bak` | any file ending in `.bak` | extension glob |
| `.[!.]?*` | hidden files like `.cache` but not `.` / `..` | classic Unix idiom |
| `[Tt]arget` | `target` or `Target` directories | character class |

Matched directories are pruned via `fs.SkipDir` — their entire subtree is skipped, not visited and filtered after the fact. That's the performance win.

For **path-aware** semantics (e.g. "skip `src/build` but not other `build` dirs"), use `--respect-gitignore` with a `.gitignore` containing `src/build/`. Or write a project-specific `.gitignore` and check it in.

## Gitignore semantics

`--respect-gitignore` honours the [standard gitignore spec](https://git-scm.com/docs/gitignore):

- `**/build` matches `build` anywhere in the tree
- `/build` matches `build` only at the root
- `build/` matches only directories (not files named `build`)
- `!keep.txt` negates an earlier pattern
- `*.log` standard glob

**Caveat:** only the `.gitignore` at the walk root is consulted. Nested `.gitignore` files in subdirectories are NOT honoured in this version. If you have a monorepo with per-package gitignores, the leaf-level patterns won't take effect. Add the patterns you need to the root file, or open an issue if nested support becomes important.

## Performance

For a typical Node project, `--exclude node_modules` cuts walk time by 10×–100× depending on how bloated `node_modules` is. For a Rust project with `--exclude target`, similar savings. The cost of evaluating excludes is negligible (basename glob check per entry); the savings come from `fs.SkipDir` pruning the visit entirely.

## MCP

```json
{
  "name": "search",
  "arguments": {
    "expr": "is_source && language == \"go\"",
    "dir": "/Users/me/proj",
    "excludes": ["node_modules", ".git", "vendor", "dist"],
    "respect_gitignore": true
  }
}
```

Same semantics as the CLI. Agents typically pair `excludes` with `respect_gitignore` for safety: gitignore catches project-specific patterns the agent doesn't know about, excludes catches universal noise (`node_modules`, `.git`) that may not always be in `.gitignore`.
