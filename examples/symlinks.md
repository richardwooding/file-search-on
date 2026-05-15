# Recipes — Symlinks

Symlink awareness is a per-file CEL surface, not a content-type family. Every matched entry gets `is_symlink`, `is_broken_symlink`, and (when symlink) `target_path` populated by the walker's `os.Lstat` + `os.Readlink` probe. Behaviour for symlink-to-directory entries is controlled by the `--follow-symlinks` (CLI) / `follow_symlinks` (MCP) walker option — default **off** (preserves Go's `fs.WalkDir` behaviour: dir-symlinks surface as leaf entries, not recursed into).

## Detection

```sh
# Every symlink under a directory
file-search-on 'is_symlink' -d ~

# File symlinks vs directory symlinks (dir-symlinks have target_path
# resolving to a directory; combine with `name` patterns or a separate
# Glob if you need the discriminator more precisely)
file-search-on 'is_symlink && content_type != ""' -d ~     # file symlinks (the target's type fired)
file-search-on 'is_symlink && content_type == ""' -d ~     # dir-symlinks-as-leaves OR broken
```

## Audit broken / dangling symlinks

The classic "what's left over after I moved this directory" reconnaissance:

```sh
# Every dangling symlink under ~
file-search-on 'is_broken_symlink' -d ~ --exclude .Trash

# Broken symlinks pointing at a specific prefix (e.g. an old install dir)
file-search-on 'is_broken_symlink && target_path.startsWith("/opt/old-app/")' -d /

# Sort by mod_time to find the oldest dangling links first
file-search-on 'is_broken_symlink' -d ~ --sort-by mod_time --order asc
```

Broken symlinks surface as entries with `content_type = ""` (no target to detect) but `is_symlink = true` AND `is_broken_symlink = true`. The walk doesn't error — agents see them as regular results.

## Follow target paths

`target_path` is the raw string from `os.Readlink` — exactly what `ln -s` wrote. Relative or absolute, whatever the user passed:

```sh
# Absolute-target symlinks (less portable; common cause of breakage on copy)
file-search-on 'is_symlink && target_path.startsWith("/")' -d ~

# Relative-target symlinks (portable; typical Homebrew / asdf pattern)
file-search-on 'is_symlink && !target_path.startsWith("/")' -d ~

# Symlinks pointing OUT of a tree (target escapes via ../..)
file-search-on 'is_symlink && target_path.contains("../")' -d ./vendor
```

## Common-pattern cleanup

Most package managers and toolchain pinning (asdf, pyenv, nvm, Homebrew) leave large numbers of symlinks. Auditing them:

```sh
# Every Homebrew-managed binary symlink
file-search-on 'is_symlink && target_path.contains("/Cellar/")' -d /opt/homebrew/bin

# asdf-style shims (typically point at versions/<lang>/<ver>/bin/<exe>)
file-search-on 'is_symlink && target_path.matches("/versions/[^/]+/[^/]+/bin/")' -d ~/.asdf/shims

# node_modules symlink hoisting (pnpm / npm workspaces) — find all
# symlink entry points
file-search-on 'is_symlink' -d ./node_modules --exclude .bin
```

## Descend through symlinked directories

By default the walker treats symlinks-to-dirs as leaf entries — useful for "audit my symlinks" workflows, useless for "search the actual code that's installed via this symlink farm." Opt-in to descent:

```sh
# Find Go source inside an asdf-managed Go install (symlinked to versions/...)
file-search-on 'is_source && language == "go"' -d ~/.asdf/installs/golang/current --follow-symlinks

# Search a Homebrew formula's installed files (Cellar layout is fully symlinked)
file-search-on 'is_markdown' -d /opt/homebrew/share --follow-symlinks

# MCP equivalent
{"name": "mcp__file-search-on__search",
 "arguments": {"expr": "is_source", "dir": "/opt/homebrew", "follow_symlinks": true}}
```

When `--follow-symlinks` is set, the walker descends via `filepath.EvalSymlinks` → `fs.WalkDir(os.DirFS(target), ".", ...)` and re-anchors each sub-entry's `path` under the original symlink. Search results show the user-facing location (`/opt/homebrew/share/...`) rather than the resolved target (`/opt/homebrew/Cellar/...`) — convenient for agents that want to act on the path the user sees.

**No symlink-loop detection.** A cycle (`a -> b/c`, `b/c -> ../a`) produces an OS-level "too many levels of symbolic links" error from Go's `WalkDir`, which the walker reports as a non-fatal warning. The walk continues with the next root. Best to know your tree is acyclic before enabling.

## Duplicate detection + symlinks

`find_duplicates` hashes the target's bytes (the file's actual content) via `os.Open` which follows symlinks. So a regular file plus a symlink to it will hash identically and surface as a duplicate group:

```sh
# Find duplicates including symlinked copies
file-search-on duplicates -d ~

# Restrict to NON-symlink duplicates only (real copies, not just aliases)
file-search-on 'is_source && !is_symlink' -d ~ --sort-by size --order desc
# … then pipe to find_duplicates externally on the result paths
```

To filter symlinks OUT of a duplicate search entirely (treating two symlinks-to-the-same-target as a single entry rather than a group), pair `find_duplicates` with the existing `--exclude` mechanism by file-pattern, or post-filter the JSON output via `jq`.

## Caveats

- **`--follow-symlinks=true` has no loop protection.** A cyclic symlink tree will surface as an `ELOOP` error from the OS once Go's WalkDir hits the depth limit (typically 40 levels). Best avoided unless the tree is known acyclic.
- **In-memory test filesystems** (`fstest.MapFS`, `os.DirFS` without a real path) don't surface symlink info — `os.Lstat` returns ENOENT and `probeSymlink` falls through silently. This matters only for tests; real searches always run against `os.DirFS(root)` with real OS paths.
- **Hard links are not detected.** Two paths sharing the same inode look like two independent files. Hard-link detection requires `os.Stat`'s `Sys()` field and platform-specific Inode comparison — out of scope here.
- **Cache keys still use the symlink path, not the resolved target.** Two symlinks pointing at the same file get cached independently. Could be improved with `filepath.EvalSymlinks`-normalised keys; out of scope for v1.
